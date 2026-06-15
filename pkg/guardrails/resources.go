package guardrails

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/charmbracelet/log"
)

// ResourceLimits defines hard resource constraints for the container. These
// are translated into container runtime flags (for docker/podman) and/or
// cgroup settings applied at container init time.
type ResourceLimits struct {
	// Memory is the maximum memory the container may use, expressed as a
	// human-readable size string (e.g., "4Gi", "512Mi", "256m").
	Memory string `yaml:"memory"`

	// CPU is the CPU limit expressed as a fractional core count (e.g., "2.0"
	// for two full cores, "0.5" for half a core).
	CPU string `yaml:"cpu"`

	// Pids is the maximum number of processes (PIDs) the container may create.
	Pids int `yaml:"pids"`

	// Nofile is the maximum number of open file descriptors permitted.
	Nofile int `yaml:"nofile"`
}

// ResourceLimiter translates [ResourceLimits] into container runtime flags
// and applies cgroup/rlimit settings within the container.
//
// ResourceLimiter is safe for concurrent use from multiple goroutines once
// constructed; its internal state is read-only after initialization.
type ResourceLimiter struct {
	limits ResourceLimits
	logger *log.Logger
}

// NewResourceLimiter constructs a new [ResourceLimiter] from the supplied
// [ResourceLimits] configuration.
func NewResourceLimiter(limits ResourceLimits) *ResourceLimiter {
	logger := log.Default().With("component", "guardrails.resources")
	return &ResourceLimiter{
		limits: limits,
		logger: logger,
	}
}

// Apply applies the configured resource limits within the running container.
// It sets rlimits for nofile and logs the actions taken. Memory and CPU
// limits are typically enforced by the container runtime via [RuntimeFlags]
// rather than from within the container, but this method handles any limits
// that can be applied from within (e.g., RLIMIT_NOFILE).
func (rl *ResourceLimiter) Apply() error {
	if rl.limits.Nofile > 0 {
		rl.logger.Info("setting RLIMIT_NOFILE", "limit", rl.limits.Nofile)
		if err := setRlimitNofile(uint64(rl.limits.Nofile)); err != nil {
			return fmt.Errorf("setting RLIMIT_NOFILE to %d: %w", rl.limits.Nofile, err)
		}
	}

	// Memory and CPU limits are enforced by the container runtime and cannot
	// be meaningfully set from within the container. Log them for visibility.
	if rl.limits.Memory != "" {
		bytes, err := ParseSize(rl.limits.Memory)
		if err != nil {
			return fmt.Errorf("parsing memory limit %q: %w", rl.limits.Memory, err)
		}
		rl.logger.Info("memory limit configured (enforced by runtime)", "bytes", bytes, "raw", rl.limits.Memory)

		// Attempt to write to the cgroup memory limit if the cgroup filesystem
		// is available (works inside containers with cgroup v2).
		if err := writeCgroupMemoryLimit(bytes); err != nil {
			rl.logger.Debug("cgroup memory limit not writable (expected if enforced by runtime)", "err", err)
		}
	}

	if rl.limits.CPU != "" {
		rl.logger.Info("CPU limit configured (enforced by runtime)", "cpu", rl.limits.CPU)
	}

	if rl.limits.Pids > 0 {
		rl.logger.Info("PID limit configured (enforced by runtime)", "pids", rl.limits.Pids)
	}

	return nil
}

// RuntimeFlags returns the container runtime flags (compatible with both
// docker and podman) needed to enforce the configured resource limits. The
// returned slice can be appended to a container run command.
func (rl *ResourceLimiter) RuntimeFlags() []string {
	var flags []string

	if rl.limits.Memory != "" {
		bytes, err := ParseSize(rl.limits.Memory)
		if err == nil {
			flags = append(flags, "--memory", fmt.Sprintf("%d", bytes))
		}
	}

	if rl.limits.CPU != "" {
		flags = append(flags, "--cpus", rl.limits.CPU)
	}

	if rl.limits.Pids > 0 {
		flags = append(flags, "--pids-limit", strconv.Itoa(rl.limits.Pids))
	}

	if rl.limits.Nofile > 0 {
		flags = append(flags, "--ulimit", fmt.Sprintf("nofile=%d:%d", rl.limits.Nofile, rl.limits.Nofile))
	}

	return flags
}

// ParseSize parses a human-readable size string into bytes. It supports the
// following suffixes (case-insensitive):
//
//   - IEC binary: Ki, Mi, Gi, Ti (powers of 1024)
//   - SI decimal: K, M, G, T (powers of 1000)
//   - Docker-style: k, m, g (powers of 1024)
//   - Plain bytes: no suffix
//
// Examples: "4Gi" → 4294967296, "512Mi" → 536870912, "256m" → 268435456.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// Identify and strip the suffix.
	type multiplier struct {
		suffix string
		factor int64
	}

	// Order matters: longer suffixes must be checked before shorter ones.
	multipliers := []multiplier{
		{"Ti", 1 << 40},
		{"Gi", 1 << 30},
		{"Mi", 1 << 20},
		{"Ki", 1 << 10},
		{"T", 1_000_000_000_000},
		{"G", 1_000_000_000},
		{"M", 1_000_000},
		{"K", 1_000},
		{"t", 1 << 40},
		{"g", 1 << 30},
		{"m", 1 << 20},
		{"k", 1 << 10},
	}

	for _, m := range multipliers {
		if strings.HasSuffix(s, m.suffix) {
			numStr := strings.TrimSuffix(s, m.suffix)
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("invalid numeric value %q in size %q: %w", numStr, s, err)
			}
			return int64(num * float64(m.factor)), nil
		}
	}

	// No suffix — treat as raw bytes.
	num, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return num, nil
}

// setRlimitNofile sets the RLIMIT_NOFILE (maximum open file descriptors)
// soft and hard limits for the current process.
func setRlimitNofile(limit uint64) error {
	rlim := &syscall.Rlimit{
		Cur: limit,
		Max: limit,
	}
	return syscall.Setrlimit(syscall.RLIMIT_NOFILE, rlim)
}

// writeCgroupMemoryLimit attempts to write the memory limit to the cgroup v2
// memory controller. This is a best-effort operation — it will fail silently
// if the cgroup filesystem is not available or not writable.
func writeCgroupMemoryLimit(bytes int64) error {
	const cgroupMemMaxPath = "/sys/fs/cgroup/memory.max"

	data := []byte(strconv.FormatInt(bytes, 10))
	return os.WriteFile(cgroupMemMaxPath, data, 0644)
}
