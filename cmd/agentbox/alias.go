package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func aliasCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alias [name] [image]",
		Short: "Create a shell alias for running an agent",
		Long: `Generates a shell alias command for running the specified agent image.
You can append the output to your ~/.bashrc or ~/.zshrc.`,
		Example: `  # Generate alias
  agentbox alias myagent ghcr.io/myorg/my-agent:v1
  
  # Append to zshrc
  agentbox alias myagent ghcr.io/myorg/my-agent:v1 >> ~/.zshrc`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			image := args[1]

			aliasCmd := fmt.Sprintf("alias %s='agentbox run %s --workspace .'", name, image)
			fmt.Println(aliasCmd)
			
			return nil
		},
	}

	return cmd
}
