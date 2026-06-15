package harness

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/nag-sh/agentbox/pkg/manifest"
)

// GooseAdapter implements harness.Adapter for the Block/Goose agent.
type GooseAdapter struct{}

// Name returns the harness identifier.
func (a *GooseAdapter) Name() string {
	return string(manifest.HarnessGoose)
}

// DefaultEntrypoint returns the default command to start Goose.
func (a *GooseAdapter) DefaultEntrypoint() []string {
	return []string{"goose", "session"}
}

// ValidateManifest checks if the manifest contains required settings for Goose.
func (a *GooseAdapter) ValidateManifest(m *manifest.Manifest) error {
	if m.Spec.Model.Name == "" {
		return fmt.Errorf("model name is required for goose")
	}
	return nil
}

// GenerateConfig generates the goose config.yaml configuration file.
func (a *GooseAdapter) GenerateConfig(m *manifest.Manifest) (map[string][]byte, error) {
	// Goose uses ~/.config/goose/config.yaml
	// Example format:
	// extensions:
	//   my-mcp:
	//     name: my-mcp
	//     cmd: npx
	//     args: ["-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
	//     envs:
	//       FOO: BAR
	
	config := make(map[string]interface{})
	
	// Add extensions (MCP servers)
	if len(m.Spec.MCP.Servers) > 0 {
		extensions := make(map[string]interface{})
		for _, srv := range m.Spec.MCP.Servers {
			extConfig := map[string]interface{}{
				"name": srv.Name,
				"cmd":  srv.Command,
				"args": srv.Args,
			}
			
			if srv.Command == "" && srv.Source != "" {
				// We fall back to the path installed by builder
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
	
	// Generate YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshaling goose config: %w", err)
	}
	
	return map[string][]byte{
		"/opt/agentbox/config/harness/goose.yaml": data,
	}, nil
}
