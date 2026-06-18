package manifest

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// ValidationError represents a single validation error in a manifest.
type ValidationError struct {
	// Field is the dot-separated path to the field (e.g., "spec.harness.name").
	Field string

	// Message describes what is wrong.
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationResult contains all validation errors found in a manifest.
type ValidationResult struct {
	Errors []ValidationError
}

// IsValid returns true if no validation errors were found.
func (r *ValidationResult) IsValid() bool {
	return len(r.Errors) == 0
}

// Error returns a combined error message from all validation errors.
func (r *ValidationResult) Error() string {
	if r.IsValid() {
		return ""
	}
	var msgs []string
	for _, e := range r.Errors {
		msgs = append(msgs, e.Error())
	}
	return fmt.Sprintf("manifest validation failed with %d error(s):\n  - %s",
		len(r.Errors), strings.Join(msgs, "\n  - "))
}

// addError appends a validation error.
func (r *ValidationResult) addError(field, message string) {
	r.Errors = append(r.Errors, ValidationError{Field: field, Message: message})
}

// Validate performs comprehensive validation on a parsed manifest.
// It checks required fields, valid enum values, mutual exclusivity constraints,
// reference formats, and semantic correctness.
func Validate(m *Manifest) *ValidationResult {
	result := &ValidationResult{}

	validateTopLevel(m, result)
	validateMetadata(&m.Metadata, result)
	validateOS(&m.Spec.OS, result)
	validateHarness(&m.Spec.Harness, result)
	validateModel(&m.Spec.Model, result)
	validateSkills(m.Spec.Skills, result)
	validateMCP(&m.Spec.MCP, result)
	validatePlugins(m.Spec.Plugins, result)
	validateGuardrails(&m.Spec.Guardrails, result)
	validateNetwork(&m.Spec.Network, result)
	validateGPU(&m.Spec.GPU, result)
	validateSecrets(&m.Spec.Secrets, result)
	validateRuntime(&m.Spec.Runtime, result)
	validateOCX(&m.Spec.OCX, result)

	return result
}

func validateTopLevel(m *Manifest, r *ValidationResult) {
	if m.APIVersion == "" {
		r.addError("apiVersion", "is required")
	} else if m.APIVersion != APIVersion {
		r.addError("apiVersion", fmt.Sprintf("must be %q, got %q", APIVersion, m.APIVersion))
	}

	if m.Kind == "" {
		r.addError("kind", "is required")
	} else if m.Kind != "AgentImage" {
		r.addError("kind", fmt.Sprintf("must be %q, got %q", "AgentImage", m.Kind))
	}
}

// semverPattern matches semantic versions like 1.0.0, 0.1.0-beta.1, etc.
var semverPattern = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// dnsLabelPattern matches valid DNS labels (RFC 1035).
var dnsLabelPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

func validateMetadata(m *Metadata, r *ValidationResult) {
	if m.Name == "" {
		r.addError("metadata.name", "is required")
	} else if !dnsLabelPattern.MatchString(m.Name) {
		r.addError("metadata.name", "must be a valid DNS label (lowercase alphanumeric and hyphens, starting with a letter, max 63 chars)")
	}

	if m.Version == "" {
		r.addError("metadata.version", "is required")
	} else if !semverPattern.MatchString(m.Version) {
		r.addError("metadata.version", fmt.Sprintf("must be valid semver, got %q", m.Version))
	}
}

func validateOS(o *OSSpec, r *ValidationResult) {
	if o.Base == "" {
		r.addError("spec.os.base", "is required")
	}
}

var validHarnesses = map[HarnessName]bool{
	HarnessOpenCode:  true,
	HarnessGoose:     true,
	HarnessAider:     true,
	HarnessClaudeCode: true,
}

func validateHarness(h *HarnessSpec, r *ValidationResult) {
	if h.Name == "" {
		r.addError("spec.harness.name", "is required")
	} else if !validHarnesses[h.Name] {
		r.addError("spec.harness.name", fmt.Sprintf("must be one of: opencode, goose, aider, claude-code; got %q", h.Name))
	}

	if h.Version == "" {
		r.addError("spec.harness.version", "is required")
	}

	if h.Source != "" {
		validateOCIRef(h.Source, "spec.harness.source", r)
	}
}

var validProviders = map[ModelProvider]bool{
	ModelProviderAnthropic: true,
	ModelProviderOpenAI:    true,
	ModelProviderOllama:    true,
	ModelProviderGoogle:    true,
	ModelProviderKimi:      true,
	ModelProviderCustom:    true,
}

func validateModel(m *ModelSpec, r *ValidationResult) {
	if m.Provider == "" {
		r.addError("spec.model.provider", "is required")
	} else if !validProviders[m.Provider] {
		r.addError("spec.model.provider", fmt.Sprintf("must be one of: anthropic, openai, ollama, custom; got %q", m.Provider))
	}

	if m.Name == "" {
		r.addError("spec.model.name", "is required")
	}
}

func validateSkills(skills []SkillSpec, r *ValidationResult) {
	names := make(map[string]bool)
	for i, s := range skills {
		field := fmt.Sprintf("spec.skills[%d]", i)

		if s.Name == "" {
			r.addError(field+".name", "is required")
		} else if names[s.Name] {
			r.addError(field+".name", fmt.Sprintf("duplicate skill name %q", s.Name))
		}
		names[s.Name] = true

		if s.Source == "" && s.Path == "" {
			r.addError(field, "must specify either source or path")
		}
		if s.Source != "" && s.Path != "" {
			r.addError(field, "source and path are mutually exclusive")
		}
		if s.Source != "" {
			validateOCIRef(s.Source, field+".source", r)
		}
	}
}

var validTransports = map[MCPTransport]bool{
	MCPTransportStdio: true,
	MCPTransportHTTP:  true,
	MCPTransportSSE:   true,
}

func validateMCP(mcp *MCPSpec, r *ValidationResult) {
	names := make(map[string]bool)
	for i, s := range mcp.Servers {
		field := fmt.Sprintf("spec.mcp.servers[%d]", i)

		if s.Name == "" {
			r.addError(field+".name", "is required")
		} else if names[s.Name] {
			r.addError(field+".name", fmt.Sprintf("duplicate MCP server name %q", s.Name))
		}
		names[s.Name] = true

		if s.Source == "" && s.Command == "" {
			r.addError(field, "must specify either source or command")
		}

		if !validTransports[s.Transport] {
			r.addError(field+".transport", fmt.Sprintf("must be one of: stdio, http, sse; got %q", s.Transport))
		}

		if s.Source != "" {
			validateOCIRef(s.Source, field+".source", r)
		}
	}
}

func validatePlugins(plugins []PluginSpec, r *ValidationResult) {
	names := make(map[string]bool)
	for i, p := range plugins {
		field := fmt.Sprintf("spec.plugins[%d]", i)

		if p.Name == "" {
			r.addError(field+".name", "is required")
		} else if names[p.Name] {
			r.addError(field+".name", fmt.Sprintf("duplicate plugin name %q", p.Name))
		}
		names[p.Name] = true

		if p.Source == "" && p.Path == "" {
			r.addError(field, "must specify either source or path")
		}
		if p.Source != "" && p.Path != "" {
			r.addError(field, "source and path are mutually exclusive")
		}
		if p.Source != "" {
			validateOCIRef(p.Source, field+".source", r)
		}
	}
}

func validateGuardrails(g *GuardrailsSpec, r *ValidationResult) {
	validPolicies := map[string]bool{"": true, "allow": true, "deny": true}
	if !validPolicies[g.Commands.DefaultPolicy] {
		r.addError("spec.guardrails.commands.defaultPolicy", fmt.Sprintf("must be 'allow' or 'deny', got %q", g.Commands.DefaultPolicy))
	}
	if !validPolicies[g.Filesystem.DefaultPolicy] {
		r.addError("spec.guardrails.filesystem.defaultPolicy", fmt.Sprintf("must be 'allow' or 'deny', got %q", g.Filesystem.DefaultPolicy))
	}

	// Validate resource limits if specified.
	if g.Resources.MaxMemory != "" {
		if !isValidMemorySize(g.Resources.MaxMemory) {
			r.addError("spec.guardrails.resources.maxMemory",
				fmt.Sprintf("invalid memory size %q (expected format like '4Gi', '512Mi')", g.Resources.MaxMemory))
		}
	}

	if g.Resources.MaxCPUs < 0 {
		r.addError("spec.guardrails.resources.maxCpus", "must be non-negative")
	}

	if g.Resources.MaxProcesses < 0 {
		r.addError("spec.guardrails.resources.maxProcesses", "must be non-negative")
	}

	if g.Resources.MaxOpenFiles < 0 {
		r.addError("spec.guardrails.resources.maxOpenFiles", "must be non-negative")
	}
}

func validateNetwork(n *NetworkSpec, r *ValidationResult) {
	for i, rule := range n.Egress.Allow {
		field := fmt.Sprintf("spec.network.egress.allow[%d]", i)
		if rule.Host == "" {
			r.addError(field+".host", "is required")
		}
		for j, port := range rule.Ports {
			if port < 1 || port > 65535 {
				r.addError(fmt.Sprintf("%s.ports[%d]", field, j),
					fmt.Sprintf("must be between 1 and 65535, got %d", port))
			}
		}
	}

	for i, rule := range n.Egress.Deny {
		field := fmt.Sprintf("spec.network.egress.deny[%d]", i)
		if rule.Host == "" {
			r.addError(field+".host", "is required")
		}
	}

	for i, rule := range n.Ingress.Allow {
		field := fmt.Sprintf("spec.network.ingress.allow[%d]", i)
		if rule.Port < 1 || rule.Port > 65535 {
			r.addError(field+".port",
				fmt.Sprintf("must be between 1 and 65535, got %d", rule.Port))
		}
		if rule.Source != "" {
			if !isValidNetworkSource(rule.Source) {
				r.addError(field+".source",
					fmt.Sprintf("invalid source %q (expected 'localhost', IP, or CIDR)", rule.Source))
			}
		}
	}
}

func validateGPU(g *GPUSpec, r *ValidationResult) {
	if !g.Enabled {
		return
	}

	validRuntimes := map[string]bool{"nvidia": true, "amd": true, "": true}
	if !validRuntimes[g.Runtime] {
		r.addError("spec.gpu.runtime", fmt.Sprintf("must be 'nvidia' or 'amd', got %q", g.Runtime))
	}
}

func validateSecrets(s *SecretsSpec, r *ValidationResult) {
	for i, f := range s.Files {
		field := fmt.Sprintf("spec.secrets.files[%d]", i)
		if f.Source == "" {
			r.addError(field+".source", "is required")
		}
		if f.Target == "" {
			r.addError(field+".target", "is required")
		}
		if f.Mode != "" && !isValidFileMode(f.Mode) {
			r.addError(field+".mode", fmt.Sprintf("invalid file mode %q (expected octal like '0600')", f.Mode))
		}
	}
}

func validateRuntime(rt *RuntimeSpec, r *ValidationResult) {
	validMountTypes := map[string]bool{"bind": true, "volume": true, "tmpfs": true}
	for i, m := range rt.Mounts {
		field := fmt.Sprintf("spec.runtime.mounts[%d]", i)
		if !validMountTypes[m.Type] {
			r.addError(field+".type", fmt.Sprintf("must be 'bind', 'volume', or 'tmpfs'; got %q", m.Type))
		}
		if m.Target == "" {
			r.addError(field+".target", "is required")
		}
	}

	for i, p := range rt.Ports {
		field := fmt.Sprintf("spec.runtime.ports[%d]", i)
		if p.Host < 1 || p.Host > 65535 {
			r.addError(field+".host", fmt.Sprintf("must be between 1 and 65535, got %d", p.Host))
		}
		if p.Container < 1 || p.Container > 65535 {
			r.addError(field+".container", fmt.Sprintf("must be between 1 and 65535, got %d", p.Container))
		}
	}
}

// validateOCIRef checks if a string looks like a valid OCI image reference.
func validateOCIRef(ref, field string, r *ValidationResult) {
	// Basic OCI reference validation: must contain at least a registry/repo pattern.
	if !strings.Contains(ref, "/") {
		r.addError(field, fmt.Sprintf("invalid OCI reference %q (expected registry/repo:tag format)", ref))
	}
}

func validateOCX(o *OCXSpec, r *ValidationResult) {
	for alias, reg := range o.Registries {
		if reg.URL == "" {
			r.addError(fmt.Sprintf("spec.ocx.registries[%s].url", alias), "is required")
		}
	}

	names := make(map[string]bool)
	for i, c := range o.Components {
		field := fmt.Sprintf("spec.ocx.components[%d]", i)
		if c.Name == "" {
			r.addError(field+".name", "is required")
		} else if names[c.Name] {
			r.addError(field+".name", fmt.Sprintf("duplicate OCX component name %q", c.Name))
		}
		names[c.Name] = true
		if c.Source == "" {
			r.addError(field+".source", "is required")
		}
	}
}

// isValidMemorySize checks if a string is a valid memory size (e.g., "4Gi", "512Mi", "1G").
func isValidMemorySize(s string) bool {
	pattern := regexp.MustCompile(`^[1-9]\d*(\.\d+)?\s*(Ki|Mi|Gi|Ti|Pi|Ei|K|M|G|T|P|E|k|m|g)?$`)
	return pattern.MatchString(s)
}

// isValidNetworkSource checks if a string is a valid network source (localhost, IP, CIDR).
func isValidNetworkSource(s string) bool {
	if s == "localhost" || s == "0.0.0.0" || s == "::" {
		return true
	}
	if net.ParseIP(s) != nil {
		return true
	}
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	return false
}

// isValidFileMode checks if a string is a valid octal file mode.
func isValidFileMode(s string) bool {
	pattern := regexp.MustCompile(`^0[0-7]{3}$`)
	return pattern.MatchString(s)
}
