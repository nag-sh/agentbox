// Package manifest defines the declarative schema for agentbox container image
// specifications. An agentbox manifest (agentbox.yaml) describes everything
// needed to build an immutable OCI container image containing an AI agent
// harness, its configuration, skills, plugins, MCP servers, and runtime
// policies.
//
// The manifest schema is designed to be:
//   - Declarative: describe the desired state, not the steps to get there
//   - Composable: skills, plugins, and MCP servers can come from OCI registries or local paths
//   - Secure: guardrails, network policies, and secret management are first-class
//   - Runtime-agnostic: the same manifest works with podman, docker, or containerd
package manifest

import "time"

// APIVersion is the current manifest schema version.
const APIVersion = "agentbox/v1"

// Manifest is the top-level structure of an agentbox.yaml file.
// It follows a Kubernetes-inspired structure with apiVersion, kind, metadata, and spec.
type Manifest struct {
	// APIVersion identifies the schema version (e.g., "agentbox/v1").
	APIVersion string `yaml:"apiVersion" json:"apiVersion" validate:"required,eq=agentbox/v1"`

	// Kind identifies the type of manifest. Currently only "AgentImage" is supported.
	Kind string `yaml:"kind" json:"kind" validate:"required,eq=AgentImage"`

	// Metadata contains descriptive information about the agent image.
	Metadata Metadata `yaml:"metadata" json:"metadata" validate:"required"`

	// Spec contains the full specification for building and running the agent image.
	Spec Spec `yaml:"spec" json:"spec" validate:"required"`
}

// Metadata contains descriptive information about an agent image.
type Metadata struct {
	// Name is the human-readable name of the agent image.
	Name string `yaml:"name" json:"name" validate:"required,dns_rfc1035_label"`

	// Version is the semantic version of this agent image.
	Version string `yaml:"version" json:"version" validate:"required,semver"`

	// Description is an optional human-readable description.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Labels are arbitrary key-value metadata for organizing and filtering.
	Labels map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`

	// Annotations are arbitrary key-value metadata for tooling integration.
	Annotations map[string]string `yaml:"annotations,omitempty" json:"annotations,omitempty"`
}

// Spec contains the full specification for building and running an agent container image.
type Spec struct {
	// OS defines the base operating system layer.
	OS OSSpec `yaml:"os" json:"os" validate:"required"`

	// Harness defines the agent harness to install and configure.
	Harness HarnessSpec `yaml:"harness" json:"harness" validate:"required"`

	// Model defines the LLM model configuration.
	Model ModelSpec `yaml:"model" json:"model" validate:"required"`

	// Skills lists skills to bundle into the image.
	Skills []SkillSpec `yaml:"skills,omitempty" json:"skills,omitempty"`

	// MCP defines MCP server configurations.
	MCP MCPSpec `yaml:"mcp,omitempty" json:"mcp,omitempty"`

	// Plugins lists harness plugins to bundle.
	Plugins []PluginSpec `yaml:"plugins,omitempty" json:"plugins,omitempty"`

	// Tools defines tool permission configuration.
	Tools ToolsSpec `yaml:"tools,omitempty" json:"tools,omitempty"`

	// Guardrails defines security guardrails for the agent.
	Guardrails GuardrailsSpec `yaml:"guardrails,omitempty" json:"guardrails,omitempty"`

	// Network defines network ingress/egress policies.
	Network NetworkSpec `yaml:"network,omitempty" json:"network,omitempty"`

	// GPU defines GPU passthrough configuration.
	GPU GPUSpec `yaml:"gpu,omitempty" json:"gpu,omitempty"`

	// Secrets defines secret injection configuration.
	Secrets SecretsSpec `yaml:"secrets,omitempty" json:"secrets,omitempty"`

	// Runtime defines container runtime configuration.
	Runtime RuntimeSpec `yaml:"runtime,omitempty" json:"runtime,omitempty"`

	// OCX defines OCX component sources and registry configuration.
	OCX OCXSpec `yaml:"ocx,omitempty" json:"ocx,omitempty"`
}

// OSSpec defines the base operating system layer of the container image.
type OSSpec struct {
	// Base is the base container image reference (e.g., "alpine:3.21", "ubuntu:24.04").
	Base string `yaml:"base" json:"base" validate:"required"`

	// Packages lists OS-level packages to install via the base image's package manager.
	Packages []string `yaml:"packages,omitempty" json:"packages,omitempty"`

	// Shell is the default shell to use (e.g., "/bin/bash", "/bin/sh").
	Shell string `yaml:"shell,omitempty" json:"shell,omitempty"`
}

// HarnessName is the type for supported agent harness identifiers.
type HarnessName string

const (
	// HarnessOpenCode is the OpenCode agent harness.
	HarnessOpenCode HarnessName = "opencode"

	// HarnessGoose is the Goose agent harness (by Block / Linux Foundation AAIF).
	HarnessGoose HarnessName = "goose"

	// HarnessAider is the Aider agent harness (future support).
	HarnessAider HarnessName = "aider"

	// HarnessClaudeCode is the Claude Code agent harness (future support).
	HarnessClaudeCode HarnessName = "claude-code"
)

// HarnessSpec defines which agent harness to install and how to source it.
type HarnessSpec struct {
	// Name identifies the agent harness (e.g., "opencode", "goose").
	Name HarnessName `yaml:"name" json:"name" validate:"required,oneof=opencode goose aider claude-code"`

	// Version is the version to install. Use "latest" for the most recent release.
	Version string `yaml:"version" json:"version" validate:"required"`

	// Source is an optional OCI reference to pull the harness from.
	// If not specified, the harness is installed from its default source.
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

// ModelProvider is the type for supported LLM provider identifiers.
type ModelProvider string

const (
	ModelProviderAnthropic ModelProvider = "anthropic"
	ModelProviderOpenAI    ModelProvider = "openai"
	ModelProviderOllama    ModelProvider = "ollama"
	ModelProviderCustom    ModelProvider = "custom"
)

// ModelSpec defines the LLM model configuration.
type ModelSpec struct {
	// Provider identifies the LLM provider (e.g., "anthropic", "openai").
	Provider ModelProvider `yaml:"provider" json:"provider" validate:"required,oneof=anthropic openai ollama custom"`

	// Name is the model identifier (e.g., "claude-sonnet-4-20250514").
	Name string `yaml:"name" json:"name" validate:"required"`

	// APIKeyEnv is the environment variable name containing the API key.
	// The actual value is injected at runtime, never baked into the image.
	APIKeyEnv string `yaml:"apiKeyEnv,omitempty" json:"apiKeyEnv,omitempty"`

	// BaseURL is an optional custom API base URL (for custom/self-hosted providers).
	BaseURL string `yaml:"baseURL,omitempty" json:"baseURL,omitempty"`

	// Parameters are optional model parameters (temperature, maxTokens, etc.).
	Parameters map[string]interface{} `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// SkillSpec defines a skill to bundle into the image.
type SkillSpec struct {
	// Name is the skill identifier.
	Name string `yaml:"name" json:"name" validate:"required"`

	// Source is an OCI artifact reference to pull the skill from.
	// Mutually exclusive with Path.
	Source string `yaml:"source,omitempty" json:"source,omitempty"`

	// Path is a local directory path containing the skill.
	// Mutually exclusive with Source.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Config is optional skill-specific configuration.
	Config map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`
}

// MCPSpec defines the MCP (Model Context Protocol) server configuration.
type MCPSpec struct {
	// Servers lists the MCP servers to configure.
	Servers []MCPServerSpec `yaml:"servers,omitempty" json:"servers,omitempty"`
}

// MCPTransport is the transport type for MCP servers.
type MCPTransport string

const (
	MCPTransportStdio MCPTransport = "stdio"
	MCPTransportHTTP  MCPTransport = "http"
	MCPTransportSSE   MCPTransport = "sse"
)

// MCPServerSpec defines a single MCP server configuration.
type MCPServerSpec struct {
	// Name is the MCP server identifier.
	Name string `yaml:"name" json:"name" validate:"required"`

	// Source is an OCI artifact reference to pull the MCP server from.
	Source string `yaml:"source,omitempty" json:"source,omitempty"`

	// Command is the command to start the MCP server (if not using a source).
	Command string `yaml:"command,omitempty" json:"command,omitempty"`

	// Args are command-line arguments for the MCP server.
	Args []string `yaml:"args,omitempty" json:"args,omitempty"`

	// Transport is the MCP transport protocol (stdio, http, sse).
	Transport MCPTransport `yaml:"transport" json:"transport" validate:"required,oneof=stdio http sse"`

	// Env is environment variables to set for the MCP server process.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// Config is arbitrary configuration to pass to the MCP server.
	Config map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`

	// HealthCheck defines how to verify the MCP server is ready.
	HealthCheck *HealthCheckSpec `yaml:"healthCheck,omitempty" json:"healthCheck,omitempty"`
}

// HealthCheckSpec defines a health check for a process.
type HealthCheckSpec struct {
	// Command is a command to run to check health. Exit 0 = healthy.
	Command string `yaml:"command,omitempty" json:"command,omitempty"`

	// Interval is the time between health checks.
	Interval Duration `yaml:"interval,omitempty" json:"interval,omitempty"`

	// Timeout is the maximum time to wait for a health check response.
	Timeout Duration `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Retries is the number of consecutive failures before marking unhealthy.
	Retries int `yaml:"retries,omitempty" json:"retries,omitempty"`

	// StartPeriod is the grace period after startup before health checks begin.
	StartPeriod Duration `yaml:"startPeriod,omitempty" json:"startPeriod,omitempty"`
}

// PluginSpec defines a harness plugin to bundle into the image.
type PluginSpec struct {
	// Name is the plugin identifier.
	Name string `yaml:"name" json:"name" validate:"required"`

	// Source is an OCI artifact reference to pull the plugin from.
	Source string `yaml:"source,omitempty" json:"source,omitempty"`

	// Path is a local directory path containing the plugin.
	Path string `yaml:"path,omitempty" json:"path,omitempty"`

	// Config is optional plugin-specific configuration.
	Config map[string]interface{} `yaml:"config,omitempty" json:"config,omitempty"`
}

// ToolsSpec defines tool permission configuration for the agent.
type ToolsSpec struct {
	// Permissions defines per-tool access controls.
	Permissions []ToolPermission `yaml:"permissions,omitempty" json:"permissions,omitempty"`

	// Custom defines custom tool definitions.
	Custom []CustomTool `yaml:"custom,omitempty" json:"custom,omitempty"`
}

// ToolPermission defines whether a specific tool is allowed.
type ToolPermission struct {
	// Tool is the tool name (e.g., "run_command", "write_file").
	Tool string `yaml:"tool" json:"tool" validate:"required"`

	// Allow indicates whether the tool is permitted.
	Allow bool `yaml:"allow" json:"allow"`

	// Scope limits the tool to specific paths or patterns.
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
}

// CustomTool defines a custom tool that can be invoked by the agent.
type CustomTool struct {
	// Name is the tool name.
	Name string `yaml:"name" json:"name" validate:"required"`

	// Command is the executable to run.
	Command string `yaml:"command" json:"command" validate:"required"`

	// Args are default arguments.
	Args []string `yaml:"args,omitempty" json:"args,omitempty"`

	// Description is a human-readable description of what the tool does.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// WorkDir is the working directory for the tool.
	WorkDir string `yaml:"workDir,omitempty" json:"workDir,omitempty"`
}

// GuardrailsSpec defines security guardrails for the agent container.
type GuardrailsSpec struct {
	// Commands defines command execution restrictions.
	Commands CommandGuardrails `yaml:"commands,omitempty" json:"commands,omitempty"`

	// Filesystem defines filesystem access restrictions.
	Filesystem FilesystemGuardrails `yaml:"filesystem,omitempty" json:"filesystem,omitempty"`

	// Resources defines resource usage limits.
	Resources ResourceGuardrails `yaml:"resources,omitempty" json:"resources,omitempty"`
}

// CommandGuardrails defines which commands the agent is allowed to execute.
type CommandGuardrails struct {
	// Allow is a list of allowed command patterns (glob syntax).
	// If non-empty, only matching commands are allowed.
	Allow []string `yaml:"allow,omitempty" json:"allow,omitempty"`

	// Deny is a list of denied command patterns (glob syntax).
	// Deny rules take precedence over allow rules.
	Deny []string `yaml:"deny,omitempty" json:"deny,omitempty"`

	// DefaultPolicy is the policy applied when no rule matches ("allow" or "deny").
	// Defaults to "deny".
	DefaultPolicy string `yaml:"defaultPolicy,omitempty" json:"defaultPolicy,omitempty" validate:"omitempty,oneof=allow deny"`

	// MaxExecutionTime is the maximum time a single command may run.
	MaxExecutionTime Duration `yaml:"maxExecutionTime,omitempty" json:"maxExecutionTime,omitempty"`
}

// FilesystemGuardrails defines filesystem access restrictions.
type FilesystemGuardrails struct {
	// Writable is a list of paths the agent may write to.
	Writable []string `yaml:"writable,omitempty" json:"writable,omitempty"`

	// Readable is a list of paths the agent may read from.
	// Writable paths are implicitly readable.
	Readable []string `yaml:"readable,omitempty" json:"readable,omitempty"`

	// Deny is a list of paths that are always denied, regardless of other rules.
	Deny []string `yaml:"deny,omitempty" json:"deny,omitempty"`

	// DefaultPolicy is the policy applied when no rule matches ("allow" or "deny").
	DefaultPolicy string `yaml:"defaultPolicy,omitempty" json:"defaultPolicy,omitempty" validate:"omitempty,oneof=allow deny"`
}

// ResourceGuardrails defines container resource limits.
type ResourceGuardrails struct {
	// MaxMemory is the maximum memory (e.g., "4Gi", "512Mi").
	MaxMemory string `yaml:"maxMemory,omitempty" json:"maxMemory,omitempty"`

	// MaxCPUs is the maximum number of CPUs.
	MaxCPUs float64 `yaml:"maxCpus,omitempty" json:"maxCpus,omitempty"`

	// MaxProcesses is the maximum number of processes (PIDs).
	MaxProcesses int `yaml:"maxProcesses,omitempty" json:"maxProcesses,omitempty"`

	// MaxOpenFiles is the maximum number of open file descriptors.
	MaxOpenFiles int `yaml:"maxOpenFiles,omitempty" json:"maxOpenFiles,omitempty"`
}

// NetworkSpec defines network ingress and egress policies.
type NetworkSpec struct {
	// Egress defines outbound network access rules.
	Egress EgressSpec `yaml:"egress,omitempty" json:"egress,omitempty"`

	// Ingress defines inbound network access rules.
	Ingress IngressSpec `yaml:"ingress,omitempty" json:"ingress,omitempty"`
}

// DefaultPolicy is the default network policy when no rules match.
type DefaultPolicy string

const (
	DefaultPolicyAllow DefaultPolicy = "allow"
	DefaultPolicyDeny  DefaultPolicy = "deny"
)

// EgressSpec defines outbound network access rules.
type EgressSpec struct {
	// Allow lists permitted outbound destinations.
	Allow []EgressRule `yaml:"allow,omitempty" json:"allow,omitempty"`

	// Deny lists blocked outbound destinations.
	Deny []EgressRule `yaml:"deny,omitempty" json:"deny,omitempty"`

	// DefaultPolicy is the default action when no rules match ("allow" or "deny").
	DefaultPolicy DefaultPolicy `yaml:"defaultPolicy,omitempty" json:"defaultPolicy,omitempty" validate:"omitempty,oneof=allow deny"`
}

// EgressRule defines a single outbound network rule.
type EgressRule struct {
	// Host is a hostname or glob pattern (e.g., "api.anthropic.com", "*.github.com").
	Host string `yaml:"host" json:"host" validate:"required"`

	// Ports lists the allowed destination ports. Empty means all ports.
	Ports []int `yaml:"ports,omitempty" json:"ports,omitempty"`
}

// IngressSpec defines inbound network access rules.
type IngressSpec struct {
	// Allow lists permitted inbound connections.
	Allow []IngressRule `yaml:"allow,omitempty" json:"allow,omitempty"`

	// DefaultPolicy is the default action when no rules match ("allow" or "deny").
	DefaultPolicy DefaultPolicy `yaml:"defaultPolicy,omitempty" json:"defaultPolicy,omitempty" validate:"omitempty,oneof=allow deny"`
}

// IngressRule defines a single inbound network rule.
type IngressRule struct {
	// Port is the container port to allow inbound connections on.
	Port int `yaml:"port" json:"port" validate:"required,min=1,max=65535"`

	// Source restricts which source addresses may connect (e.g., "localhost", CIDR).
	Source string `yaml:"source,omitempty" json:"source,omitempty"`

	// Protocol is the network protocol ("tcp" or "udp"). Defaults to "tcp".
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty" validate:"omitempty,oneof=tcp udp"`
}

// GPUSpec defines GPU passthrough configuration.
type GPUSpec struct {
	// Enabled indicates whether GPU passthrough is active.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// Devices lists specific GPU device paths (e.g., ["/dev/nvidia0"]).
	// Empty means all available GPUs.
	Devices []string `yaml:"devices,omitempty" json:"devices,omitempty"`

	// Runtime is the GPU container runtime to use ("nvidia" or "amd").
	Runtime string `yaml:"runtime,omitempty" json:"runtime,omitempty" validate:"omitempty,oneof=nvidia amd"`

	// Capabilities lists required GPU capabilities (e.g., ["compute", "utility"]).
	Capabilities []string `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// SecretsSpec defines secret injection configuration.
type SecretsSpec struct {
	// Files lists secret files to mount into the container.
	Files []SecretFile `yaml:"files,omitempty" json:"files,omitempty"`

	// Env lists environment variable names to pass through from the host.
	// The values are never baked into the image.
	Env []string `yaml:"env,omitempty" json:"env,omitempty"`
}

// SecretFile defines a secret file to mount into the container.
type SecretFile struct {
	// Source is the host path to the secret file.
	Source string `yaml:"source" json:"source" validate:"required"`

	// Target is the container path where the secret will be mounted.
	Target string `yaml:"target" json:"target" validate:"required"`

	// Env optionally exports the file contents as an environment variable.
	Env string `yaml:"env,omitempty" json:"env,omitempty"`

	// Mode is the file permission mode (e.g., "0600"). Defaults to "0400".
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

// RuntimeSpec defines container runtime configuration.
type RuntimeSpec struct {
	// Workdir is the working directory inside the container. Defaults to "/workspace".
	Workdir string `yaml:"workdir,omitempty" json:"workdir,omitempty"`

	// Env is additional environment variables to set in the container.
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// Mounts defines filesystem mounts.
	Mounts []MountSpec `yaml:"mounts,omitempty" json:"mounts,omitempty"`

	// Interactive indicates whether to allocate a TTY for interactive use.
	Interactive bool `yaml:"interactive,omitempty" json:"interactive,omitempty"`

	// Ports defines port mappings from host to container.
	Ports []PortSpec `yaml:"ports,omitempty" json:"ports,omitempty"`

	// User is the user to run the container as (e.g., "agent", "1000:1000").
	User string `yaml:"user,omitempty" json:"user,omitempty"`
}

// MountSpec defines a filesystem mount for the container.
type MountSpec struct {
	// Type is the mount type: "bind", "volume", or "tmpfs".
	Type string `yaml:"type" json:"type" validate:"required,oneof=bind volume tmpfs"`

	// Source is the host path (for bind mounts) or volume name.
	// Use "." for the current directory.
	Source string `yaml:"source" json:"source" validate:"required"`

	// Target is the container path.
	Target string `yaml:"target" json:"target" validate:"required"`

	// ReadOnly makes the mount read-only.
	ReadOnly bool `yaml:"readOnly,omitempty" json:"readOnly,omitempty"`

	// Options are additional mount options.
	Options []string `yaml:"options,omitempty" json:"options,omitempty"`
}

// PortSpec defines a port mapping from host to container.
type PortSpec struct {
	// Host is the host port number.
	Host int `yaml:"host" json:"host" validate:"required,min=1,max=65535"`

	// Container is the container port number.
	Container int `yaml:"container" json:"container" validate:"required,min=1,max=65535"`

	// Protocol is "tcp" or "udp". Defaults to "tcp".
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty" validate:"omitempty,oneof=tcp udp"`
}

// Duration is a wrapper around time.Duration that supports YAML/JSON marshaling
// with human-readable strings like "300s", "5m", "1h".
type Duration struct {
	time.Duration
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

// MarshalYAML implements the yaml.Marshaler interface.
func (d Duration) MarshalYAML() (interface{}, error) {
	if d.Duration == 0 {
		return "", nil
	}
	return d.Duration.String(), nil
}

// OCXSpec declares OCX components to resolve and the registries used to
// resolve them. Components may also be referenced directly from
// SkillSpec.Source, PluginSpec.Source, or MCPServerSpec.Source using an
// "ocx://" or full OCI reference scheme.
type OCXSpec struct {
	// Registries maps an alias to an OCX registry URL or OCI repository prefix.
	Registries map[string]OCXRegistryRef `yaml:"registries,omitempty" json:"registries,omitempty"`

	// Components lists OCX components to resolve and merge into the image.
	Components []OCXComponentRef `yaml:"components,omitempty" json:"components,omitempty"`
}

// OCXComponentRef identifies an OCX component to resolve.
type OCXComponentRef struct {
	// Name is the component name.
	Name string `yaml:"name" json:"name" validate:"required"`

	// Source is an OCI reference or an alias-prefixed component reference.
	Source string `yaml:"source" json:"source" validate:"required"`

	// Version is the component version or tag. If empty, "latest" is used.
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

// OCXRegistryRef describes an OCX registry or OCI repository prefix.
type OCXRegistryRef struct {
	// URL is the registry base URL or OCI repository prefix.
	URL string `yaml:"url" json:"url" validate:"required,url"`

	// Headers are optional HTTP headers for HTTP registries.
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}
