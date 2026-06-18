package builder

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/log"
	"gopkg.in/yaml.v3"

	"github.com/nag-sh/agentbox/pkg/harness"
	agentboxinit "github.com/nag-sh/agentbox/pkg/init"
	"github.com/nag-sh/agentbox/pkg/manifest"
	"github.com/nag-sh/agentbox/pkg/ocx"
)

// ConfigGenerator orchestrates configuration file generation by delegating
// to the appropriate harness adapter based on a normalized HarnessConfig.
type ConfigGenerator struct {
	adapters   map[string]harness.Adapter
	normalizer *ocx.Normalizer
	logger     *log.Logger
}

// NewConfigGenerator creates a new ConfigGenerator.
func NewConfigGenerator() *ConfigGenerator {
	return &ConfigGenerator{
		adapters:   make(map[string]harness.Adapter),
		normalizer: ocx.NewNormalizer(),
		logger:     log.Default(),
	}
}

// RegisterAdapter registers a harness adapter.
func (g *ConfigGenerator) RegisterAdapter(name string, adapter harness.Adapter) {
	g.adapters[name] = adapter
}

// Generate produces all configuration files for the harness. If resolved is
// non-nil, the OCX components are merged into the normalized HarnessConfig.
func (g *ConfigGenerator) Generate(m *manifest.Manifest, resolved *ocx.ResolvedSet) (map[string][]byte, error) {
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

	cfg, err := g.normalizer.Normalize(m, resolved)
	if err != nil {
		return nil, fmt.Errorf("normalizing harness config: %w", err)
	}

	if err := adapter.ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("harness config validation failed for %q: %w", harnessName, err)
	}

	configs, err := adapter.GenerateConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("generating config for harness %q: %w", harnessName, err)
	}

	allConfigs, err := g.generateCommonConfig(cfg, adapter)
	if err != nil {
		return nil, err
	}
	for path, content := range configs {
		allConfigs[path] = content
	}

	return allConfigs, nil
}

// generateCommonConfig produces configuration files common to all harnesses.
func (g *ConfigGenerator) generateCommonConfig(cfg *harness.HarnessConfig, adapter harness.Adapter) (map[string][]byte, error) {
	configs := make(map[string][]byte)

	// Save the full normalized config inside the container for reference.
	if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		configs["/opt/agentbox/config/agentbox.json"] = data
	}

	// Guardrail configuration in the format the init engine expects.
	guardConfig := cfg.Guardrails
	if guardrailsData, err := yaml.Marshal(&guardConfig); err == nil {
		configs["/opt/agentbox/config/guardrails.yaml"] = guardrailsData
	}

	// Network policy configuration for the init engine.
	if networkData, err := yaml.Marshal(&cfg.Network); err == nil {
		configs["/opt/agentbox/config/network.yaml"] = networkData
	}

	// Runtime configuration consumed by agentbox-init.
	runtimeConfig, err := runtimeConfigFromHarnessConfig(cfg, adapter)
	if err != nil {
		return nil, fmt.Errorf("generating runtime config: %w", err)
	}
	if runtimeData, err := yaml.Marshal(&runtimeConfig); err == nil {
		configs["/opt/agentbox/config/runtime.yaml"] = runtimeData
	}

	return configs, nil
}

// runtimeConfigFromHarnessConfig builds the init RuntimeConfig from the
// normalized harness config.
func runtimeConfigFromHarnessConfig(cfg *harness.HarnessConfig, adapter harness.Adapter) (agentboxinit.RuntimeConfig, error) {
	entrypoint := adapter.DefaultEntrypoint()
	if len(entrypoint) == 0 {
		return agentboxinit.RuntimeConfig{}, fmt.Errorf("harness adapter %q returned empty entrypoint", adapter.Name())
	}

	harnessEnv := make(map[string]string)
	for k, v := range cfg.Runtime.Env {
		harnessEnv[k] = v
	}

	requiredEnv := []string{}
	if cfg.Model.APIKeyEnv != "" {
		requiredEnv = append(requiredEnv, cfg.Model.APIKeyEnv)
	}

	mcpServers := make([]agentboxinit.MCPServerConfig, 0, len(cfg.MCPs))
	for _, srv := range cfg.MCPs {
		c := agentboxinit.MCPServerConfig{
			Name:        srv.Name,
			Env:         srv.Env,
			MaxRestarts: 3,
		}
		if srv.Command != "" {
			c.Command = srv.Command
			c.Args = srv.Args
		} else {
			c.Command = fmt.Sprintf("/opt/agentbox/mcp/%s/run.sh", srv.Name)
		}
		if srv.HealthCheck != nil {
			c.HealthCheck = &agentboxinit.HealthCheckConfig{
				Command:  splitShellCommand(srv.HealthCheck.Command),
				Interval: manifest.Duration{Duration: srv.HealthCheck.Interval},
				Timeout:  manifest.Duration{Duration: srv.HealthCheck.Timeout},
				Retries:  srv.HealthCheck.Retries,
			}
		}
		mcpServers = append(mcpServers, c)
	}

	secrets := make([]agentboxinit.SecretConfig, 0, len(cfg.Secrets.Files))
	for _, f := range cfg.Secrets.Files {
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
			Workdir: cfg.Runtime.Workdir,
		},
		MCPServers:  mcpServers,
		RequiredEnv: requiredEnv,
		Secrets:     secrets,
	}, nil
}

func splitShellCommand(s string) []string {
	var args []string
	var current strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		if current.Len() > 0 {
			args = append(args, current.String())
			current.Reset()
		}
	}

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}
		if r == '"' || r == '\'' {
			quote = r
			continue
		}
		if unicode.IsSpace(r) {
			flush()
			continue
		}
		current.WriteRune(r)
	}
	flush()
	return args
}
