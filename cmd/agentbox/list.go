package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nag-sh/agentbox/pkg/registry"
)

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [repo]",
		Short: "List artifacts in an OCI repository",
		Long:  `List all available skills, plugins, harnesses, and MCP servers in an OCI repository.`,
		Example: `  agentbox list ghcr.io/myorg/agentbox/skills`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo := args[0]

			client, err := registry.NewClient(registry.ClientOptions{})
			if err != nil {
				return fmt.Errorf("creating registry client: %w", err)
			}

			store := registry.NewArtifactStore(client)
			
			// Try listing all artifact types
			types := []registry.ArtifactType{
				registry.ArtifactTypeSkill,
				registry.ArtifactTypePlugin,
				registry.ArtifactTypeMCP,
				registry.ArtifactTypeHarness,
			}
			
			found := false
			for _, at := range types {
				artifacts, err := store.ListArtifacts(cmd.Context(), repo, at)
				if err != nil {
					continue // Might not support listing or no tags
				}
				
				if len(artifacts) > 0 {
					found = true
					fmt.Fprintf(os.Stdout, "--- %s ---\n", at)
					for _, a := range artifacts {
						fmt.Fprintf(os.Stdout, "%s:%s\n", repo, a.Tag)
					}
				}
			}
			
			if !found {
				// Fallback to basic tag listing if artifact filtering isn't supported
				tags, err := client.ListTags(cmd.Context(), repo)
				if err != nil {
					return fmt.Errorf("listing tags: %w", err)
				}
				
				fmt.Fprintf(os.Stdout, "--- Tags in %s ---\n", repo)
				for _, tag := range tags {
					fmt.Fprintf(os.Stdout, "%s\n", tag)
				}
			}

			return nil
		},
	}

	return cmd
}
