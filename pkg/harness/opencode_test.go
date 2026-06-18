package harness

import (
	"encoding/json"
	"testing"
)

func TestOpenCodeAdapter_GenerateConfig(t *testing.T) {
	cfg := &HarnessConfig{
		Model: ModelConfig{
			Provider:  "anthropic",
			Name:      "claude",
			APIKeyEnv: "ANTHROPIC_API_KEY",
		},
		MCPs: []MCPConfig{
			{Name: "fetch", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-fetch"}},
		},
		Tools: ToolsConfig{
			Permissions: []ToolPermissionConfig{{Tool: "bash", Allow: true}},
		},
		Opencode: map[string]any{
			"default_agent": "researcher",
		},
	}

	adapter := &OpenCodeAdapter{}
	files, err := adapter.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}

	data := files["/opt/agentbox/config/harness/opencode.json"]
	if len(data) == 0 {
		t.Fatal("missing opencode.json")
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	if config["model"] != "anthropic/claude" {
		t.Errorf("model: got %v", config["model"])
	}
	if config["default_agent"] != "researcher" {
		t.Errorf("default_agent not merged: %v", config["default_agent"])
	}
}

func TestOpenCodeAdapter_ValidateConfig(t *testing.T) {
	adapter := &OpenCodeAdapter{}
	if err := adapter.ValidateConfig(&HarnessConfig{Model: ModelConfig{Name: "claude"}}); err != nil {
		t.Errorf("expected valid config: %v", err)
	}
	if err := adapter.ValidateConfig(&HarnessConfig{}); err == nil {
		t.Error("expected error for missing model name")
	}
}
