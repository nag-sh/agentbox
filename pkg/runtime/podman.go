package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// PodmanRuntime implements the [Runtime] interface using the podman CLI.
//
// Podman is the preferred runtime for agentbox because it supports rootless
// containers out of the box, does not require a daemon, and is compatible with
// OCI image specifications. GPU passthrough is handled via --device flags and
// --security-opt for NVIDIA CDI devices.
type PodmanRuntime struct {
	// binary is the path to the podman binary.
	binary string

	// logger is the structured logger for this runtime.
	logger *log.Logger
}

// NewPodmanRuntime creates a new podman runtime instance.
// If binaryPath is empty, "podman" is resolved from $PATH.
func NewPodmanRuntime(binaryPath string, logger *log.Logger) *PodmanRuntime {
	if binaryPath == "" {
		binaryPath = "podman"
	}
	return &PodmanRuntime{
		binary: binaryPath,
		logger: logger,
	}
}

// Name returns "podman".
func (r *PodmanRuntime) Name() string {
	return "podman"
}

// Version returns the podman version string.
func (r *PodmanRuntime) Version(ctx context.Context) (string, error) {
	out, err := r.exec(ctx, "version", "--format", "{{.Client.Version}}")
	if err != nil {
		return "", fmt.Errorf("podman version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Run creates and starts a container with the given options using podman run.
// It blocks until the container exits or the context is cancelled.
func (r *PodmanRuntime) Run(ctx context.Context, opts RunOptions) error {
	args := r.buildRunArgs(opts)
	r.logger.Debug("running container", "image", opts.Image, "name", opts.Name)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if opts.Interactive {
		cmd.Stdin = os.Stdin
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman run %s: %w", opts.Image, err)
	}
	return nil
}

// Build builds a container image from a Dockerfile/Containerfile using podman build.
func (r *PodmanRuntime) Build(ctx context.Context, opts BuildOptions) error {
	args := r.buildBuildArgs(opts)
	r.logger.Debug("building image", "context", opts.ContextDir, "tags", opts.Tags)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("podman build: %w", err)
	}
	return nil
}

// Pull fetches a container image from a remote registry.
func (r *PodmanRuntime) Pull(ctx context.Context, ref string) error {
	r.logger.Debug("pulling image", "ref", ref)
	_, err := r.exec(ctx, "pull", ref)
	if err != nil {
		return fmt.Errorf("podman pull %s: %w", ref, err)
	}
	return nil
}

// Push uploads a container image to a remote registry.
func (r *PodmanRuntime) Push(ctx context.Context, ref string) error {
	r.logger.Debug("pushing image", "ref", ref)
	_, err := r.exec(ctx, "push", ref)
	if err != nil {
		return fmt.Errorf("podman push %s: %w", ref, err)
	}
	return nil
}

// Import loads a container image from a local OCI tarball or directory.
// The ref parameter is used to tag the imported image.
func (r *PodmanRuntime) Import(ctx context.Context, path string, ref string) error {
	r.logger.Debug("importing image", "path", path, "ref", ref)
	_, err := r.exec(ctx, "load", "-i", path)
	if err != nil {
		return fmt.Errorf("podman load %s: %w", path, err)
	}
	return nil
}

// Inspect returns metadata about a local container image.
func (r *PodmanRuntime) Inspect(ctx context.Context, ref string) (*ImageInfo, error) {
	r.logger.Debug("inspecting image", "ref", ref)
	out, err := r.exec(ctx, "inspect", "--type", "image", "--format", "json", ref)
	if err != nil {
		return nil, fmt.Errorf("podman inspect %s: %w", ref, err)
	}

	return parsePodmanInspect(out)
}

// Remove deletes a local container image.
func (r *PodmanRuntime) Remove(ctx context.Context, ref string) error {
	r.logger.Debug("removing image", "ref", ref)
	_, err := r.exec(ctx, "rmi", ref)
	if err != nil {
		return fmt.Errorf("podman rmi %s: %w", ref, err)
	}
	return nil
}

// buildRunArgs constructs the full argument list for a podman run command.
func (r *PodmanRuntime) buildRunArgs(opts RunOptions) []string {
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
		// Pass env var from host without specifying value — podman inherits it.
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

	// GPU passthrough — podman uses --device and CDI.
	args = append(args, r.buildGPUArgs(opts)...)

	// Resource limits.
	if opts.ResourceLimits != nil {
		args = append(args, buildResourceArgs(opts.ResourceLimits)...)
	}

	// Network flags.
	args = append(args, opts.NetworkFlags...)

	// Secret file mounts — podman supports --secret natively, but for
	// cross-runtime compatibility we use bind mounts with ro option.
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
		// Additional entrypoint args are prepended to command.
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

// buildBuildArgs constructs the argument list for a podman build command.
func (r *PodmanRuntime) buildBuildArgs(opts BuildOptions) []string {
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

// buildGPUArgs returns podman-specific flags for GPU passthrough.
// Podman uses --device for direct device access and --security-opt for
// CDI (Container Device Interface) with NVIDIA GPUs.
func (r *PodmanRuntime) buildGPUArgs(opts RunOptions) []string {
	if len(opts.GPUDevices) == 0 && opts.GPURuntime == "" {
		return nil
	}

	var args []string

	switch strings.ToLower(opts.GPURuntime) {
	case "nvidia":
		// Use CDI for NVIDIA GPUs with podman.
		for _, dev := range opts.GPUDevices {
			args = append(args, "--device", "nvidia.com/gpu="+dev)
		}
		if len(opts.GPUDevices) == 0 {
			// Default: expose all NVIDIA GPUs.
			args = append(args, "--device", "nvidia.com/gpu=all")
		}
		args = append(args, "--security-opt", "label=disable")
	case "amd":
		// AMD GPUs use /dev/dri and /dev/kfd device passthrough.
		args = append(args, "--device", "/dev/dri")
		args = append(args, "--device", "/dev/kfd")
		args = append(args, "--security-opt", "label=disable")
	default:
		// Pass devices directly.
		for _, dev := range opts.GPUDevices {
			args = append(args, "--device", dev)
		}
	}

	return args
}

// exec runs a podman command and returns its combined stdout output.
func (r *PodmanRuntime) exec(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, r.binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// podmanInspectResult is the JSON structure returned by podman inspect.
type podmanInspectResult struct {
	ID           string   `json:"Id"`
	RepoTags     []string `json:"RepoTags"`
	Size         int64    `json:"Size"`
	Created      string   `json:"Created"`
	Architecture string   `json:"Architecture"`
	OS           string   `json:"Os"`
	Labels       map[string]string `json:"Labels"`
	Config       struct {
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
}

// parsePodmanInspect parses the JSON output of podman inspect into an ImageInfo.
func parsePodmanInspect(data []byte) (*ImageInfo, error) {
	var results []podmanInspectResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("parsing inspect output: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no image info returned")
	}

	r := results[0]

	created, _ := time.Parse(time.RFC3339Nano, r.Created)

	// Labels can come from either top-level or Config.
	labels := r.Labels
	if labels == nil {
		labels = r.Config.Labels
	}

	return &ImageInfo{
		ID:           r.ID,
		RepoTags:     r.RepoTags,
		Size:         r.Size,
		Created:      created,
		Architecture: r.Architecture,
		OS:           r.OS,
		Labels:       labels,
	}, nil
}

// formatMount produces a --mount flag value from a Mount struct.
func formatMount(m Mount) string {
	parts := []string{"type=" + m.Type}
	if m.Source != "" {
		parts = append(parts, "source="+m.Source)
	}
	parts = append(parts, "target="+m.Target)
	parts = append(parts, m.Options...)
	return strings.Join(parts, ",")
}

// formatPort produces a -p flag value from a PortMapping struct.
func formatPort(p PortMapping) string {
	proto := p.Protocol
	if proto == "" {
		proto = "tcp"
	}
	host := strconv.Itoa(p.HostPort)
	container := strconv.Itoa(p.ContainerPort)
	if p.HostIP != "" {
		return p.HostIP + ":" + host + ":" + container + "/" + proto
	}
	return host + ":" + container + "/" + proto
}

// formatSecretMount produces a --mount flag value for a secret file.
// Secrets are mounted as read-only bind mounts.
func formatSecretMount(s SecretMount) string {
	parts := []string{
		"type=bind",
		"source=" + s.Source,
		"target=" + s.Target,
		"readonly",
	}
	return strings.Join(parts, ",")
}

// buildResourceArgs translates ResourceLimits into CLI flags common to
// both podman and docker.
func buildResourceArgs(limits *ResourceLimits) []string {
	var args []string

	if limits.CPUs != "" {
		args = append(args, "--cpus", limits.CPUs)
	}
	if limits.MemoryBytes > 0 {
		args = append(args, "--memory", strconv.FormatInt(limits.MemoryBytes, 10))
	}
	if limits.MemorySwapBytes > 0 {
		args = append(args, "--memory-swap", strconv.FormatInt(limits.MemorySwapBytes, 10))
	}
	if limits.PidsLimit > 0 {
		args = append(args, "--pids-limit", strconv.FormatInt(limits.PidsLimit, 10))
	}
	if limits.ReadonlyRootfs {
		args = append(args, "--read-only")
	}
	if limits.NoNewPrivileges {
		args = append(args, "--security-opt", "no-new-privileges")
	}

	return args
}
