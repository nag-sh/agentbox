package harness

import (
	"encoding/json"
	"fmt"
)

// OpenCodeAdapter implements Adapter for the OpenCode agent.
type OpenCodeAdapter struct{}

// Name returns the harness identifier.
func (a *OpenCodeAdapter) Name() string {
	return "opencode"
}

// DefaultEntrypoint returns the default command to start OpenCode.
func (a *OpenCodeAdapter) DefaultEntrypoint() []string {
	return []string{"opencode"}
}

// ValidateConfig checks if the config contains required settings for OpenCode.
func (a *OpenCodeAdapter) ValidateConfig(cfg *HarnessConfig) error {
	if cfg == nil {
		return fmt.Errorf("config must not be nil")
	}
	if cfg.Model.Name == "" {
		return fmt.Errorf("model name is required for opencode")
	}
	return nil
}

// GenerateConfig generates the opencode.json configuration file from a
// harness-agnostic config, then deep-merges any OCX opencode block on top.
func (a *OpenCodeAdapter) GenerateConfig(cfg *HarnessConfig) (map[string][]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config must not be nil")
	}

	config := make(map[string]interface{})

	provider := cfg.Model.Provider
	modelName := cfg.Model.Name
	modelRef := modelName
	if provider != "" && provider != "custom" && provider != "ollama" {
		modelRef = fmt.Sprintf("%s/%s", provider, modelName)
	}
	config["model"] = modelRef

	if cfg.Model.APIKeyEnv != "" {
		providerMap := map[string]interface{}{
			"options": map[string]interface{}{
				"apiKey": fmt.Sprintf("{env:%s}", cfg.Model.APIKeyEnv),
			},
		}
		if cfg.Model.BaseURL != "" {
			providerMap["options"].(map[string]interface{})["baseURL"] = cfg.Model.BaseURL
		}

		config["provider"] = map[string]interface{}{
			provider: providerMap,
		}
	}

	if len(cfg.MCPs) > 0 {
		mcpConfig := make(map[string]interface{})
		for _, srv := range cfg.MCPs {
			srvConfig := map[string]interface{}{
				"type": "local",
			}
			if srv.Command != "" {
				srvConfig["command"] = append([]string{srv.Command}, srv.Args...)
			} else if srv.Source != "" {
				srvConfig["command"] = []string{fmt.Sprintf("/opt/agentbox/mcp/%s/run.sh", srv.Name)}
			}
			if len(srv.Env) > 0 {
				srvConfig["env"] = srv.Env
			}
			mcpConfig[srv.Name] = srvConfig
		}
		config["mcp"] = mcpConfig
	}

	if len(cfg.Tools.Permissions) > 0 {
		toolsConfig := make(map[string]interface{})
		for _, p := range cfg.Tools.Permissions {
			toolsConfig[p.Tool] = p.Allow
		}
		config["tools"] = toolsConfig
	}

	if cfg.Opencode != nil {
		config = deepMergeMaps(config, cfg.Opencode).(map[string]interface{})
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling opencode config: %w", err)
	}

	return map[string][]byte{
		"/opt/agentbox/config/harness/opencode.json": data,
	}, nil
}

func deepMergeMaps(dst, src interface{}) interface{} {
	srcMap, srcIsMap := src.(map[string]interface{})
	dstMap, dstIsMap := dst.(map[string]interface{})
	if srcIsMap && dstIsMap {
		out := make(map[string]interface{}, len(dstMap))
		for k, v := range dstMap {
			out[k] = v
		}
		for k, v := range srcMap {
			out[k] = deepMergeMaps(out[k], v)
		}
		return out
	}

	srcSlice, srcIsSlice := src.([]interface{})
	dstSlice, dstIsSlice := dst.([]interface{})
	if srcIsSlice && dstIsSlice {
		return append(dstSlice, srcSlice...)
	}

	return src
}

