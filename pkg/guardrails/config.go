package guardrails

import (
	"fmt"
	"strconv"

	"github.com/nag-sh/agentbox/pkg/manifest"
)

// FromManifest converts the user-facing manifest guardrail specification into
// the engine's internal configuration format.
func FromManifest(g manifest.GuardrailsSpec) GuardrailConfig {
	cpu := ""
	if g.Resources.MaxCPUs != 0 {
		cpu = strconv.FormatFloat(g.Resources.MaxCPUs, 'f', -1, 64)
	}

	return GuardrailConfig{
		Commands: CommandRules{
			Allow:           g.Commands.Allow,
			Deny:            g.Commands.Deny,
			DefaultPolicy:   g.Commands.DefaultPolicy,
			MaxExecutionTime: g.Commands.MaxExecutionTime.Duration,
		},
		Filesystem: FilesystemRules{
			WritablePaths: g.Filesystem.Writable,
			ReadablePaths: g.Filesystem.Readable,
			DeniedPaths:   g.Filesystem.Deny,
			DefaultPolicy: g.Filesystem.DefaultPolicy,
		},
		Resources: ResourceLimits{
			Memory: g.Resources.MaxMemory,
			CPU:    cpu,
			Pids:   g.Resources.MaxProcesses,
			Nofile: g.Resources.MaxOpenFiles,
		},
	}
}

// ToRuntimeFlags returns the container runtime flags derived from the manifest
// guardrail configuration.
func ToRuntimeFlags(g manifest.GuardrailsSpec) []string {
	return NewResourceLimiter(FromManifest(g).Resources).RuntimeFlags()
}

// ValidateManifest validates that the guardrail resource fields can be
// translated into the engine's configuration.
func ValidateManifest(g manifest.GuardrailsSpec) error {
	if g.Resources.MaxMemory != "" {
		if _, err := ParseSize(g.Resources.MaxMemory); err != nil {
			return fmt.Errorf("invalid maxMemory %q: %w", g.Resources.MaxMemory, err)
		}
	}
	if g.Resources.MaxCPUs < 0 {
		return fmt.Errorf("maxCpus must be non-negative")
	}
	if g.Resources.MaxProcesses < 0 {
		return fmt.Errorf("maxProcesses must be non-negative")
	}
	if g.Resources.MaxOpenFiles < 0 {
		return fmt.Errorf("maxOpenFiles must be non-negative")
	}
	return nil
}
