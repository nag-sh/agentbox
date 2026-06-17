package builder

import (
	"testing"

	"github.com/nag-sh/agentbox/pkg/harness"
	"github.com/nag-sh/agentbox/pkg/manifest"
	"github.com/nag-sh/agentbox/pkg/ocx"
)

func TestConfigGenerator_Generate(t *testing.T) {
	m := &manifest.Manifest{
		APIVersion: "agentbox/v1",
		Kind:       "AgentImage",
		Metadata: manifest.Metadata{
			Name:    "test",
			Version: "1.0.0",
		},
		Spec: manifest.Spec{
			OS: manifest.OSSpec{
				Base: "alpine:latest",
			},
			Harness: manifest.HarnessSpec{
				Name:    manifest.HarnessOpenCode,
				Version: "1.0",
			},
			Model: manifest.ModelSpec{
				Provider: manifest.ModelProviderAnthropic,
				Name:     "claude",
			},
		},
	}

	gen := NewConfigGenerator()
	gen.RegisterAdapter("opencode", &harness.OpenCodeAdapter{})

	files, err := gen.Generate(m, nil)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if _, ok := files["/opt/agentbox/config/harness/opencode.json"]; !ok {
		t.Error("missing opencode harness config")
	}
	if _, ok := files["/opt/agentbox/config/runtime.yaml"]; !ok {
		t.Error("missing runtime config")
	}
	if _, ok := files["/opt/agentbox/config/guardrails.yaml"]; !ok {
		t.Error("missing guardrails config")
	}
}

func TestConfigGenerator_Generate_WithOCX(t *testing.T) {
	m := &manifest.Manifest{
		APIVersion: "agentbox/v1",
		Kind:       "AgentImage",
		Metadata: manifest.Metadata{
			Name:    "test",
			Version: "1.0.0",
		},
		Spec: manifest.Spec{
			OS: manifest.OSSpec{
				Base: "alpine:latest",
			},
			Harness: manifest.HarnessSpec{
				Name:    manifest.HarnessOpenCode,
				Version: "1.0",
			},
			Model: manifest.ModelSpec{
				Provider: manifest.ModelProviderAnthropic,
				Name:     "claude",
			},
		},
	}

	resolved := &ocx.ResolvedSet{
		Components: []*ocx.ResolvedComponent{
			{
				Manifest: &ocx.ComponentManifest{
					Name: "ocx-agent",
					Type: ocx.ComponentAgent,
					Opencode: ocx.OpencodeBlock{
						"default_agent": "ocx-agent",
					},
				},
				Source: "reg.io/ocx-agent:v1",
			},
		},
		Opencode: ocx.OpencodeBlock{
			"model": "ocx-model",
		},
	}

	gen := NewConfigGenerator()
	gen.RegisterAdapter("opencode", &harness.OpenCodeAdapter{})

	files, err := gen.Generate(m, resolved)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	data := files["/opt/agentbox/config/harness/opencode.json"]
	if len(data) == 0 {
		t.Fatal("missing opencode harness config")
	}
	if string(data) == "" {
		t.Fatal("empty opencode config")
	}
}

func TestConfigGenerator_Generate_UnknownHarness(t *testing.T) {
	m := &manifest.Manifest{
		APIVersion: "agentbox/v1",
		Kind:       "AgentImage",
		Spec: manifest.Spec{
			OS:      manifest.OSSpec{Base: "alpine:latest"},
			Harness: manifest.HarnessSpec{Name: manifest.HarnessAider, Version: "1.0"},
			Model:   manifest.ModelSpec{Provider: manifest.ModelProviderAnthropic, Name: "claude"},
		},
	}

	gen := NewConfigGenerator()
	if _, err := gen.Generate(m, nil); err == nil {
		t.Error("expected error for unregistered harness")
	}
}
