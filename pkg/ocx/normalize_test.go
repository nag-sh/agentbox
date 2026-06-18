package ocx

import (
	"testing"

	"github.com/nag-sh/agentbox/pkg/harness"
	"github.com/nag-sh/agentbox/pkg/manifest"
)

func TestNormalizer_Normalize(t *testing.T) {
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
			Skills: []manifest.SkillSpec{
				{Name: "local-skill", Path: "./skills/local"},
			},
		},
	}

	resolved := &ResolvedSet{
		Components: []*ResolvedComponent{
			{
				Manifest: &ComponentManifest{
					Name: "ocx-skill",
					Type: ComponentSkill,
					Files: []FileSpec{
						{Path: "SKILL.md", Target: "skills/ocx-skill/SKILL.md"},
					},
				},
				Source:     "reg.io/ocx-skill:v1",
				StagingDir: "/tmp/ocx-skill",
			},
			{
				Manifest: &ComponentManifest{
					Name: "ocx-agent",
					Type: ComponentAgent,
					Opencode: OpencodeBlock{
						"default_agent": "ocx-agent",
					},
				},
				Source: "reg.io/ocx-agent:v1",
			},
		},
		Opencode: OpencodeBlock{
			"model": "ocx-model",
		},
	}

	n := NewNormalizer()
	h, err := n.Normalize(m, resolved)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}

	if h.Metadata.Name != "test" {
		t.Errorf("metadata name: got %q", h.Metadata.Name)
	}
	if h.Harness.Name != "opencode" {
		t.Errorf("harness name: got %q", h.Harness.Name)
	}
	if len(h.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(h.Skills))
	}
	if h.Skills[0].Name != "local-skill" || h.Skills[1].Name != "ocx-skill" {
		t.Errorf("unexpected skill names: %v", h.Skills)
	}
	if len(h.Skills[1].Files) != 1 || h.Skills[1].Files[0] != "skills/ocx-skill/SKILL.md" {
		t.Errorf("unexpected skill files: %v", h.Skills[1].Files)
	}
	if len(h.Agents) != 1 || h.Agents[0].Name != "ocx-agent" {
		t.Errorf("unexpected agents: %v", h.Agents)
	}
	if h.Opencode["model"] != "ocx-model" {
		t.Errorf("opencode model not merged: %v", h.Opencode["model"])
	}
}

func TestNormalizer_Normalize_NilManifest(t *testing.T) {
	n := NewNormalizer()
	if _, err := n.Normalize(nil, nil); err == nil {
		t.Error("expected error for nil manifest")
	}
}

func TestFileTargets(t *testing.T) {
	files := []FileSpec{
		{Path: "a.md", Target: "a.md"},
		{Path: "b.md", Target: "dst/b.md"},
	}
	got := fileTargets(files)
	want := []string{"a.md", "dst/b.md"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAppendIfMissing(t *testing.T) {
	s := []string{"a", "b"}
	s = appendIfMissing(s, "a")
	s = appendIfMissing(s, "c")
	if len(s) != 3 {
		t.Errorf("expected 3 items, got %d", len(s))
	}
}

func TestAgentConfig_Type(t *testing.T) {
	var _ harness.AgentConfig = harness.AgentConfig{}
}
