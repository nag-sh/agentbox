package harness

import (
	"time"

	"github.com/nag-sh/agentbox/pkg/guardrails"
	"github.com/nag-sh/agentbox/pkg/network"
)

// HarnessConfig is a harness-agnostic, normalized representation of everything
// needed to configure an agent container. It is produced by combining the
// user-facing agentbox manifest with any resolved OCX components, and it is
// consumed by harness adapters instead of the raw manifest or OCX structs.
type HarnessConfig struct {
	APIVersion string            `json:"apiVersion" yaml:"apiVersion"`
	Kind       string            `json:"kind" yaml:"kind"`
	Metadata   MetadataConfig    `json:"metadata" yaml:"metadata"`
	OS         OSConfig          `json:"os" yaml:"os"`
	Harness    HarnessInfo       `json:"harness" yaml:"harness"`
	Model      ModelConfig       `json:"model" yaml:"model"`
	Skills     []SkillConfig     `json:"skills,omitempty" yaml:"skills,omitempty"`
	Plugins    []PluginConfig    `json:"plugins,omitempty" yaml:"plugins,omitempty"`
	MCPs       []MCPConfig       `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	Tools      ToolsConfig       `json:"tools,omitempty" yaml:"tools,omitempty"`
	Agents     []AgentConfig     `json:"agents,omitempty" yaml:"agents,omitempty"`
	Commands   []CommandConfig   `json:"commands,omitempty" yaml:"commands,omitempty"`
	Bundles    []BundleConfig    `json:"bundles,omitempty" yaml:"bundles,omitempty"`
	Opencode   map[string]any    `json:"opencode,omitempty" yaml:"opencode,omitempty"`
	Guardrails guardrails.GuardrailConfig `json:"guardrails,omitempty" yaml:"guardrails,omitempty"`
	Network    network.NetworkPolicy      `json:"network,omitempty" yaml:"network,omitempty"`
	GPU        GPUConfig         `json:"gpu,omitempty" yaml:"gpu,omitempty"`
	Secrets    SecretsConfig     `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Runtime    RuntimeConfig     `json:"runtime,omitempty" yaml:"runtime,omitempty"`
}

// MetadataConfig carries descriptive metadata about the agent image.
type MetadataConfig struct {
	Name        string            `json:"name" yaml:"name"`
	Version     string            `json:"version" yaml:"version"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// OSConfig describes the base operating system layer.
type OSConfig struct {
	Base     string   `json:"base" yaml:"base"`
	Packages []string `json:"packages,omitempty" yaml:"packages,omitempty"`
	Shell    string   `json:"shell,omitempty" yaml:"shell,omitempty"`
}

// HarnessInfo identifies the harness and how to start it.
type HarnessInfo struct {
	Name       string   `json:"name" yaml:"name"`
	Version    string   `json:"version" yaml:"version"`
	Source     string   `json:"source,omitempty" yaml:"source,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty" yaml:"entrypoint,omitempty"`
}

// ModelConfig describes the LLM model configuration.
type ModelConfig struct {
	Provider   string                 `json:"provider" yaml:"provider"`
	Name       string                 `json:"name" yaml:"name"`
	APIKeyEnv  string                 `json:"apiKeyEnv,omitempty" yaml:"apiKeyEnv,omitempty"`
	BaseURL    string                 `json:"baseURL,omitempty" yaml:"baseURL,omitempty"`
	Parameters map[string]interface{} `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// SkillConfig describes a bundled skill.
type SkillConfig struct {
	Name    string                 `json:"name" yaml:"name"`
	Source  string                 `json:"source,omitempty" yaml:"source,omitempty"`
	Path    string                 `json:"path,omitempty" yaml:"path,omitempty"`
	Files   []string               `json:"files,omitempty" yaml:"files,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

// PluginConfig describes a bundled harness plugin.
type PluginConfig struct {
	Name    string                 `json:"name" yaml:"name"`
	Source  string                 `json:"source,omitempty" yaml:"source,omitempty"`
	Path    string                 `json:"path,omitempty" yaml:"path,omitempty"`
	Files   []string               `json:"files,omitempty" yaml:"files,omitempty"`
	Config  map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

// MCPConfig describes an MCP server.
type MCPConfig struct {
	Name        string                 `json:"name" yaml:"name"`
	Source      string                 `json:"source,omitempty" yaml:"source,omitempty"`
	Command     string                 `json:"command,omitempty" yaml:"command,omitempty"`
	Args        []string               `json:"args,omitempty" yaml:"args,omitempty"`
	Transport   string                 `json:"transport,omitempty" yaml:"transport,omitempty"`
	Env         map[string]string      `json:"env,omitempty" yaml:"env,omitempty"`
	Config      map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
	HealthCheck *HealthCheckConfig     `json:"healthCheck,omitempty" yaml:"healthCheck,omitempty"`
}

// HealthCheckConfig describes a process health check.
type HealthCheckConfig struct {
	Command     string        `json:"command" yaml:"command"`
	Args        []string      `json:"args,omitempty" yaml:"args,omitempty"`
	Interval    time.Duration `json:"interval,omitempty" yaml:"interval,omitempty"`
	Timeout     time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries     int           `json:"retries,omitempty" yaml:"retries,omitempty"`
	StartPeriod time.Duration `json:"startPeriod,omitempty" yaml:"startPeriod,omitempty"`
}

// ToolsConfig describes tool permissions and custom tools.
type ToolsConfig struct {
	Permissions []ToolPermissionConfig `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Custom      []CustomToolConfig     `json:"custom,omitempty" yaml:"custom,omitempty"`
}

// ToolPermissionConfig allows or denies a specific tool.
type ToolPermissionConfig struct {
	Tool  string `json:"tool" yaml:"tool"`
	Allow bool   `json:"allow" yaml:"allow"`
	Scope string `json:"scope,omitempty" yaml:"scope,omitempty"`
}

// CustomToolConfig defines a custom tool.
type CustomToolConfig struct {
	Name        string   `json:"name" yaml:"name"`
	Command     string   `json:"command" yaml:"command"`
	Args        []string `json:"args,omitempty" yaml:"args,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	WorkDir     string   `json:"workDir,omitempty" yaml:"workDir,omitempty"`
}

// AgentConfig describes an OCX agent persona.
type AgentConfig struct {
	Name   string                 `json:"name" yaml:"name"`
	Prompt string                 `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Config map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

// CommandConfig describes an OCX command workflow.
type CommandConfig struct {
	Name   string                 `json:"name" yaml:"name"`
	Prompt string                 `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Args   []string               `json:"args,omitempty" yaml:"args,omitempty"`
	Config map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

// BundleConfig records a resolved OCX bundle.
type BundleConfig struct {
	Name       string   `json:"name" yaml:"name"`
	Components []string `json:"components,omitempty" yaml:"components,omitempty"`
}

// GPUConfig describes GPU passthrough configuration.
type GPUConfig struct {
	Enabled      bool     `json:"enabled" yaml:"enabled"`
	Devices      []string `json:"devices,omitempty" yaml:"devices,omitempty"`
	Runtime      string   `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Capabilities []string `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
}

// SecretsConfig describes secret injection configuration.
type SecretsConfig struct {
	Files []SecretFileConfig `json:"files,omitempty" yaml:"files,omitempty"`
	Env   []string           `json:"env,omitempty" yaml:"env,omitempty"`
}

// SecretFileConfig describes a mounted secret file.
type SecretFileConfig struct {
	Source string `json:"source" yaml:"source"`
	Target string `json:"target" yaml:"target"`
	Env    string `json:"env,omitempty" yaml:"env,omitempty"`
	Mode   string `json:"mode,omitempty" yaml:"mode,omitempty"`
}

// RuntimeConfig describes container runtime configuration.
type RuntimeConfig struct {
	Workdir     string            `json:"workdir,omitempty" yaml:"workdir,omitempty"`
	Env         map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	Mounts      []MountConfig     `json:"mounts,omitempty" yaml:"mounts,omitempty"`
	Interactive bool              `json:"interactive,omitempty" yaml:"interactive,omitempty"`
	Ports       []PortConfig      `json:"ports,omitempty" yaml:"ports,omitempty"`
	User        string            `json:"user,omitempty" yaml:"user,omitempty"`
}

// MountConfig describes a filesystem mount.
type MountConfig struct {
	Type     string   `json:"type" yaml:"type"`
	Source   string   `json:"source" yaml:"source"`
	Target   string   `json:"target" yaml:"target"`
	ReadOnly bool     `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	Options  []string `json:"options,omitempty" yaml:"options,omitempty"`
}

// PortConfig describes a port mapping.
type PortConfig struct {
	Host      int    `json:"host" yaml:"host"`
	Container int    `json:"container" yaml:"container"`
	Protocol  string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}
