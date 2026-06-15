package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/nag-sh/agentbox/pkg/manifest"
)

func validateCmd() *cobra.Command {
	var manifestFile string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate an agentbox.yaml manifest",
		Long:  `Check a manifest file for schema correctness and logical errors without building.`,
		Example: `  agentbox validate -f agentbox.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := manifest.LoadFile(manifestFile)
			if err != nil {
				return fmt.Errorf("loading manifest: %w", err)
			}

			result := manifest.Validate(m)
			if !result.IsValid() {
				return fmt.Errorf("%s", result.Error())
			}

			fmt.Fprintf(os.Stderr, "Manifest %s is valid.\n", manifestFile)
			return nil
		},
	}

	cmd.Flags().StringVarP(&manifestFile, "file", "f", "agentbox.yaml", "Path to the manifest file")
	return cmd
}
