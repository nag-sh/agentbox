package guardrails

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
)

// FilesystemRules defines the access control rules for filesystem operations
// within the container. Rules are evaluated in priority order: denied paths
// (highest), then writable paths, then readable paths.
type FilesystemRules struct {
	// WritablePaths is a whitelist of paths that may be written to.
	// Supports glob patterns (e.g., "/workspace/**", "/tmp/*").
	// Writable paths are implicitly readable.
	WritablePaths []string `yaml:"writable_paths"`

	// ReadablePaths is a whitelist of paths that may be read.
	// Supports glob patterns (e.g., "/usr/lib/**", "/etc/resolv.conf").
	// This is a superset of writable paths — writable paths are always
	// implicitly readable.
	ReadablePaths []string `yaml:"readable_paths"`

	// DeniedPaths is a blacklist of paths that are never accessible,
	// regardless of other rules. This has the highest priority.
	// Supports glob patterns (e.g., "/etc/shadow", "/root/**").
	DeniedPaths []string `yaml:"denied_paths"`

	// DefaultPolicy is the policy applied when no rule matches a path.
	// Valid values are "allow" and "deny". Defaults to "deny" if empty.
	DefaultPolicy string `yaml:"default_policy"`
}

// FilesystemChecker evaluates filesystem access requests against configured
// allow/deny rules. It performs path normalization before evaluation and
// supports glob patterns for flexible path matching.
//
// FilesystemChecker is safe for concurrent use from multiple goroutines once
// constructed; its internal state is read-only after initialization.
type FilesystemChecker struct {
	rules  FilesystemRules
	logger *log.Logger
}

// NewFilesystemChecker constructs a new [FilesystemChecker] from the supplied
// [FilesystemRules]. It normalizes the default policy and prepares the checker
// for evaluation.
func NewFilesystemChecker(rules FilesystemRules) *FilesystemChecker {
	if rules.DefaultPolicy == "" {
		rules.DefaultPolicy = "deny"
	}

	logger := log.Default().With("component", "guardrails.filesystem")

	return &FilesystemChecker{
		rules:  rules,
		logger: logger,
	}
}

// Check evaluates whether the given filesystem path is permitted for the
// specified operation. It returns whether access is allowed and a
// human-readable reason explaining the decision.
//
// Evaluation order:
//  1. The path is normalized and canonicalized.
//  2. Denied paths are checked first (highest priority). If any denied
//     pattern matches, access is rejected.
//  3. For write operations, writable paths are checked.
//  4. For read operations, both writable and readable paths are checked
//     (writable paths are implicitly readable).
//  5. If no rules match, the default policy is applied.
func (fc *FilesystemChecker) Check(filePath string, op FileOp) (allowed bool, reason string) {
	normalized := normalizePath(filePath)
	if normalized == "" {
		return false, "empty path"
	}

	// Phase 1: Check denied paths (highest priority).
	for _, pattern := range fc.rules.DeniedPaths {
		if matchPath(pattern, normalized) {
			fc.logger.Warn("path denied by blacklist",
				"path", normalized,
				"op", op,
				"pattern", pattern,
			)
			return false, fmt.Sprintf("path matches denied rule: %q", pattern)
		}
	}

	// Phase 2: Check operation-specific whitelists.
	switch op {
	case FileOpWrite:
		for _, pattern := range fc.rules.WritablePaths {
			if matchPath(pattern, normalized) {
				return true, fmt.Sprintf("path matches writable rule: %q", pattern)
			}
		}

	case FileOpRead, FileOpExec:
		// Writable paths are implicitly readable.
		for _, pattern := range fc.rules.WritablePaths {
			if matchPath(pattern, normalized) {
				return true, fmt.Sprintf("path matches writable rule (implicitly readable): %q", pattern)
			}
		}
		for _, pattern := range fc.rules.ReadablePaths {
			if matchPath(pattern, normalized) {
				return true, fmt.Sprintf("path matches readable rule: %q", pattern)
			}
		}
	}

	// Phase 3: Apply default policy.
	if fc.rules.DefaultPolicy == "allow" {
		return true, "no matching rule; default policy is allow"
	}
	return false, fmt.Sprintf("no matching rule for %s on %q; default policy is deny", op, normalized)
}

// normalizePath cleans and canonicalizes a filesystem path. It resolves "."
// and ".." components, removes trailing slashes, and ensures the path is
// absolute. Relative paths are rejected by returning an empty string.
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}

	// Clean the path to resolve . and .. components.
	p = path.Clean(p)

	// Reject relative paths — all filesystem guardrails operate on absolute
	// paths within the container filesystem.
	if !strings.HasPrefix(p, "/") {
		return ""
	}

	return p
}

// matchPath tests whether a glob pattern matches the given normalized path.
// It supports standard glob patterns and also handles the "**" recursive
// wildcard by testing whether the path starts with the pattern prefix.
func matchPath(pattern, filePath string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}

	// Clean the pattern for consistent matching.
	pattern = path.Clean(pattern)

	// Handle recursive wildcard "**" — matches any number of path segments.
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if filePath == prefix || strings.HasPrefix(filePath, prefix+"/") {
			return true
		}
	}

	// Exact match.
	if pattern == filePath {
		return true
	}

	// Standard glob match using filepath.Match. Note that filepath.Match
	// does not support "**", so we handle it above.
	if matched, err := filepath.Match(pattern, filePath); err == nil && matched {
		return true
	}

	// Check if the file path is under a directory specified by the pattern.
	// For example, pattern "/workspace" should match "/workspace/foo.txt".
	if strings.HasPrefix(filePath, pattern+"/") {
		return true
	}

	return false
}
