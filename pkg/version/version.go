// Package version provides build-time version information for the agentbox binary.
// Values are injected via -ldflags at build time by the Makefile.
package version

import (
	"fmt"
	"runtime"
)

// These variables are set at build time via -ldflags.
var (
	// Version is the semantic version or git describe output.
	Version = "dev"

	// Commit is the short git commit hash.
	Commit = "unknown"

	// BuildDate is the UTC build timestamp.
	BuildDate = "unknown"
)

// Info returns a formatted version string.
func Info() string {
	return fmt.Sprintf("agentbox %s (commit: %s, built: %s, %s/%s)",
		Version, Commit, BuildDate, runtime.GOOS, runtime.GOARCH)
}

// Short returns just the version string.
func Short() string {
	return Version
}
