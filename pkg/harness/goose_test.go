package harness

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGooseAdapter_GenerateConfig(t *testing.T) {
	cfg := &HarnessConfig{
		Model: ModelConfig{Name: "claude"},
		MCPs: []MCPConfig{
			{Name: "fetch", Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-fetch"}},
		},
	}

	adapter := &GooseAdapter{}
	files, err := adapter.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("generate config: %v", err)
	}

	data := files["/opt/agentbox/config/harness/goose.yaml"]
	if len(data) == 0 {
		t.Fatal("missing goose.yaml")
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse config: %v", err)
	}

	extensions, ok := config["extensions"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing extensions: %v", config)
	}
	if _, ok := extensions["fetch"]; !ok {
		t.Errorf("missing fetch extension: %v", extensions)
	}
}

func TestGooseAdapter_ValidateConfig(t *testing.T) {
	adapter := &GooseAdapter{}
	if err := adapter.ValidateConfig(&HarnessConfig{Model: ModelConfig{Name: "claude"}}); err != nil {
		t.Errorf("expected valid config: %v", err)
	}
	if err := adapter.ValidateConfig(&HarnessConfig{}); err == nil {
		t.Error("expected error for missing model name")
	}
}
