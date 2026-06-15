package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nag-sh/agentbox/pkg/registry"
)

func pushCmd() *cobra.Command {
	var (
		tag string
	)

	cmd := &cobra.Command{
		Use:   "push [type] [path]",
		Short: "Push an artifact to an OCI registry",
		Long: `Push a skill, plugin, MCP server, or harness to an OCI registry as an OCI artifact.

Artifact types:
  skill    - Agent skill bundle
  plugin   - Harness plugin bundle
  mcp      - MCP server bundle
  harness  - Agent harness package`,
		Example: `  # Push a skill
  agentbox push skill ./my-skill --tag ghcr.io/myorg/agentbox/skill/my-skill:v1

  # Push an MCP server
  agentbox push mcp ./my-mcp-server --tag ghcr.io/myorg/agentbox/mcp/my-server:v1

  # Push a plugin
  agentbox push plugin ./my-plugin --tag ghcr.io/myorg/agentbox/plugin/my-plugin:v1`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			artifactType := args[0]
			path := args[1]

			// Validate artifact type.
			at, err := registry.ParseArtifactType(artifactType)
			if err != nil {
				return err
			}

			if tag == "" {
				return fmt.Errorf("--tag is required")
			}

			// Verify path exists.
			info, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("artifact path %s: %w", path, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("artifact path %s must be a directory", path)
			}

			// Create registry client.
			client, err := registry.NewClient(registry.ClientOptions{})
			if err != nil {
				return fmt.Errorf("creating registry client: %w", err)
			}

			// Push the artifact.
			store := registry.NewArtifactStore(client)
			if err := store.PushArtifact(cmd.Context(), tag, at, path, nil); err != nil {
				return fmt.Errorf("pushing artifact: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Pushed %s artifact to %s\n", artifactType, tag)
			return nil
		},
	}

	cmd.Flags().StringVarP(&tag, "tag", "t", "", "OCI reference tag (required)")
	_ = cmd.MarkFlagRequired("tag")

	return cmd
}

func pullCmd() *cobra.Command {
	var (
		output string
	)

	cmd := &cobra.Command{
		Use:   "pull [reference]",
		Short: "Pull an artifact from an OCI registry",
		Long:  `Pull a skill, plugin, MCP server, or harness from an OCI registry.`,
		Example: `  # Pull a skill
  agentbox pull ghcr.io/myorg/agentbox/skill/my-skill:v1

  # Pull to a specific directory
  agentbox pull ghcr.io/myorg/agentbox/skill/my-skill:v1 -o ./skills/`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := args[0]

			if output == "" {
				output = "."
			}

			// Create registry client.
			client, err := registry.NewClient(registry.ClientOptions{})
			if err != nil {
				return fmt.Errorf("creating registry client: %w", err)
			}

			// Pull the artifact.
			store := registry.NewArtifactStore(client)
			info, err := store.PullArtifact(cmd.Context(), ref, output)
			if err != nil {
				return fmt.Errorf("pulling artifact: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Pulled %s to %s (digest: %s)\n", ref, output, info.Digest)
			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Output directory (default: current directory)")

	return cmd
}
