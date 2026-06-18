package ocx

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type fakeFetcher struct {
	components map[string]*ComponentManifest
}

func (f *fakeFetcher) Fetch(ctx context.Context, ref string) (*ComponentManifest, string, error) {
	c, ok := f.components[ref]
	if !ok {
		return nil, "", fmt.Errorf("not found: %s", ref)
	}
	return c, "", nil
}

func TestResolver_Resolve(t *testing.T) {
	fetcher := &fakeFetcher{
		components: map[string]*ComponentManifest{
			"reg.io/base": {
				Name: "base",
				Type: ComponentSkill,
				Opencode: OpencodeBlock{
					"model": "base-model",
					"plugin": []interface{}{"plugin-a@1.0.0"},
				},
			},
			"reg.io/child": {
				Name: "child",
				Type: ComponentSkill,
				Dependencies: []string{"base"},
				Opencode: OpencodeBlock{
					"plugin": []interface{}{"plugin-a@2.0.0", "plugin-b"},
					"tools": map[string]interface{}{"bash": true},
				},
			},
		},
	}

	r := NewResolver(fetcher)
	set, err := r.Resolve(t.Context(), []string{"reg.io/child"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer set.Cleanup()

	if len(set.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(set.Components))
	}
	if set.Components[0].Manifest.Name != "base" {
		t.Errorf("expected base first, got %s", set.Components[0].Manifest.Name)
	}
	if set.Components[1].Manifest.Name != "child" {
		t.Errorf("expected child second, got %s", set.Components[1].Manifest.Name)
	}

	plugins, ok := set.Opencode["plugin"].([]interface{})
	if !ok {
		t.Fatalf("expected plugin slice, got %T", set.Opencode["plugin"])
	}
	if len(plugins) != 2 || plugins[0] != "plugin-a@2.0.0" || plugins[1] != "plugin-b" {
		t.Errorf("unexpected plugins: %v", plugins)
	}

	if set.Opencode["model"] != "base-model" {
		t.Errorf("model not merged: %v", set.Opencode["model"])
	}
}

func TestResolver_Resolve_Cycle(t *testing.T) {
	fetcher := &fakeFetcher{
		components: map[string]*ComponentManifest{
			"a": {Name: "a", Type: ComponentBundle, Dependencies: []string{"b"}},
			"b": {Name: "b", Type: ComponentBundle, Dependencies: []string{"a"}},
		},
	}

	r := NewResolver(fetcher)
	_, err := r.Resolve(t.Context(), []string{"a"})
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestNormalizeDependencyRef(t *testing.T) {
	cases := []struct {
		dep      string
		parent   string
		expected string
	}{
		{"foo:latest", "reg.io/bar:v1", "reg.io/foo:latest"},
		{"reg.io/foo:tag", "reg.io/bar:v1", "reg.io/foo:tag"},
		{"alias/name", "reg.io/bar:v1", "reg.io/alias/name"},
	}
	for _, tc := range cases {
		if got := normalizeDependencyRef(tc.dep, tc.parent); got != tc.expected {
			t.Errorf("%s relative to %s: got %s, want %s", tc.dep, tc.parent, got, tc.expected)
		}
	}
}

func TestMergeOpencode(t *testing.T) {
	dst := OpencodeBlock{
		"model":        "a",
		"plugin":       []interface{}{"x@1"},
		"instructions": []interface{}{"do this"},
		"tools":        map[string]interface{}{"bash": false},
	}
	src := OpencodeBlock{
		"plugin":       []interface{}{"x@2", "y"},
		"instructions": []interface{}{"do that"},
		"tools":        map[string]interface{}{"bash": true, "edit": false},
	}

	merged := mergeOpencode(dst, src)

	plugins, ok := merged["plugin"].([]interface{})
	if !ok || len(plugins) != 2 {
		t.Fatalf("unexpected plugins: %v", merged["plugin"])
	}

	if merged["model"] != "a" {
		t.Errorf("model should be preserved from dst: %v", merged["model"])
	}

	wantTools := OpencodeBlock{"bash": true, "edit": false}
	if !reflect.DeepEqual(merged["tools"], wantTools) {
		t.Errorf("tools not merged: got %#v, want %#v", merged["tools"], wantTools)
	}
}
