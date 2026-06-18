package ocx

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestComponentType_IsValid(t *testing.T) {
	for _, tc := range []struct {
		typ   ComponentType
		valid bool
	}{
		{ComponentSkill, true},
		{ComponentBundle, true},
		{ComponentType("foo"), false},
		{ComponentType(""), false},
	} {
		if got := tc.typ.IsValid(); got != tc.valid {
			t.Errorf("%q: expected valid=%v, got %v", tc.typ, tc.valid, got)
		}
	}
}

func TestComponentManifest_Validate(t *testing.T) {
	valid := ComponentManifest{Name: "researcher", Type: ComponentAgent}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	missingName := ComponentManifest{Type: ComponentSkill}
	if err := missingName.Validate(); err == nil {
		t.Error("expected error for missing name")
	}

	invalidType := ComponentManifest{Name: "x", Type: ComponentType("bad")}
	if err := invalidType.Validate(); err == nil {
		t.Error("expected error for invalid type")
	}

	emptyFilePath := ComponentManifest{
		Name:  "x",
		Type:  ComponentSkill,
		Files: []FileSpec{{Path: ""}},
	}
	if err := emptyFilePath.Validate(); err == nil {
		t.Error("expected error for empty file path")
	}
}

func TestFileSpec_JSON(t *testing.T) {
	cases := []struct {
		name string
		in   FileSpec
		want string
	}{
		{"shorthand", FileSpec{Path: "skills/x.md", Target: "skills/x.md"}, `"skills/x.md"`},
		{"explicit", FileSpec{Path: "src.md", Target: "dst.md"}, `{"path":"src.md","target":"dst.md"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(data) != tc.want {
				t.Errorf("marshal: got %s, want %s", data, tc.want)
			}

			var out FileSpec
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if out != tc.in {
				t.Errorf("round-trip: got %+v, want %+v", out, tc.in)
			}
		})
	}
}

func TestFileSpec_YAML(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want FileSpec
	}{
		{"shorthand", `- skills/x.md`, FileSpec{Path: "skills/x.md", Target: "skills/x.md"}},
		{"explicit", `- {path: src.md, target: dst.md}`, FileSpec{Path: "src.md", Target: "dst.md"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out []FileSpec
			if err := yaml.Unmarshal([]byte(tc.yaml), &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(out) != 1 || out[0] != tc.want {
				t.Errorf("got %+v, want %+v", out, []FileSpec{tc.want})
			}
		})
	}
}

func TestOpencodeBlock_JSON(t *testing.T) {
	data := []byte(`{"model":"claude","tools":{"bash":true}}`)
	var block OpencodeBlock
	if err := json.Unmarshal(data, &block); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if block["model"] != "claude" {
		t.Errorf("model not parsed: %v", block["model"])
	}

	back, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(back) != string(data) {
		t.Errorf("round-trip: got %s", back)
	}
}
