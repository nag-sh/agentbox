// Package runtime provides a runtime-agnostic container runtime interface.
//
// The runtime package abstracts over podman, docker, and containerd/nerdctl,
// allowing agentbox to build, run, and manage container images regardless of
// the underlying container runtime installed on the host system. Podman is
// preferred for its rootless capabilities, but docker and nerdctl are supported
// as fallbacks.
//
// The primary entry point is [Detect], which auto-discovers the best available
// runtime, or [ForName] to select a specific runtime by name.
package runtime

import (
	"context"
	"time"
)

// Runtime is the interface that all container runtime implementations must
// satisfy. It provides a uniform API for image and container operations
// across podman, docker, and nerdctl.
type Runtime interface {
	// Name returns the human-readable name of this runtime (e.g. "podman", "docker", "nerdctl").
	Name() string

	// Version returns the version string of the runtime binary.
	// Returns an error if the runtime binary is not found or not functional.
	Version(ctx context.Context) (string, error)

	// Run creates and starts a container with the given options.
	// It blocks until the container exits or the context is cancelled.
	Run(ctx context.Context, opts RunOptions) error

	// Build builds a container image from a Dockerfile/Containerfile.
	Build(ctx context.Context, opts BuildOptions) error

	// Pull fetches a container image from a remote registry.
	Pull(ctx context.Context, ref string) error

	// Push uploads a container image to a remote registry.
	Push(ctx context.Context, ref string) error

	// Import loads a container image from a local OCI tarball or directory.
	Import(ctx context.Context, path string, ref string) error

	// Inspect returns metadata about a local container image.
	// Returns an error if the image is not found locally.
	Inspect(ctx context.Context, ref string) (*ImageInfo, error)

	// Remove deletes a local container image.
	Remove(ctx context.Context, ref string) error
}

// RunOptions configures how a container is created and started.
type RunOptions struct {
	// Image is the container image reference to run (required).
	Image string

	// Name is the optional container name. If empty, the runtime assigns one.
	Name string

	// Interactive attaches stdin to the container process.
	Interactive bool

	// TTY allocates a pseudo-TTY for the container.
	TTY bool

	// Remove automatically removes the container when it exits.
	Remove bool

	// Env specifies environment variables to set inside the container.
	// Keys are variable names, values are variable values.
	Env map[string]string

	// EnvPassthrough lists environment variable names to pass through from
	// the host environment into the container. Variables that are not set
	// on the host are silently ignored.
	EnvPassthrough []string

	// Mounts configures filesystem mounts into the container.
	Mounts []Mount

	// Ports configures port mappings between the host and the container.
	Ports []PortMapping

	// Workdir sets the working directory inside the container.
	Workdir string

	// Entrypoint overrides the container image's default entrypoint.
	Entrypoint []string

	// Command specifies the command and arguments to run inside the container.
	// If Entrypoint is also set, Command provides arguments to the entrypoint.
	Command []string

	// GPUDevices lists GPU device paths or identifiers to expose to the container.
	// The format is runtime-specific (e.g. "/dev/nvidia0" for podman, "all" for docker).
	GPUDevices []string

	// GPURuntime specifies the GPU runtime to use. Supported values are "nvidia" and "amd".
	// When set, the runtime will configure the appropriate GPU passthrough mechanism.
	GPURuntime string

	// ResourceLimits constrains the CPU, memory, and other resources available
	// to the container. Nil means no limits beyond the runtime defaults.
	ResourceLimits *ResourceLimits

	// NetworkFlags specifies additional network configuration flags derived
	// from network policy rules. These are passed directly to the runtime.
	NetworkFlags []string

	// SecretFiles configures secret file mounts into the container.
	// Secrets are mounted read-only and are not included in image layers.
	SecretFiles []SecretMount

	// Labels sets metadata labels on the container.
	Labels map[string]string

	// ExtraArgs provides an escape hatch for runtime-specific flags that are
	// not covered by the structured options. These are appended to the
	// runtime command line verbatim.
	ExtraArgs []string
}

// BuildOptions configures how a container image is built.
type BuildOptions struct {
	// ContextDir is the build context directory (required).
	ContextDir string

	// Dockerfile is the path to the Dockerfile/Containerfile relative to
	// ContextDir. Defaults to "Dockerfile" if empty.
	Dockerfile string

	// Tags lists the image tags to apply to the built image.
	Tags []string

	// BuildArgs specifies build-time variables.
	BuildArgs map[string]string

	// Target specifies a multi-stage build target.
	Target string

	// NoCache disables the build cache.
	NoCache bool

	// Pull forces pulling the base image before building.
	Pull bool

	// Labels sets metadata labels on the built image.
	Labels map[string]string
}

// Mount describes a filesystem mount into a container.
type Mount struct {
	// Type is the mount type: "bind", "volume", or "tmpfs".
	Type string

	// Source is the host path (for bind mounts) or volume name (for volume mounts).
	// Ignored for tmpfs mounts.
	Source string

	// Target is the mount point inside the container.
	Target string

	// Options are mount-specific options (e.g. "ro", "noexec", "size=64m").
	Options []string
}

// PortMapping describes a port forwarding rule between the host and a container.
type PortMapping struct {
	// HostPort is the port on the host to listen on.
	HostPort int

	// ContainerPort is the port inside the container to forward to.
	ContainerPort int

	// Protocol is the transport protocol: "tcp" (default) or "udp".
	Protocol string

	// HostIP is the host interface IP to bind to. Empty means all interfaces.
	HostIP string
}

// SecretMount describes a secret file to be mounted into a container.
// Secret mounts are read-only and are not baked into image layers.
type SecretMount struct {
	// Source is the host path to the secret file.
	Source string

	// Target is the path where the secret is mounted inside the container.
	Target string

	// Mode is the file permission mode string (e.g. "0400"). Defaults to "0444".
	Mode string

	// Env optionally exports the secret file contents as an environment variable
	// with this name. If empty, the secret is only available as a mounted file.
	Env string
}

// ResourceLimits constrains the resources available to a container.
type ResourceLimits struct {
	// CPUs is the number of CPUs available to the container (e.g. "2.0").
	// Empty means no CPU limit.
	CPUs string

	// MemoryBytes is the memory limit in bytes. Zero means no memory limit.
	MemoryBytes int64

	// MemorySwapBytes is the swap limit in bytes. Zero means no swap limit.
	// Set equal to MemoryBytes to disable swap.
	MemorySwapBytes int64

	// PidsLimit is the maximum number of processes. Zero means no limit.
	PidsLimit int64

	// ReadonlyRootfs makes the container's root filesystem read-only.
	ReadonlyRootfs bool

	// NoNewPrivileges prevents the container from gaining additional privileges.
	NoNewPrivileges bool
}

// ImageInfo holds metadata about a container image.
type ImageInfo struct {
	// ID is the image's unique identifier (typically a digest).
	ID string

	// RepoTags lists the image's repository tags.
	RepoTags []string

	// Size is the image size in bytes.
	Size int64

	// Created is the image creation timestamp.
	Created time.Time

	// Architecture is the image's target CPU architecture.
	Architecture string

	// OS is the image's target operating system.
	OS string

	// Labels are the image's metadata labels.
	Labels map[string]string
}

// HealthCheck describes how to verify that a process is healthy and ready.
type HealthCheck struct {
	// Command is the health check command to execute.
	// If it exits 0, the process is considered healthy.
	Command []string

	// Interval is the time between consecutive health checks.
	Interval time.Duration

	// Timeout is the maximum time to wait for a single health check to complete.
	Timeout time.Duration

	// Retries is the number of consecutive failures before the process
	// is considered unhealthy.
	Retries int

	// StartPeriod is the grace period after process start during which
	// health check failures are not counted toward the retry limit.
	StartPeriod time.Duration
}

// ProcessStatus represents the current state of a managed process.
type ProcessStatus struct {
	// Name is the process name.
	Name string

	// PID is the operating system process ID. Zero if the process is not running.
	PID int

	// State is the current state: "running", "healthy", "unhealthy", "stopped", "failed".
	State string

	// Restarts is the number of times this process has been restarted.
	Restarts int

	// LastError is the most recent error encountered, if any.
	LastError error

	// ExitCode is the last exit code. -1 if the process has not exited.
	ExitCode int
}
