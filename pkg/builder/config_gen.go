package builder

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"gopkg.in/yaml.v3"

	"github.com/nag-sh/agentbox/pkg/guardrails"
	"github.com/nag-sh/agentbox/pkg/harness"
	agentboxinit "github.com/nag-sh/agentbox/pkg/init"
	"github.com/nag-sh/agentbox/pkg/manifest"
	"github.com/nag-sh/agentbox/pkg/network"
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
	allConfigs, err := g.generateCommonConfig(m, adapter)
	if err != nil {
		return nil, err
	}
	for path, content := range configs {
		allConfigs[path] = content
	}

	return allConfigs, nil
}

// generateCommonConfig produces configuration files common to all harnesses.
func (g *ConfigGenerator) generateCommonConfig(m *manifest.Manifest, adapter harness.Adapter) (map[string][]byte, error) {
	configs := make(map[string][]byte)

	// Save the full original manifest inside the container for reference.
	if manifestData, err := json.MarshalIndent(m, "", "  "); err == nil {
		configs["/opt/agentbox/config/agentbox.json"] = manifestData
	}

	// Guardrail configuration in the format the init engine expects.
	guardConfig := guardrails.FromManifest(m.Spec.Guardrails)
	if guardrailsData, err := yaml.Marshal(&guardConfig); err == nil {
		configs["/opt/agentbox/config/guardrails.yaml"] = guardrailsData
	}

	// Network policy configuration for the init engine.
	netPolicy := network.FromManifest(m.Spec.Network)
	if networkData, err := yaml.Marshal(&netPolicy); err == nil {
		configs["/opt/agentbox/config/network.yaml"] = networkData
	}

	// Runtime configuration consumed by agentbox-agentboxinit.
	runtimeConfig, err := runtimeConfigFromManifest(m, adapter)
	if err != nil {
		return nil, fmt.Errorf("generating runtime config: %w", err)
	}
	if runtimeData, err := yaml.Marshal(&runtimeConfig); err == nil {
		configs["/opt/agentbox/config/runtime.yaml"] = runtimeData
	}

	return configs, nil
}

// runtimeConfigFromManifest builds the init RuntimeConfig from the manifest.
func runtimeConfigFromManifest(m *manifest.Manifest, adapter harness.Adapter) (agentboxinit.RuntimeConfig, error) {
	entrypoint := adapter.DefaultEntrypoint()
	if len(entrypoint) == 0 {
		return agentboxinit.RuntimeConfig{}, fmt.Errorf("harness adapter %q returned empty entrypoint", adapter.Name())
	}

	harnessEnv := make(map[string]string)
	for k, v := range m.Spec.Runtime.Env {
		harnessEnv[k] = v
	}

	requiredEnv := []string{}
	if m.Spec.Model.APIKeyEnv != "" {
		requiredEnv = append(requiredEnv, m.Spec.Model.APIKeyEnv)
	}

	mcpServers := make([]agentboxinit.MCPServerConfig, 0, len(m.Spec.MCP.Servers))
	for _, srv := range m.Spec.MCP.Servers {
		cfg := agentboxinit.MCPServerConfig{
			Name:        srv.Name,
			Env:         srv.Env,
			MaxRestarts: 3,
		}
		if srv.Command != "" {
			cfg.Command = srv.Command
			cfg.Args = srv.Args
		} else {
			cfg.Command = fmt.Sprintf("/opt/agentbox/mcp/%s/run.sh", srv.Name)
		}
		if srv.HealthCheck != nil {
			cfg.HealthCheck = &agentboxinit.HealthCheckConfig{
				Command:  strings.Fields(srv.HealthCheck.Command),
				Interval: manifest.Duration{Duration: srv.HealthCheck.Interval.Duration},
				Timeout:  manifest.Duration{Duration: srv.HealthCheck.Timeout.Duration},
				Retries:  srv.HealthCheck.Retries,
			}
		}
		mcpServers = append(mcpServers, cfg)
	}

	secrets := make([]agentboxinit.SecretConfig, 0, len(m.Spec.Secrets.Files))
	for _, f := range m.Spec.Secrets.Files {
		secrets = append(secrets, agentboxinit.SecretConfig{
			Path:   f.Target,
			EnvVar: f.Env,
		})
	}

	return agentboxinit.RuntimeConfig{
		Harness: agentboxinit.HarnessConfig{
			Command: entrypoint[0],
			Args:    entrypoint[1:],
			Env:     harnessEnv,
			Workdir: m.Spec.Runtime.Workdir,
		},
		MCPServers:  mcpServers,
		RequiredEnv: requiredEnv,
		Secrets:     secrets,
	}, nil
}
