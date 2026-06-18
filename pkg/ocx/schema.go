// Package ocx provides Go types and utilities for working with OCX
// (OpenCode eXtensions) components. OCX defines a component taxonomy
// (agent, skill, plugin, command, tool, bundle, profile) and an optional
// "opencode" configuration block that deep-merges into opencode.jsonc.
//
// Agentbox hosts OCX components as OCI artifacts, so this package also
// includes the OCI fetcher and a resolver that turns OCX components into
// the harness-agnostic model consumed by pkg/harness adapters.
package ocx

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ComponentType identifies the kind of OCX component.
type ComponentType string

const (
	ComponentAgent   ComponentType = "agent"
	ComponentSkill   ComponentType = "skill"
	ComponentPlugin  ComponentType = "plugin"
	ComponentCommand ComponentType = "command"
	ComponentTool    ComponentType = "tool"
	ComponentBundle  ComponentType = "bundle"
	ComponentProfile ComponentType = "profile"
)

// ValidComponentTypes lists all supported OCX component types.
var ValidComponentTypes = []ComponentType{
	ComponentAgent,
	ComponentSkill,
	ComponentPlugin,
	ComponentCommand,
	ComponentTool,
	ComponentBundle,
	ComponentProfile,
}

// IsValid reports whether t is a known OCX component type.
func (t ComponentType) IsValid() bool {
	switch t {
	case ComponentAgent, ComponentSkill, ComponentPlugin, ComponentCommand,
		ComponentTool, ComponentBundle, ComponentProfile:
		return true
	}
	return false
}

// ComponentManifest is the core unit of an OCX package. It is embedded as
// the config descriptor of an OCI artifact (serialized as component.json).
type ComponentManifest struct {
	Name               string          `json:"name" yaml:"name"`
	Type               ComponentType   `json:"type" yaml:"type"`
	Description        string          `json:"description" yaml:"description"`
	Files              []FileSpec      `json:"files,omitempty" yaml:"files,omitempty"`
	Dependencies       []string        `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
	NPMDependencies    []string        `json:"npmDependencies,omitempty" yaml:"npmDependencies,omitempty"`
	NPMDevDependencies []string        `json:"npmDevDependencies,omitempty" yaml:"npmDevDependencies,omitempty"`
	Opencode           OpencodeBlock   `json:"opencode,omitempty" yaml:"opencode,omitempty"`
}

// Validate performs lightweight structural validation of a component manifest.
func (m ComponentManifest) Validate() error {
	if m.Name == "" {
		return fmt.Errorf("component name is required")
	}
	if !m.Type.IsValid() {
		return fmt.Errorf("unknown component type %q", m.Type)
	}
	for _, f := range m.Files {
		if f.Path == "" {
			return fmt.Errorf("file entry with empty path")
		}
	}
	return nil
}

// FileSpec describes a file shipped with an OCX component. It supports a
// Cargo-style shorthand where a bare string `"path"` is equivalent to
// `{path: "path", target: "path"}`.
type FileSpec struct {
	Path   string `json:"path" yaml:"path"`
	Target string `json:"target" yaml:"target"`
}

// UnmarshalJSON implements json.Unmarshaler for the string-or-object union.
func (f *FileSpec) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		f.Path = s
		f.Target = s
		return nil
	}

	type fileSpecAlias FileSpec
	var alias fileSpecAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return fmt.Errorf("file spec must be a string or an object: %w", err)
	}
	*f = FileSpec(alias)
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler for the string-or-object union.
func (f *FileSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		f.Path = node.Value
		f.Target = node.Value
		return nil
	}

	type fileSpecAlias FileSpec
	var alias fileSpecAlias
	if err := node.Decode(&alias); err != nil {
		return fmt.Errorf("file spec must be a scalar or a mapping: %w", err)
	}
	*f = FileSpec(alias)
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (f FileSpec) MarshalYAML() (interface{}, error) {
	if f.Path == f.Target {
		return f.Path, nil
	}
	type fileSpecAlias FileSpec
	return fileSpecAlias(f), nil
}

// MarshalJSON implements json.Marshaler. A FileSpec with matching Path and
// Target is serialized as a string shorthand.
func (f FileSpec) MarshalJSON() ([]byte, error) {
	if f.Path == f.Target {
		return json.Marshal(f.Path)
	}
	type fileSpecAlias FileSpec
	return json.Marshal(fileSpecAlias(f))
}

// OpencodeBlock is the raw "opencode" configuration object carried by an OCX
// component. It is intentionally an untyped map so that OCX can evolve its
// opencode.jsonc schema without requiring Agentbox schema changes. Agentbox
// deep-merges these blocks during dependency resolution and passes the result
// to harness adapters (primarily OpenCode).
type OpencodeBlock map[string]interface{}

// UnmarshalJSON copies a JSON object into the block.
func (o *OpencodeBlock) UnmarshalJSON(data []byte) error {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*o = m
	return nil
}

// MarshalJSON serializes the block.
func (o OpencodeBlock) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	return json.Marshal(map[string]interface{}(o))
}

// UnmarshalYAML implements yaml.Unmarshaler for the object form.
func (o *OpencodeBlock) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("opencode block must be a mapping")
	}
	m := make(map[string]interface{}, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		var value interface{}
		if err := node.Content[i+1].Decode(&value); err != nil {
			return err
		}
		m[key] = value
	}
	*o = m
	return nil
}

// MarshalYAML implements yaml.Marshaler.
func (o OpencodeBlock) MarshalYAML() (interface{}, error) {
	if o == nil {
		return nil, nil
	}
	return map[string]interface{}(o), nil
}

// Packument is the npm-style versioned envelope served by an HTTP OCX
// registry. Agentbox uses it only when consuming OCX components directly
// from an HTTP registry; OCI artifacts embed ComponentManifest directly.
type Packument struct {
	Name     string                       `json:"name"`
	DistTags map[string]string            `json:"dist-tags"`
	Versions map[string]ComponentManifest `json:"versions"`
}

// RegistryManifest is the source manifest used to build an OCX registry.
type RegistryManifest struct {
	Schema     string              `json:"$schema,omitempty" yaml:"$schema,omitempty"`
	Name       string              `json:"name" yaml:"name"`
	Namespace  string              `json:"namespace" yaml:"namespace"`
	Version    string              `json:"version" yaml:"version"`
	Author     string              `json:"author,omitempty" yaml:"author,omitempty"`
	Opencode   string              `json:"opencode,omitempty" yaml:"opencode,omitempty"`
	OCX        string              `json:"ocx,omitempty" yaml:"ocx,omitempty"`
	Components []ComponentManifest `json:"components" yaml:"components"`
}

// RegistryConfig describes how to reach an OCX HTTP registry.
type RegistryConfig struct {
	URL     string            `json:"url" yaml:"url"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// ProfileOcxConfig is the per-profile OCX configuration stored alongside
// opencode.jsonc in a profile directory.
type ProfileOcxConfig struct {
	Schema        string                    `json:"$schema,omitempty" yaml:"$schema,omitempty"`
	Bin           string                    `json:"bin,omitempty" yaml:"bin,omitempty"`
	Registries    map[string]RegistryConfig `json:"registries" yaml:"registries"`
	ComponentPath string                    `json:"componentPath,omitempty" yaml:"componentPath,omitempty"`
	RenameWindow  bool                      `json:"renameWindow" yaml:"renameWindow"`
	Exclude       []string                  `json:"exclude" yaml:"exclude"`
	Include       []string                  `json:"include" yaml:"include"`
}
