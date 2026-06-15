package guardrails

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// CommandRules defines the allow/deny rules for command execution within the
// container. Deny rules are evaluated first and take precedence over allow
// rules. If no rules match, the default policy is applied.
type CommandRules struct {
	// Allow is a list of glob patterns for permitted commands.
	// Examples: "git *", "npm *", "python3 *"
	Allow []string `yaml:"allow"`

	// Deny is a list of glob patterns for denied commands.
	// Examples: "sudo", "rm -rf /", "chmod *"
	// Deny rules always take precedence over allow rules.
	Deny []string `yaml:"deny"`

	// DefaultPolicy is the policy applied when no allow or deny rule matches.
	// Valid values are "allow" and "deny". Defaults to "deny" if empty.
	DefaultPolicy string `yaml:"default_policy"`

	// MaxExecutionTime is the maximum duration a command is allowed to run.
	// Commands running longer than this are forcibly terminated. Zero means
	// no time limit.
	MaxExecutionTime time.Duration `yaml:"max_execution_time"`
}

// CommandChecker evaluates commands against allow/deny rules using glob
// pattern matching. It provides audit logging for denied commands.
//
// CommandChecker is safe for concurrent use from multiple goroutines once
// constructed; its internal state is read-only after initialization.
type CommandChecker struct {
	rules  CommandRules
	logger *log.Logger
}

// NewCommandChecker constructs a new [CommandChecker] from the supplied
// [CommandRules]. It normalizes the default policy and prepares internal
// state for efficient command evaluation.
func NewCommandChecker(rules CommandRules) *CommandChecker {
	if rules.DefaultPolicy == "" {
		rules.DefaultPolicy = "deny"
	}

	logger := log.Default().With("component", "guardrails.commands")

	return &CommandChecker{
		rules:  rules,
		logger: logger,
	}
}

// Check evaluates whether the given command string is permitted. It returns
// whether the command is allowed and a human-readable reason string.
//
// Evaluation order:
//  1. Deny rules are checked first. If any deny pattern matches, the command
//     is rejected immediately.
//  2. Allow rules are checked next. If any allow pattern matches, the command
//     is permitted.
//  3. If no rules match, the default policy is applied.
func (c *CommandChecker) Check(cmd string) (allowed bool, reason string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false, "empty command"
	}

	// Extract the base command name for matching.
	cmdName := extractCommandName(cmd)

	// Phase 1: Check deny rules first (highest priority).
	for _, pattern := range c.rules.Deny {
		if matchCommand(pattern, cmd, cmdName) {
			c.auditDenied(cmd, pattern)
			return false, fmt.Sprintf("command matches deny rule: %q", pattern)
		}
	}

	// Phase 2: Check allow rules.
	for _, pattern := range c.rules.Allow {
		if matchCommand(pattern, cmd, cmdName) {
			return true, fmt.Sprintf("command matches allow rule: %q", pattern)
		}
	}

	// Phase 3: Apply default policy.
	if c.rules.DefaultPolicy == "allow" {
		return true, "no matching rule; default policy is allow"
	}
	c.auditDenied(cmd, "<default deny>")
	return false, "no matching rule; default policy is deny"
}

// MaxExecutionTime returns the configured maximum execution time for commands.
// A zero value indicates no time limit.
func (c *CommandChecker) MaxExecutionTime() time.Duration {
	return c.rules.MaxExecutionTime
}

// matchCommand tests whether a glob pattern matches the given command. It
// tests against both the full command string and the extracted base command
// name to support patterns like "sudo" (exact) and "git *" (prefix glob).
func matchCommand(pattern, fullCmd, cmdName string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}

	// Try matching against the full command string first.
	if matched, _ := filepath.Match(pattern, fullCmd); matched {
		return true
	}

	// Try matching against just the command name.
	if matched, _ := filepath.Match(pattern, cmdName); matched {
		return true
	}

	// Handle the common case of "cmd *" patterns: if the pattern ends with
	// " *", check whether the command starts with the prefix.
	if strings.HasSuffix(pattern, " *") {
		prefix := strings.TrimSuffix(pattern, " *")
		if cmdName == prefix || strings.HasPrefix(fullCmd, prefix+" ") {
			return true
		}
	}

	return false
}

// extractCommandName returns the first whitespace-delimited token of the
// command string, which is typically the executable name.
func extractCommandName(cmd string) string {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// auditDenied logs a denied command for audit purposes. This creates a
// structured log entry that can be consumed by monitoring systems.
func (c *CommandChecker) auditDenied(cmd, matchedPattern string) {
	c.logger.Warn("command denied",
		"cmd", cmd,
		"matched_pattern", matchedPattern,
		"action", "audit",
	)
}
