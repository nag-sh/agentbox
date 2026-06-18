package manifest

import "testing"

func newValidManifest() *Manifest {
	return &Manifest{
		APIVersion: APIVersion,
		Kind:       "AgentImage",
		Metadata: Metadata{
			Name:    "my-agent",
			Version: "1.0.0",
		},
		Spec: Spec{
			OS: OSSpec{
				Base: "alpine:latest",
			},
			Harness: HarnessSpec{
				Name:    HarnessOpenCode,
				Version: "1.0.0",
			},
			Model: ModelSpec{
				Provider: ModelProviderAnthropic,
				Name:     "claude",
			},
		},
	}
}

func TestValidate_valid(t *testing.T) {
	m := newValidManifest()
	result := Validate(m)
	if !result.IsValid() {
		t.Errorf("expected valid manifest, got: %s", result.Error())
	}
}

func TestValidate_missingAPIVersion(t *testing.T) {
	m := newValidManifest()
	m.APIVersion = ""
	result := Validate(m)
	if result.IsValid() {
		t.Error("expected invalid manifest")
	}
}

func TestValidate_invalidGuardrailsDefaultPolicy(t *testing.T) {
	m := newValidManifest()
	m.Spec.Guardrails.Commands.DefaultPolicy = "block"
	m.Spec.Guardrails.Filesystem.DefaultPolicy = "block"
	result := Validate(m)
	if result.IsValid() {
		t.Error("expected invalid manifest")
	}
}

func TestValidate_invalidMemory(t *testing.T) {
	m := newValidManifest()
	m.Spec.Guardrails.Resources.MaxMemory = "lots"
	result := Validate(m)
	if result.IsValid() {
		t.Error("expected invalid manifest")
	}
}

func TestValidate_duplicateSkillName(t *testing.T) {
	m := newValidManifest()
	m.Spec.Skills = []SkillSpec{
		{Name: "foo", Source: "example.com/foo:v1"},
		{Name: "foo", Path: "/foo"},
	}
	result := Validate(m)
	if result.IsValid() {
		t.Error("expected invalid manifest")
	}
}
