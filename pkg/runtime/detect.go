package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/log"
)

// runtimeOrder is the detection priority. Podman is preferred for its
// rootless capabilities and daemonless architecture.
var runtimeOrder = []string{"podman", "docker", "nerdctl"}

// envOverride is the environment variable that allows users to force a
// specific container runtime.
const envOverride = "AGENTBOX_RUNTIME"

// Detect auto-detects the best available container runtime on the system.
//
// The detection strategy is:
//  1. If the AGENTBOX_RUNTIME environment variable is set, use that runtime.
//  2. Check for podman first (preferred for rootless container support).
//  3. Fall back to docker.
//  4. Fall back to nerdctl (containerd's CLI).
//
// Each candidate runtime is verified with a version check to ensure the
// binary is functional and not just present on $PATH.
func Detect(ctx context.Context) (Runtime, error) {
	logger := log.Default()

	// Check for explicit override.
	if override := os.Getenv(envOverride); override != "" {
		logger.Info("runtime override", "env", envOverride, "value", override)
		rt, err := ForName(override)
		if err != nil {
			return nil, fmt.Errorf("AGENTBOX_RUNTIME=%s: %w", override, err)
		}
		if err := verify(ctx, rt); err != nil {
			return nil, fmt.Errorf("AGENTBOX_RUNTIME=%s: %w", override, err)
		}
		return rt, nil
	}

	// Auto-detect in priority order.
	var lastErr error
	for _, name := range runtimeOrder {
		rt, err := ForName(name)
		if err != nil {
			lastErr = err
			continue
		}

		if err := verify(ctx, rt); err != nil {
			logger.Debug("runtime not available", "name", name, "error", err)
			lastErr = err
			continue
		}

		logger.Info("detected runtime", "name", rt.Name())
		return rt, nil
	}

	return nil, fmt.Errorf("no container runtime found (tried %s): %w",
		strings.Join(runtimeOrder, ", "), lastErr)
}

// ForName returns a Runtime implementation for the given runtime name.
//
// Supported names are "podman", "docker", and "nerdctl". The name is
// case-insensitive. Returns an error if the name is not recognized.
//
// Note: ForName does not verify that the runtime binary exists or is functional.
// Call [Detect] for auto-detection with verification, or call [Runtime.Version]
// to verify manually.
func ForName(name string) (Runtime, error) {
	logger := log.Default()

	switch strings.ToLower(strings.TrimSpace(name)) {
	case "podman":
		return NewPodmanRuntime("", logger), nil
	case "docker":
		return NewDockerRuntime("", logger), nil
	case "nerdctl":
		// nerdctl is API-compatible with docker, so we reuse the docker
		// implementation with a different binary name.
		return NewDockerRuntime("nerdctl", logger), nil
	default:
		return nil, fmt.Errorf("unsupported runtime: %q (supported: %s)",
			name, strings.Join(runtimeOrder, ", "))
	}
}

// verify checks that a runtime binary is available and functional by
// running its version command.
func verify(ctx context.Context, rt Runtime) error {
	// First check that the binary exists on PATH.
	binaryName := rt.Name()
	if binaryName == "docker" {
		// The DockerRuntime might have been created with "nerdctl" binary.
		if dr, ok := rt.(*DockerRuntime); ok {
			binaryName = dr.binary
		}
	}

	if _, err := exec.LookPath(binaryName); err != nil {
		return fmt.Errorf("%s not found in PATH: %w", binaryName, err)
	}

	// Verify it actually works.
	version, err := rt.Version(ctx)
	if err != nil {
		return fmt.Errorf("%s version check failed: %w", binaryName, err)
	}

	log.Debug("runtime verified", "name", rt.Name(), "version", version)
	return nil
}
