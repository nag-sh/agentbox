// Package main is the entrypoint for the agentbox CLI.
// Agentbox builds immutable OCI container images for AI agent harnesses
// from declarative YAML manifests.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nag-sh/agentbox/pkg/version"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agentbox",
		Short: "Build and run immutable AI agent container images",
		Long: `Agentbox builds immutable OCI container images for AI agent harnesses
from declarative YAML manifests. It handles harness installation, MCP server
configuration, skill/plugin bundling, security guardrails, and network policies.

Store and distribute everything — containers, skills, plugins, MCP servers —
as OCI artifacts in any OCI-compliant registry.`,
		Version:       version.Short(),
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.SetVersionTemplate(version.Info() + "\n")

	// Register all subcommands.
	cmd.AddCommand(
		buildCmd(),
		runCmd(),
		pushCmd(),
		pullCmd(),
		initCmd(),
		aliasCmd(),
		validateCmd(),
		listCmd(),
	)

	return cmd
}
