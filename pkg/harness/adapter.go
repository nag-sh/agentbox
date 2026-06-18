package harness

// Adapter generates harness-specific configuration files from a harness-agnostic
// HarnessConfig. This decouples harness adapters from both the agentbox manifest
// schema and OCX component schemas.
type Adapter interface {
	// Name returns the harness name (e.g., "opencode", "goose").
	Name() string

	// GenerateConfig produces the configuration files needed by the harness.
	// Keys are absolute paths in the container, values are file contents.
	GenerateConfig(cfg *HarnessConfig) (map[string][]byte, error)

	// ValidateConfig checks if the harness config contains required settings.
	ValidateConfig(cfg *HarnessConfig) error

	// DefaultEntrypoint returns the command to start the harness.
	DefaultEntrypoint() []string
}
