package harness

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// GooseAdapter implements Adapter for the Block/Goose agent.
type GooseAdapter struct{}

// Name returns the harness identifier.
func (a *GooseAdapter) Name() string {
	return "goose"
}

// DefaultEntrypoint returns the default command to start Goose.
func (a *GooseAdapter) DefaultEntrypoint() []string {
	return []string{"goose", "session"}
}

// ValidateConfig checks if the config contains required settings for Goose.
func (a *GooseAdapter) ValidateConfig(cfg *HarnessConfig) error {
	if cfg == nil {
		return fmt.Errorf("config must not be nil")
	}
	if cfg.Model.Name == "" {
		return fmt.Errorf("model name is required for goose")
	}
	return nil
}

// GenerateConfig generates the goose config.yaml configuration file.
func (a *GooseAdapter) GenerateConfig(cfg *HarnessConfig) (map[string][]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}

	config := make(map[string]interface{})

	if len(cfg.MCPs) > 0 {
		extensions := make(map[string]interface{})
		for _, srv := range cfg.MCPs {
			extConfig := map[string]interface{}{
				"name": srv.Name,
				"cmd":  srv.Command,
				"args": srv.Args,
			}
			if srv.Command == "" && srv.Source != "" {
				extConfig["cmd"] = fmt.Sprintf("/opt/agentbox/mcp/%s/run.sh", srv.Name)
				extConfig["args"] = []string{}
			}
			if len(srv.Env) > 0 {
				extConfig["envs"] = srv.Env
			}
			extensions[srv.Name] = extConfig
		}
		config["extensions"] = extensions
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshaling goose config: %w", err)
	}

	return map[string][]byte{
		"/opt/agentbox/config/harness/goose.yaml": data,
	}, nil
}
