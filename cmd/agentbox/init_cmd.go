package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nag-sh/agentbox/pkg/manifest"
)

func initCmd() *cobra.Command {
	var (
		outputFile string
		harness    string
		nonInteractive bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a new agentbox.yaml manifest",
		Long: `Initialize a new agentbox.yaml manifest file with sensible defaults.

In interactive mode (default), prompts for harness selection, model provider,
and basic configuration. In non-interactive mode, generates a minimal manifest
with the specified harness.`,
		Example: `  # Interactive scaffold
  agentbox init

  # Non-interactive with explicit harness
  agentbox init --harness opencode --non-interactive

  # Custom output path
  agentbox init -o my-agent.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Check if output file already exists.
			if _, err := os.Stat(outputFile); err == nil {
				return fmt.Errorf("%s already exists (use a different name or delete it first)", outputFile)
			}

			var m *manifest.Manifest

			if nonInteractive {
				h := manifest.HarnessName(harness)
				if h == "" {
					h = manifest.HarnessOpenCode
				}
				m = scaffoldManifest(h)
			} else {
				var err error
				m, err = interactiveScaffold()
				if err != nil {
					return err
				}
			}

			// Serialize to YAML.
			data, err := manifest.MarshalYAML(m)
			if err != nil {
				return fmt.Errorf("generating manifest: %w", err)
			}

			// Write the file.
			if err := os.WriteFile(outputFile, data, 0644); err != nil {
				return fmt.Errorf("writing manifest: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Created %s\n", outputFile)
			fmt.Fprintf(os.Stderr, "\nNext steps:\n")
			fmt.Fprintf(os.Stderr, "  1. Edit %s to customize your agent\n", outputFile)
			fmt.Fprintf(os.Stderr, "  2. Run 'agentbox validate' to check your manifest\n")
			fmt.Fprintf(os.Stderr, "  3. Run 'agentbox build' to build your agent image\n")

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFile, "output", "o", "agentbox.yaml", "Output manifest file path")
	cmd.Flags().StringVar(&harness, "harness", "opencode", "Agent harness (opencode, goose)")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Skip interactive prompts")

	return cmd
}

func scaffoldManifest(harness manifest.HarnessName) *manifest.Manifest {
	return &manifest.Manifest{
		APIVersion: manifest.APIVersion,
		Kind:       "AgentImage",
		Metadata: manifest.Metadata{
			Name:        "my-agent",
			Version:     "0.1.0",
			Description: "My AI coding agent",
		},
		Spec: manifest.Spec{
			OS: manifest.OSSpec{
				Base:     "alpine:3.21",
				Packages: []string{"git", "curl", "ripgrep"},
				Shell:    "/bin/bash",
			},
			Harness: manifest.HarnessSpec{
				Name:    harness,
				Version: "latest",
			},
			Model: manifest.ModelSpec{
				Provider:  manifest.ModelProviderAnthropic,
				Name:      "claude-sonnet-4-20250514",
				APIKeyEnv: "ANTHROPIC_API_KEY",
			},
			Guardrails: manifest.GuardrailsSpec{
				Commands: manifest.CommandGuardrails{
					Deny: []string{"rm -rf /", "sudo *"},
				},
				Filesystem: manifest.FilesystemGuardrails{
					Writable: []string{"/workspace", "/tmp"},
					Deny:     []string{"/etc/shadow", "/etc/passwd"},
				},
				Resources: manifest.ResourceGuardrails{
					MaxMemory:    "4Gi",
					MaxCPUs:      4,
					MaxProcesses: 256,
				},
			},
			Network: manifest.NetworkSpec{
				Egress: manifest.EgressSpec{
					Allow: []manifest.EgressRule{
						{Host: "api.anthropic.com", Ports: []int{443}},
						{Host: "api.openai.com", Ports: []int{443}},
					},
					DefaultPolicy: manifest.DefaultPolicyDeny,
				},
				Ingress: manifest.IngressSpec{
					DefaultPolicy: manifest.DefaultPolicyDeny,
				},
			},
			Secrets: manifest.SecretsSpec{
				Env: []string{"ANTHROPIC_API_KEY"},
			},
			Runtime: manifest.RuntimeSpec{
				Workdir:     "/workspace",
				Interactive: true,
				Mounts: []manifest.MountSpec{
					{Type: "bind", Source: ".", Target: "/workspace"},
				},
			},
		},
	}
}

func interactiveScaffold() (*manifest.Manifest, error) {
	reader := bufio.NewReader(os.Stdin)

	// Ask for harness.
	fmt.Print("Select agent harness [opencode/goose] (default: opencode): ")
	harnessStr, _ := reader.ReadString('\n')
	harnessStr = strings.TrimSpace(harnessStr)
	if harnessStr == "" {
		harnessStr = "opencode"
	}
	harness := manifest.HarnessName(harnessStr)

	// Ask for name.
	fmt.Print("Agent name (default: my-agent): ")
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)
	if name == "" {
		name = "my-agent"
	}

	// Ask for model provider.
	fmt.Print("Model provider [anthropic/openai/ollama] (default: anthropic): ")
	providerStr, _ := reader.ReadString('\n')
	providerStr = strings.TrimSpace(providerStr)
	if providerStr == "" {
		providerStr = "anthropic"
	}

	m := scaffoldManifest(harness)
	m.Metadata.Name = name
	m.Spec.Model.Provider = manifest.ModelProvider(providerStr)

	// Set model defaults based on provider.
	switch m.Spec.Model.Provider {
	case manifest.ModelProviderAnthropic:
		m.Spec.Model.Name = "claude-sonnet-4-20250514"
		m.Spec.Model.APIKeyEnv = "ANTHROPIC_API_KEY"
		m.Spec.Secrets.Env = []string{"ANTHROPIC_API_KEY"}
	case manifest.ModelProviderOpenAI:
		m.Spec.Model.Name = "gpt-4o"
		m.Spec.Model.APIKeyEnv = "OPENAI_API_KEY"
		m.Spec.Secrets.Env = []string{"OPENAI_API_KEY"}
	case manifest.ModelProviderOllama:
		m.Spec.Model.Name = "llama3.1"
		m.Spec.Model.APIKeyEnv = ""
		m.Spec.Secrets.Env = nil
	}

	return m, nil
}
