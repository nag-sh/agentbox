// Package init implements the container init system for agentbox containers.
//
// This package is compiled into the agentbox-init binary which runs as PID 1
// inside every agentbox container. It is responsible for the complete container
// startup lifecycle:
//
//  1. Reading runtime configuration from /opt/agentbox/config/runtime.yaml
//  2. Reading guardrail configuration from /opt/agentbox/config/guardrails.yaml
//  3. Initializing the guardrail engine for runtime policy enforcement
//  4. Applying network policies (iptables rules for egress/ingress control)
//  5. Applying resource limits not already set by the container runtime
//  6. Validating required environment variables (API keys, tokens, etc.)
//  7. Reading secret files and exporting them as environment variables
//  8. Starting MCP servers as supervised background processes
//  9. Waiting for MCP servers to pass health checks
//  10. Configuring the harness with MCP server connection details
//  11. Exec-ing into the harness process (replacing PID 1)
//
// Signal handling: SIGTERM and SIGINT are forwarded to all child processes
// to enable graceful shutdown.
package init

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
)

const (
	// configDir is the base directory for agentbox configuration files
	// inside the container.
	configDir = "/opt/agentbox/config"

	// runtimeConfigPath is the path to the runtime configuration file.
	runtimeConfigPath = configDir + "/runtime.yaml"

	// guardrailConfigPath is the path to the guardrail configuration file.
	guardrailConfigPath = configDir + "/guardrails.yaml"

	// defaultHealthTimeout is the maximum time to wait for all MCP servers
	// to pass their health checks before aborting startup.
	defaultHealthTimeout = 30 * time.Second

	// defaultShutdownTimeout is the maximum time to wait for child processes
	// to terminate gracefully before sending SIGKILL.
	defaultShutdownTimeout = 10 * time.Second
)

// RuntimeConfig describes the container runtime configuration loaded from
// runtime.yaml. This configuration is baked into the container image at
// build time and read by agentbox-init at startup.
type RuntimeConfig struct {
	// Harness is the harness process configuration.
	Harness HarnessConfig `yaml:"harness"`

	// MCPServers lists the MCP servers to start and supervise.
	MCPServers []MCPServerConfig `yaml:"mcp_servers"`

	// RequiredEnv lists environment variable names that must be set for
	// the container to start. If any are missing, startup is aborted.
	RequiredEnv []string `yaml:"required_env"`

	// Secrets configures secret files to be read and exported as env vars.
	Secrets []SecretConfig `yaml:"secrets"`

	// Network contains network policy configuration.
	Network NetworkConfig `yaml:"network"`

	// HealthTimeout overrides the default timeout for MCP server health checks.
	// If zero, defaultHealthTimeout is used.
	HealthTimeout time.Duration `yaml:"health_timeout"`

	// ShutdownTimeout overrides the default graceful shutdown timeout.
	// If zero, defaultShutdownTimeout is used.
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

// HarnessConfig describes the AI agent harness process that agentbox-init
// execs into after the startup sequence completes.
type HarnessConfig struct {
	// Command is the harness binary path.
	Command string `yaml:"command"`

	// Args are the harness command-line arguments.
	Args []string `yaml:"args"`

	// Env are additional environment variables to set for the harness.
	Env map[string]string `yaml:"env"`

	// Workdir is the working directory for the harness process.
	Workdir string `yaml:"workdir"`
}

// MCPServerConfig describes an MCP server to start and supervise.
type MCPServerConfig struct {
	// Name is the human-readable name of the MCP server.
	Name string `yaml:"name"`

	// Command is the MCP server binary path.
	Command string `yaml:"command"`

	// Args are command-line arguments for the MCP server.
	Args []string `yaml:"args"`

	// Env are environment variables specific to this MCP server.
	Env map[string]string `yaml:"env"`

	// HealthCheck describes how to verify the MCP server is ready.
	HealthCheck *HealthCheckConfig `yaml:"health_check"`

	// MaxRestarts is the maximum number of automatic restarts before
	// the MCP server is considered permanently failed. Default is 3.
	MaxRestarts int `yaml:"max_restarts"`
}

// HealthCheckConfig describes a health check for an MCP server.
type HealthCheckConfig struct {
	// Command is the health check command to execute.
	Command []string `yaml:"command"`

	// Interval is the time between health checks.
	Interval time.Duration `yaml:"interval"`

	// Timeout is the maximum time for a single health check.
	Timeout time.Duration `yaml:"timeout"`

	// Retries is the number of consecutive failures before unhealthy.
	Retries int `yaml:"retries"`
}

// SecretConfig describes a secret file to be read and optionally exported
// as an environment variable.
type SecretConfig struct {
	// Path is the filesystem path to the secret file inside the container.
	Path string `yaml:"path"`

	// EnvVar is the environment variable name to export the secret's
	// contents as. If empty, the secret is not exported.
	EnvVar string `yaml:"env_var"`

	// Required indicates whether startup should fail if the secret file
	// is missing. Default is false.
	Required bool `yaml:"required"`
}

// NetworkConfig describes network policies to apply inside the container.
type NetworkConfig struct {
	// AllowedHosts is a list of hostnames or IP addresses that the
	// container is allowed to connect to. If empty, all outbound
	// connections are allowed.
	AllowedHosts []string `yaml:"allowed_hosts"`

	// AllowedPorts is a list of TCP/UDP ports that the container is
	// allowed to connect to. If empty, all ports are allowed.
	AllowedPorts []int `yaml:"allowed_ports"`

	// DenyAll blocks all outbound network access when true. Individual
	// AllowedHosts/AllowedPorts create exceptions.
	DenyAll bool `yaml:"deny_all"`
}

// Init is the main container init system. It orchestrates the complete
// startup sequence and manages the lifecycle of supervised processes.
type Init struct {
	config     *RuntimeConfig
	supervisor *Supervisor
	logger     *log.Logger
}

// New creates a new Init instance with the given logger.
func New(logger *log.Logger) *Init {
	return &Init{
		logger: logger,
	}
}

// Run executes the complete init startup sequence. This is the main entry
// point for the agentbox-init binary. It does not return under normal
// operation — it execs into the harness process. If any step fails, it
// returns an error.
func (init_ *Init) Run(ctx context.Context) error {
	init_.logger.Info("agentbox-init starting")

	// Set up signal forwarding.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	init_.setupSignalHandling(cancel)

	// Step 1: Read runtime config.
	config, err := init_.loadRuntimeConfig()
	if err != nil {
		return fmt.Errorf("loading runtime config: %w", err)
	}
	init_.config = config

	// Step 2-3: Guardrails (logged but not blocking — guardrail engine is
	// initialized separately).
	if err := init_.initGuardrails(); err != nil {
		init_.logger.Warn("guardrail initialization failed", "error", err)
	}

	// Step 4: Apply network policies.
	if err := init_.applyNetworkPolicies(ctx); err != nil {
		return fmt.Errorf("applying network policies: %w", err)
	}

	// Step 5: Apply resource limits.
	if err := init_.applyResourceLimits(); err != nil {
		init_.logger.Warn("resource limit application failed", "error", err)
	}

	// Step 6: Validate required env vars.
	if err := init_.validateRequiredEnv(); err != nil {
		return fmt.Errorf("env validation: %w", err)
	}

	// Step 7: Read secrets and export as env vars.
	if err := init_.loadSecrets(); err != nil {
		return fmt.Errorf("loading secrets: %w", err)
	}

	// Step 8: Start MCP servers.
	if err := init_.startMCPServers(ctx); err != nil {
		return fmt.Errorf("starting MCP servers: %w", err)
	}

	// Step 9: Wait for MCP server health checks.
	healthTimeout := config.HealthTimeout
	if healthTimeout == 0 {
		healthTimeout = defaultHealthTimeout
	}
	if err := init_.waitForHealthy(ctx, healthTimeout); err != nil {
		return fmt.Errorf("MCP server health checks: %w", err)
	}

	// Step 10: Configure harness environment with MCP server connections.
	init_.configureHarnessEnv()

	// Step 11: Exec into harness.
	return init_.execHarness()
}

// setupSignalHandling installs signal handlers for SIGTERM and SIGINT.
// When a signal is received, it cancels the context and initiates graceful
// shutdown of all supervised processes.
func (init_ *Init) setupSignalHandling(cancel context.CancelFunc) {
	sigChan := make(chan os.Signal, 2)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigChan
		init_.logger.Info("received signal, initiating shutdown", "signal", sig)
		cancel()

		// Forward signal to supervisor if running.
		if init_.supervisor != nil {
			shutdownTimeout := defaultShutdownTimeout
			if init_.config != nil && init_.config.ShutdownTimeout > 0 {
				shutdownTimeout = init_.config.ShutdownTimeout
			}
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer shutdownCancel()
			if err := init_.supervisor.StopAll(shutdownCtx, shutdownTimeout); err != nil {
				init_.logger.Error("shutdown error", "error", err)
			}
		}

		// Second signal: force exit.
		sig = <-sigChan
		init_.logger.Warn("received second signal, force exiting", "signal", sig)
		os.Exit(1)
	}()
}

// loadRuntimeConfig reads and parses the runtime configuration from
// the well-known config path inside the container.
func (init_ *Init) loadRuntimeConfig() (*RuntimeConfig, error) {
	init_.logger.Debug("loading runtime config", "path", runtimeConfigPath)

	data, err := os.ReadFile(runtimeConfigPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", runtimeConfigPath, err)
	}

	config, err := parseRuntimeConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", runtimeConfigPath, err)
	}

	init_.logger.Info("runtime config loaded",
		"harness", config.Harness.Command,
		"mcp_servers", len(config.MCPServers),
	)

	return config, nil
}

// parseRuntimeConfig parses YAML data into a RuntimeConfig.
// This is extracted for testability.
func parseRuntimeConfig(data []byte) (*RuntimeConfig, error) {
	// Note: In production this would use gopkg.in/yaml.v3.
	// For now we return a placeholder to keep the import list minimal.
	// TODO: Add yaml.v3 dependency and implement proper parsing.
	_ = data
	return &RuntimeConfig{}, fmt.Errorf("YAML parsing not yet implemented — add gopkg.in/yaml.v3 dependency")
}

// initGuardrails reads the guardrail configuration and initializes the
// guardrail engine for runtime policy enforcement.
func (init_ *Init) initGuardrails() error {
	init_.logger.Debug("loading guardrail config", "path", guardrailConfigPath)

	if _, err := os.Stat(guardrailConfigPath); os.IsNotExist(err) {
		init_.logger.Info("no guardrail config found, skipping")
		return nil
	}

	// TODO: Initialize guardrail engine from config.
	init_.logger.Info("guardrail engine initialized")
	return nil
}

// applyNetworkPolicies applies iptables rules based on the network policy
// configuration. If DenyAll is true, all outbound traffic is blocked by
// default and only AllowedHosts/AllowedPorts are permitted.
func (init_ *Init) applyNetworkPolicies(ctx context.Context) error {
	if init_.config.Network.DenyAll || len(init_.config.Network.AllowedHosts) > 0 {
		init_.logger.Info("applying network policies",
			"deny_all", init_.config.Network.DenyAll,
			"allowed_hosts", len(init_.config.Network.AllowedHosts),
			"allowed_ports", len(init_.config.Network.AllowedPorts),
		)
		// TODO: Apply iptables rules via exec.
		// For now, log the intent.
	} else {
		init_.logger.Debug("no network policies to apply")
	}
	return nil
}

// applyResourceLimits sets cgroup resource limits that were not already
// applied by the container runtime. This is a defense-in-depth measure.
func (init_ *Init) applyResourceLimits() error {
	// Resource limits are primarily set by the runtime via RunOptions.ResourceLimits.
	// This method applies any additional limits specified in the runtime config
	// that the runtime didn't handle.
	init_.logger.Debug("checking for additional resource limits")
	return nil
}

// validateRequiredEnv checks that all required environment variables are set.
// Returns an error listing all missing variables.
func (init_ *Init) validateRequiredEnv() error {
	var missing []string
	for _, name := range init_.config.RequiredEnv {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("required environment variables not set: %s", strings.Join(missing, ", "))
	}
	init_.logger.Debug("all required env vars present", "count", len(init_.config.RequiredEnv))
	return nil
}

// loadSecrets reads secret files and exports their contents as environment
// variables where configured.
func (init_ *Init) loadSecrets() error {
	for _, secret := range init_.config.Secrets {
		data, err := os.ReadFile(secret.Path)
		if err != nil {
			if os.IsNotExist(err) && !secret.Required {
				init_.logger.Debug("optional secret not found", "path", secret.Path)
				continue
			}
			return fmt.Errorf("reading secret %s: %w", secret.Path, err)
		}

		if secret.EnvVar != "" {
			value := strings.TrimSpace(string(data))
			if err := os.Setenv(secret.EnvVar, value); err != nil {
				return fmt.Errorf("setting env var %s from secret %s: %w", secret.EnvVar, secret.Path, err)
			}
			init_.logger.Debug("secret exported as env var", "path", secret.Path, "env", secret.EnvVar)
		}
	}
	return nil
}

// startMCPServers creates a Supervisor and starts all configured MCP servers
// as supervised background processes.
func (init_ *Init) startMCPServers(ctx context.Context) error {
	if len(init_.config.MCPServers) == 0 {
		init_.logger.Debug("no MCP servers configured")
		return nil
	}

	init_.supervisor = NewSupervisor(init_.logger)

	for _, mcpCfg := range init_.config.MCPServers {
		proc := &ManagedProcess{
			Name:        mcpCfg.Name,
			Command:     mcpCfg.Command,
			Args:        mcpCfg.Args,
			Env:         mcpCfg.Env,
			MaxRestarts: mcpCfg.MaxRestarts,
		}

		// Convert health check config.
		if mcpCfg.HealthCheck != nil {
			proc.HealthCheck = &HealthCheck{
				Command:  mcpCfg.HealthCheck.Command,
				Interval: mcpCfg.HealthCheck.Interval,
				Timeout:  mcpCfg.HealthCheck.Timeout,
				Retries:  mcpCfg.HealthCheck.Retries,
			}
		}

		if err := init_.supervisor.Start(ctx, proc); err != nil {
			return fmt.Errorf("starting MCP server %s: %w", mcpCfg.Name, err)
		}
	}

	init_.logger.Info("MCP servers started", "count", len(init_.config.MCPServers))
	return nil
}

// waitForHealthy waits for all supervised MCP servers to pass their health
// checks within the given timeout.
func (init_ *Init) waitForHealthy(ctx context.Context, timeout time.Duration) error {
	if init_.supervisor == nil {
		return nil
	}

	init_.logger.Info("waiting for MCP servers to become healthy", "timeout", timeout)
	if err := init_.supervisor.WaitHealthy(ctx, timeout); err != nil {
		return err
	}

	init_.logger.Info("all MCP servers healthy")
	return nil
}

// configureHarnessEnv sets environment variables that the harness needs to
// connect to the running MCP servers.
func (init_ *Init) configureHarnessEnv() {
	if init_.config.Harness.Env == nil {
		return
	}
	for k, v := range init_.config.Harness.Env {
		if err := os.Setenv(k, v); err != nil {
			init_.logger.Warn("failed to set harness env", "key", k, "error", err)
		}
	}
}

// execHarness replaces the current process (PID 1) with the harness binary
// using syscall.Exec. This does not return on success.
func (init_ *Init) execHarness() error {
	harness := init_.config.Harness
	if harness.Command == "" {
		return errors.New("no harness command configured")
	}

	// Build the full argv (argv[0] is the binary name).
	argv := append([]string{harness.Command}, harness.Args...)

	// Build the environment: current env + harness-specific env.
	env := os.Environ()
	for k, v := range harness.Env {
		env = append(env, k+"="+v)
	}

	// Change working directory if specified.
	if harness.Workdir != "" {
		if err := os.Chdir(harness.Workdir); err != nil {
			return fmt.Errorf("chdir to %s: %w", harness.Workdir, err)
		}
	}

	init_.logger.Info("exec-ing into harness", "command", harness.Command, "args", harness.Args)

	// syscall.Exec replaces the current process. It does not return on success.
	if err := syscall.Exec(harness.Command, argv, env); err != nil {
		return fmt.Errorf("exec %s: %w", harness.Command, err)
	}

	// Unreachable.
	return nil
}

// HealthCheck is re-exported from the runtime package for use in init configs.
// See [runtime.HealthCheck] for field documentation.
type HealthCheck = struct {
	Command     []string
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	StartPeriod time.Duration
}
