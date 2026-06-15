package manifest

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFile loads and parses a manifest from a YAML file.
// It supports environment variable interpolation in the form ${VAR} and ${VAR:-default}.
func LoadFile(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading manifest file %s: %w", path, err)
	}
	return Load(data)
}

// Load parses a manifest from raw YAML bytes.
// It performs environment variable interpolation before parsing.
func Load(data []byte) (*Manifest, error) {
	// Interpolate environment variables.
	interpolated := interpolateEnvVars(string(data))

	var m Manifest
	if err := yaml.Unmarshal([]byte(interpolated), &m); err != nil {
		return nil, fmt.Errorf("parsing manifest YAML: %w", err)
	}

	// Apply defaults.
	applyDefaults(&m)

	return &m, nil
}

// envVarPattern matches ${VAR} and ${VAR:-default} patterns.
var envVarPattern = regexp.MustCompile(`\$\{([a-zA-Z_][a-zA-Z0-9_]*)(?::-([^}]*))?\}`)

// interpolateEnvVars replaces ${VAR} and ${VAR:-default} patterns with
// their environment variable values. If a variable is not set and no default
// is provided, the pattern is left as-is (will likely cause a validation error).
func interpolateEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		groups := envVarPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}

		varName := groups[1]
		defaultVal := ""
		hasDefault := len(groups) >= 3 && groups[2] != ""

		if hasDefault {
			defaultVal = groups[2]
		}

		if val, ok := os.LookupEnv(varName); ok {
			return val
		}

		if hasDefault {
			return defaultVal
		}

		// Leave as-is if no value and no default.
		return match
	})
}

// applyDefaults sets default values for optional fields that have sensible defaults.
func applyDefaults(m *Manifest) {
	if m.Spec.OS.Shell == "" {
		m.Spec.OS.Shell = "/bin/sh"
	}

	if m.Spec.Runtime.Workdir == "" {
		m.Spec.Runtime.Workdir = "/workspace"
	}

	if m.Spec.Network.Egress.DefaultPolicy == "" {
		m.Spec.Network.Egress.DefaultPolicy = DefaultPolicyDeny
	}

	if m.Spec.Network.Ingress.DefaultPolicy == "" {
		m.Spec.Network.Ingress.DefaultPolicy = DefaultPolicyDeny
	}

	// Default MCP transport to stdio.
	for i := range m.Spec.MCP.Servers {
		if m.Spec.MCP.Servers[i].Transport == "" {
			m.Spec.MCP.Servers[i].Transport = MCPTransportStdio
		}
	}

	// Default secret file mode.
	for i := range m.Spec.Secrets.Files {
		if m.Spec.Secrets.Files[i].Mode == "" {
			m.Spec.Secrets.Files[i].Mode = "0400"
		}
	}

	// Default mount source to current directory.
	for i := range m.Spec.Runtime.Mounts {
		if m.Spec.Runtime.Mounts[i].Source == "" {
			m.Spec.Runtime.Mounts[i].Source = "."
		}
	}

	// Default port protocol.
	for i := range m.Spec.Runtime.Ports {
		if m.Spec.Runtime.Ports[i].Protocol == "" {
			m.Spec.Runtime.Ports[i].Protocol = "tcp"
		}
	}

	// Ensure writable paths are also readable.
	if len(m.Spec.Guardrails.Filesystem.Writable) > 0 {
		readableSet := make(map[string]bool)
		for _, p := range m.Spec.Guardrails.Filesystem.Readable {
			readableSet[p] = true
		}
		for _, p := range m.Spec.Guardrails.Filesystem.Writable {
			if !readableSet[p] {
				m.Spec.Guardrails.Filesystem.Readable = append(
					m.Spec.Guardrails.Filesystem.Readable, p,
				)
			}
		}
	}
}

// MarshalYAML serializes a manifest back to YAML bytes.
func MarshalYAML(m *Manifest) ([]byte, error) {
	data, err := yaml.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshaling manifest to YAML: %w", err)
	}
	return data, nil
}

// DetectSchemaVersion reads just the apiVersion field from raw YAML
// without fully parsing the manifest. Useful for version migration.
func DetectSchemaVersion(data []byte) (string, error) {
	var partial struct {
		APIVersion string `yaml:"apiVersion"`
	}
	if err := yaml.Unmarshal(data, &partial); err != nil {
		return "", fmt.Errorf("detecting schema version: %w", err)
	}
	if partial.APIVersion == "" {
		return "", fmt.Errorf("manifest missing apiVersion field")
	}
	return partial.APIVersion, nil
}

// ResolveLocalPaths resolves relative paths in the manifest (skill paths,
// plugin paths, mount sources) relative to the given base directory.
func ResolveLocalPaths(m *Manifest, baseDir string) {
	for i := range m.Spec.Skills {
		if m.Spec.Skills[i].Path != "" && !isAbsolutePath(m.Spec.Skills[i].Path) {
			m.Spec.Skills[i].Path = joinPath(baseDir, m.Spec.Skills[i].Path)
		}
	}

	for i := range m.Spec.Plugins {
		if m.Spec.Plugins[i].Path != "" && !isAbsolutePath(m.Spec.Plugins[i].Path) {
			m.Spec.Plugins[i].Path = joinPath(baseDir, m.Spec.Plugins[i].Path)
		}
	}

	for i := range m.Spec.Runtime.Mounts {
		src := m.Spec.Runtime.Mounts[i].Source
		if src != "" && !isAbsolutePath(src) {
			m.Spec.Runtime.Mounts[i].Source = joinPath(baseDir, src)
		}
	}

	for i := range m.Spec.Secrets.Files {
		src := m.Spec.Secrets.Files[i].Source
		if src != "" && !isAbsolutePath(src) {
			m.Spec.Secrets.Files[i].Source = joinPath(baseDir, src)
		}
	}
}

func isAbsolutePath(p string) bool {
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~")
}

func joinPath(base, rel string) string {
	if strings.HasSuffix(base, "/") {
		return base + rel
	}
	return base + "/" + rel
}
