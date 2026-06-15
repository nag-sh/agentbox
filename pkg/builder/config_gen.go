package builder

import (
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/nag-sh/agentbox/pkg/harness"
	"github.com/nag-sh/agentbox/pkg/manifest"
)

// ConfigGenerator orchestrates configuration file generation by delegating
// to the appropriate HarnessAdapter based on the manifest.
type ConfigGenerator struct {
	adapters map[string]harness.Adapter
	logger   *log.Logger
}

// NewConfigGenerator creates a new ConfigGenerator.
func NewConfigGenerator() *ConfigGenerator {
	return &ConfigGenerator{
		adapters: make(map[string]harness.Adapter),
		logger:   log.Default(),
	}
}

// RegisterAdapter registers a HarnessAdapter.
func (g *ConfigGenerator) RegisterAdapter(name string, adapter harness.Adapter) {
	g.adapters[name] = adapter
}

// Generate produces all configuration files for the harness.
func (g *ConfigGenerator) Generate(m *manifest.Manifest) (map[string][]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("manifest must not be nil")
	}

	harnessName := string(m.Spec.Harness.Name)
	if harnessName == "" {
		return nil, fmt.Errorf("manifest does not specify a harness")
	}

	adapter, ok := g.adapters[harnessName]
	if !ok {
		return nil, fmt.Errorf("no adapter registered for harness %q", harnessName)
	}

	if err := adapter.ValidateManifest(m); err != nil {
		return nil, fmt.Errorf("manifest validation failed for harness %q: %w", harnessName, err)
	}

	configs, err := adapter.GenerateConfig(m)
	if err != nil {
		return nil, fmt.Errorf("generating config for harness %q: %w", harnessName, err)
	}

	// Merge with common agentbox configuration.
	allConfigs := g.generateCommonConfig(m)
	for path, content := range configs {
		allConfigs[path] = content
	}

	return allConfigs, nil
}

// generateCommonConfig produces configuration files common to all harnesses.
func (g *ConfigGenerator) generateCommonConfig(m *manifest.Manifest) map[string][]byte {
	configs := make(map[string][]byte)

	// Save the full original manifest inside the container for reference
	if manifestData, err := json.MarshalIndent(m, "", "  "); err == nil {
		configs["/opt/agentbox/config/agentbox.json"] = manifestData
	}
	
	// Create guardrails config for init system
	if guardrailsData, err := json.MarshalIndent(m.Spec.Guardrails, "", "  "); err == nil {
		configs["/opt/agentbox/config/guardrails.json"] = guardrailsData
	}
	
	// Create network policy config for init system
	if networkData, err := json.MarshalIndent(m.Spec.Network, "", "  "); err == nil {
		configs["/opt/agentbox/config/network.json"] = networkData
	}

	return configs
}
