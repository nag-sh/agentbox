package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// DockerRuntime implements the [Runtime] interface using the docker CLI.
//
// Docker is supported as a fallback runtime when podman is not available.
// The primary differences from the podman implementation are:
//   - GPU passthrough uses --gpus with nvidia-container-toolkit instead of CDI
//   - The docker daemon must be running (docker is not daemonless like podman)
//   - Some flag names and behaviors differ slightly (e.g. image import)
type DockerRuntime struct {
	// binary is the path to the docker binary.
	binary string

	// logger is the structured logger for this runtime.
	logger *log.Logger
}

// NewDockerRuntime creates a new docker runtime instance.
// If binaryPath is empty, "docker" is resolved from $PATH.
func NewDockerRuntime(binaryPath string, logger *log.Logger) *DockerRuntime {
	if binaryPath == "" {
		binaryPath = "docker"
	}
	return &DockerRuntime{
		binary: binaryPath,
		logger: logger,
	}
}

// Name returns "docker".
func (r *DockerRuntime) Name() string {
	return "docker"
}

// Version returns the docker version string.
func (r *DockerRuntime) Version(ctx context.Context) (string, error) {
	out, err := r.exec(ctx, "version", "--format", "{{.Client.Version}}")
	if err != nil {
		return "", fmt.Errorf("docker version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Run creates and starts a container with the given options using docker run.
// It blocks until the container exits or the context is cancelled.
func (r *DockerRuntime) Run(ctx context.Context, opts RunOptions) error {
	args := r.buildRunArgs(opts)
	r.logger.Debug("running container", "image", opts.Image, "name", opts.Name)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if opts.Interactive {
		cmd.Stdin = os.Stdin
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker run %s: %w", opts.Image, err)
	}
	return nil
}

// Build builds a container image from a Dockerfile using docker build.
func (r *DockerRuntime) Build(ctx context.Context, opts BuildOptions) error {
	args := r.buildBuildArgs(opts)
	r.logger.Debug("building image", "context", opts.ContextDir, "tags", opts.Tags)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}
	return nil
}

// Pull fetches a container image from a remote registry.
func (r *DockerRuntime) Pull(ctx context.Context, ref string) error {
	r.logger.Debug("pulling image", "ref", ref)
	_, err := r.exec(ctx, "pull", ref)
	if err != nil {
		return fmt.Errorf("docker pull %s: %w", ref, err)
	}
	return nil
}

// Push uploads a container image to a remote registry.
func (r *DockerRuntime) Push(ctx context.Context, ref string) error {
	r.logger.Debug("pushing image", "ref", ref)
	_, err := r.exec(ctx, "push", ref)
	if err != nil {
		return fmt.Errorf("docker push %s: %w", ref, err)
	}
	return nil
}

// Import loads a container image from a local OCI tarball.
// Docker uses "docker load" for OCI/docker archives. The ref parameter
// is used to tag the loaded image.
func (r *DockerRuntime) Import(ctx context.Context, path string, ref string) error {
	r.logger.Debug("importing image", "path", path, "ref", ref)
	_, err := r.exec(ctx, "load", "-i", path)
	if err != nil {
		return fmt.Errorf("docker load %s: %w", path, err)
	}
	return nil
}

// Inspect returns metadata about a local container image.
func (r *DockerRuntime) Inspect(ctx context.Context, ref string) (*ImageInfo, error) {
	r.logger.Debug("inspecting image", "ref", ref)
	out, err := r.exec(ctx, "inspect", "--type", "image", ref)
	if err != nil {
		return nil, fmt.Errorf("docker inspect %s: %w", ref, err)
	}

	return parseDockerInspect(out)
}

// Remove deletes a local container image.
func (r *DockerRuntime) Remove(ctx context.Context, ref string) error {
	r.logger.Debug("removing image", "ref", ref)
	_, err := r.exec(ctx, "rmi", ref)
	if err != nil {
		return fmt.Errorf("docker rmi %s: %w", ref, err)
	}
	return nil
}

// buildRunArgs constructs the full argument list for a docker run command.
func (r *DockerRuntime) buildRunArgs(opts RunOptions) []string {
	args := []string{"run"}

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	if opts.Interactive {
		args = append(args, "-i")
	}
	if opts.TTY {
		args = append(args, "-t")
	}
	if opts.Remove {
		args = append(args, "--rm")
	}
	if opts.Workdir != "" {
		args = append(args, "--workdir", opts.Workdir)
	}

	// Environment variables.
	for k, v := range opts.Env {
		args = append(args, "-e", k+"="+v)
	}
	for _, name := range opts.EnvPassthrough {
		// Docker inherits host env var when no value is specified.
		args = append(args, "-e", name)
	}

	// Mounts.
	for _, m := range opts.Mounts {
		args = append(args, "--mount", formatMount(m))
	}

	// Port mappings.
	for _, p := range opts.Ports {
		args = append(args, "-p", formatPort(p))
	}

	// GPU passthrough — docker uses --gpus with nvidia-container-toolkit.
	args = append(args, r.buildGPUArgs(opts)...)

	// Resource limits.
	if opts.ResourceLimits != nil {
		args = append(args, buildResourceArgs(opts.ResourceLimits)...)
	}

	// Network flags.
	args = append(args, opts.NetworkFlags...)

	// Secret file mounts — docker supports --mount type=bind for secrets.
	// We use bind mounts for cross-runtime consistency.
	for _, s := range opts.SecretFiles {
		args = append(args, "--mount", formatSecretMount(s))
		if s.Env != "" {
			args = append(args, "-e", s.Env+"="+s.Target)
		}
	}

	// Labels.
	for k, v := range opts.Labels {
		args = append(args, "--label", k+"="+v)
	}

	// Extra args escape hatch.
	args = append(args, opts.ExtraArgs...)

	// Entrypoint.
	if len(opts.Entrypoint) > 0 {
		args = append(args, "--entrypoint", opts.Entrypoint[0])
		if len(opts.Entrypoint) > 1 {
			opts.Command = append(opts.Entrypoint[1:], opts.Command...)
		}
	}

	// Image (required).
	args = append(args, opts.Image)

	// Command.
	args = append(args, opts.Command...)

	return args
}

// buildBuildArgs constructs the argument list for a docker build command.
func (r *DockerRuntime) buildBuildArgs(opts BuildOptions) []string {
	args := []string{"build"}

	for _, tag := range opts.Tags {
		args = append(args, "-t", tag)
	}

	if opts.Dockerfile != "" {
		args = append(args, "-f", opts.Dockerfile)
	}

	for k, v := range opts.BuildArgs {
		args = append(args, "--build-arg", k+"="+v)
	}

	if opts.Target != "" {
		args = append(args, "--target", opts.Target)
	}

	if opts.NoCache {
		args = append(args, "--no-cache")
	}

	if opts.Pull {
		args = append(args, "--pull")
	}

	for k, v := range opts.Labels {
		args = append(args, "--label", k+"="+v)
	}

	args = append(args, opts.ContextDir)

	return args
}

// buildGPUArgs returns docker-specific flags for GPU passthrough.
// Docker uses --gpus with NVIDIA Container Toolkit for NVIDIA GPUs
// and direct device passthrough for AMD GPUs.
func (r *DockerRuntime) buildGPUArgs(opts RunOptions) []string {
	if len(opts.GPUDevices) == 0 && opts.GPURuntime == "" {
		return nil
	}

	var args []string

	switch strings.ToLower(opts.GPURuntime) {
	case "nvidia":
		// Docker uses --gpus flag with nvidia-container-toolkit.
		if len(opts.GPUDevices) == 0 {
			args = append(args, "--gpus", "all")
		} else {
			// Specific devices: --gpus '"device=0,1"'
			deviceList := strings.Join(opts.GPUDevices, ",")
			args = append(args, "--gpus", fmt.Sprintf(`"device=%s"`, deviceList))
		}
		// Set the NVIDIA runtime explicitly for best compatibility.
		args = append(args, "--runtime=nvidia")
	case "amd":
		// AMD GPUs use direct device passthrough with docker too.
		args = append(args, "--device", "/dev/dri")
		args = append(args, "--device", "/dev/kfd")
		args = append(args, "--group-add", "video")
		args = append(args, "--group-add", "render")
	default:
		// Pass devices directly.
		for _, dev := range opts.GPUDevices {
			args = append(args, "--device", dev)
		}
	}

	return args
}

// exec runs a docker command and returns its stdout output.
func (r *DockerRuntime) exec(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// dockerInspectResult is the JSON structure returned by docker inspect.
type dockerInspectResult struct {
	ID           string   `json:"Id"`
	RepoTags     []string `json:"RepoTags"`
	Size         int64    `json:"Size"`
	Created      string   `json:"Created"`
	Architecture string   `json:"Architecture"`
	OS           string   `json:"Os"`
	Config       struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

// parseDockerInspect parses the JSON output of docker inspect into an ImageInfo.
func parseDockerInspect(data []byte) (*ImageInfo, error) {
	var results []dockerInspectResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("parsing inspect output: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no image info returned")
	}

	d := results[0]

	created, _ := time.Parse(time.RFC3339Nano, d.Created)

	return &ImageInfo{
		ID:           d.ID,
		RepoTags:     d.RepoTags,
		Size:         d.Size,
		Created:      created,
		Architecture: d.Architecture,
		OS:           d.OS,
		Labels:       d.Config.Labels,
	}, nil
}
