package harness

import (
	"github.com/nag-sh/agentbox/pkg/manifest"
)

// Adapter generates harness-specific configuration files from the
// unified agentbox build manifest.
type Adapter interface {
	// Name returns the harness name (e.g., "opencode", "goose").
	Name() string

	// GenerateConfig produces the configuration files needed by the harness.
	// Keys are absolute paths in the container, values are file contents.
	GenerateConfig(m *manifest.Manifest) (map[string][]byte, error)

	// ValidateManifest checks if the manifest contains required settings for this harness.
	ValidateManifest(m *manifest.Manifest) error

	// DefaultEntrypoint returns the command to start the harness.
	DefaultEntrypoint() []string
}
