// Package guardrails provides a security enforcement engine that evaluates
// whether operations are permitted based on a manifest's guardrail configuration.
// It covers command execution policy, filesystem access control, and resource
// limits. The engine is designed to be used at container init time and can also
// be compiled into the init binary for lightweight, dependency-free enforcement.
package guardrails

import (
	"fmt"
	"sync"

	"github.com/charmbracelet/log"
)

// FileOp represents a filesystem operation type for access control decisions.
type FileOp int

const (
	// FileOpRead represents a read operation on a file or directory.
	FileOpRead FileOp = iota
	// FileOpWrite represents a write operation on a file or directory.
	FileOpWrite
	// FileOpExec represents an execute operation on a file.
	FileOpExec
)

// String returns the human-readable name of the file operation.
func (op FileOp) String() string {
	switch op {
	case FileOpRead:
		return "read"
	case FileOpWrite:
		return "write"
	case FileOpExec:
		return "exec"
	default:
		return fmt.Sprintf("unknown(%d)", int(op))
	}
}

// GuardrailConfig holds the complete guardrail configuration parsed from a
// manifest YAML file. It is the top-level configuration object that is passed
// to [NewEngine] to construct a fully initialized guardrail engine.
type GuardrailConfig struct {
	// Commands defines rules governing which shell commands may be executed.
	Commands CommandRules `yaml:"commands"`
	// Filesystem defines rules governing filesystem read/write access.
	Filesystem FilesystemRules `yaml:"filesystem"`
	// Resources defines hard limits on memory, CPU, processes, and open files.
	Resources ResourceLimits `yaml:"resources"`
}

// Engine is the main guardrail evaluation engine. It holds pre-compiled
// checkers for each guardrail domain (commands, filesystem, resources) and
// exposes a simple allow/deny API for use at container init time.
//
// Engine is safe for concurrent use from multiple goroutines.
type Engine struct {
	config     GuardrailConfig
	cmdChecker *CommandChecker
	fsChecker  *FilesystemChecker
	resLimiter *ResourceLimiter

	mu     sync.RWMutex
	logger *log.Logger
}

// NewEngine constructs a new [Engine] from the supplied [GuardrailConfig].
// It pre-compiles pattern matchers for command and filesystem rules so that
// subsequent checks are efficient. If the configuration is invalid, NewEngine
// panics; callers should validate the config before constructing the engine.
func NewEngine(config GuardrailConfig) *Engine {
	logger := log.Default().With("component", "guardrails")

	e := &Engine{
		config:     config,
		cmdChecker: NewCommandChecker(config.Commands),
		fsChecker:  NewFilesystemChecker(config.Filesystem),
		resLimiter: NewResourceLimiter(config.Resources),
		logger:     logger,
	}

	logger.Info("guardrail engine initialized",
		"allow_commands", len(config.Commands.Allow),
		"deny_commands", len(config.Commands.Deny),
		"writable_paths", len(config.Filesystem.WritablePaths),
		"readable_paths", len(config.Filesystem.ReadablePaths),
		"denied_paths", len(config.Filesystem.DeniedPaths),
	)

	return e
}

// CheckCommand evaluates whether the given command string is permitted by the
// configured command guardrails. It returns whether the command is allowed and
// a human-readable reason explaining the decision. Deny rules always take
// precedence over allow rules.
func (e *Engine) CheckCommand(cmd string) (allowed bool, reason string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	allowed, reason = e.cmdChecker.Check(cmd)
	if !allowed {
		e.logger.Warn("command denied", "cmd", cmd, "reason", reason)
	} else {
		e.logger.Debug("command allowed", "cmd", cmd)
	}
	return allowed, reason
}

// CheckFilePath evaluates whether the given filesystem path is permitted for
// the specified operation. It returns whether access is allowed and a
// human-readable reason explaining the decision. Denied paths take highest
// priority, followed by writable/readable whitelists.
func (e *Engine) CheckFilePath(path string, op FileOp) (allowed bool, reason string) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	allowed, reason = e.fsChecker.Check(path, op)
	if !allowed {
		e.logger.Warn("file access denied", "path", path, "op", op, "reason", reason)
	} else {
		e.logger.Debug("file access allowed", "path", path, "op", op)
	}
	return allowed, reason
}

// ApplyResourceLimits applies the configured resource limits by generating
// the appropriate cgroup settings or runtime flags. This is typically called
// once during container initialization. It returns an error if any limits
// cannot be applied.
func (e *Engine) ApplyResourceLimits() error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	e.logger.Info("applying resource limits")
	if err := e.resLimiter.Apply(); err != nil {
		e.logger.Error("failed to apply resource limits", "err", err)
		return fmt.Errorf("applying resource limits: %w", err)
	}
	e.logger.Info("resource limits applied successfully")
	return nil
}

// RuntimeFlags returns all container runtime flags that should be passed to
// docker or podman to enforce the configured resource limits.
func (e *Engine) RuntimeFlags() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.resLimiter.RuntimeFlags()
}

// Config returns a copy of the engine's guardrail configuration.
func (e *Engine) Config() GuardrailConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return e.config
}
