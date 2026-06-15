package harness

import (
	"encoding/json"
	"fmt"

	"github.com/nag-sh/agentbox/pkg/manifest"
)

// OpenCodeAdapter implements harness.Adapter for the OpenCode agent.
type OpenCodeAdapter struct{}

// Name returns the harness identifier.
func (a *OpenCodeAdapter) Name() string {
	return string(manifest.HarnessOpenCode)
}

// DefaultEntrypoint returns the default command to start OpenCode.
func (a *OpenCodeAdapter) DefaultEntrypoint() []string {
	return []string{"opencode"}
}

// ValidateManifest checks if the manifest contains required settings for OpenCode.
func (a *OpenCodeAdapter) ValidateManifest(m *manifest.Manifest) error {
	// OpenCode needs an API key or an explicit model config
	if m.Spec.Model.Name == "" {
		return fmt.Errorf("model name is required for opencode")
	}
	return nil
}

// GenerateConfig generates the opencode.json configuration file.
func (a *OpenCodeAdapter) GenerateConfig(m *manifest.Manifest) (map[string][]byte, error) {
	// Build the opencode JSON structure
	config := make(map[string]interface{})

	// 1. Model Configuration
	provider := string(m.Spec.Model.Provider)
	modelName := m.Spec.Model.Name
	
	// Format: "anthropic/claude-3-5-sonnet-20241022" or just the model name depending on provider
	modelRef := modelName
	if provider != "custom" && provider != "ollama" {
		modelRef = fmt.Sprintf("%s/%s", provider, modelName)
	}
	config["model"] = modelRef

	// Set API keys if specified
	if m.Spec.Model.APIKeyEnv != "" {
		providerKey := provider
		if provider == "anthropic" {
			providerKey = "anthropic"
		}
		
		providerMap := map[string]interface{}{
			"options": map[string]interface{}{
				"apiKey": fmt.Sprintf("{env:%s}", m.Spec.Model.APIKeyEnv),
			},
		}
		
		if m.Spec.Model.BaseURL != "" {
			providerMap["options"].(map[string]interface{})["baseURL"] = m.Spec.Model.BaseURL
		}
		
		config["provider"] = map[string]interface{}{
			providerKey: providerMap,
		}
	}

	// 2. MCP Servers
	if len(m.Spec.MCP.Servers) > 0 {
		mcpConfig := make(map[string]interface{})
		for _, srv := range m.Spec.MCP.Servers {
			srvConfig := map[string]interface{}{
				"type": "local",
			}
			
			// Command handling
			if srv.Command != "" {
				// We expect the command to be something like "npx -y @modelcontextprotocol/server-filesystem /workspace"
				// For OpenCode, it expects an array of command + args
				cmdArray := []string{srv.Command}
				cmdArray = append(cmdArray, srv.Args...)
				srvConfig["command"] = cmdArray
			} else if srv.Source != "" {
				// If source is provided, the MCP server is installed by the builder to /opt/agentbox/mcp/srv.Name
				// So we use that path.
				srvConfig["command"] = []string{fmt.Sprintf("/opt/agentbox/mcp/%s/run.sh", srv.Name)}
			}
			
			// Environment variables
			if len(srv.Env) > 0 {
				srvConfig["env"] = srv.Env
			}
			
			mcpConfig[srv.Name] = srvConfig
		}
		config["mcp"] = mcpConfig
	}

	// 3. Tools (Guardrails / permissions)
	if len(m.Spec.Tools.Permissions) > 0 {
		toolsConfig := make(map[string]interface{})
		for _, p := range m.Spec.Tools.Permissions {
			toolsConfig[p.Tool] = p.Allow
		}
		config["tools"] = toolsConfig
	}

	// Serialize to JSON
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling opencode config: %w", err)
	}

	// We place it in /opt/agentbox/config/ and the init script will link it
	// to ~/.config/opencode/opencode.json
	return map[string][]byte{
		"/opt/agentbox/config/harness/opencode.json": data,
	}, nil
}
