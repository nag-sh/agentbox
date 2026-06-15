package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/nag-sh/agentbox/pkg/runtime"
)

func runCmd() *cobra.Command {
	var (
		workspace  string
		envVars    []string
		envFiles   []string
		secretEnvs []string
		runtimeName string
		detach     bool
		name       string
	)

	cmd := &cobra.Command{
		Use:   "run [image]",
		Short: "Run an agent image interactively",
		Long: `Run a built agent container image as an interactive coding assistant.

By default, the current directory is mounted as the workspace and a TTY
is allocated for interactive use. Environment variables for API keys can
be passed through from the host.`,
		Example: `  # Run with current directory as workspace
  agentbox run ghcr.io/myorg/my-agent:v1

  # Run with explicit workspace and API key
  agentbox run ghcr.io/myorg/my-agent:v1 --workspace ~/projects/myapp --secret-env ANTHROPIC_API_KEY

  # Run with a specific container runtime
  agentbox run ghcr.io/myorg/my-agent:v1 --runtime podman`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			imageRef := args[0]

			// Resolve workspace to absolute path.
			absWorkspace, err := filepath.Abs(workspace)
			if err != nil {
				return fmt.Errorf("resolving workspace path: %w", err)
			}

			// Verify workspace exists.
			info, err := os.Stat(absWorkspace)
			if err != nil {
				return fmt.Errorf("workspace %s: %w", absWorkspace, err)
			}
			if !info.IsDir() {
				return fmt.Errorf("workspace %s is not a directory", absWorkspace)
			}

			// Detect or select runtime.
			var rt runtime.Runtime
			if runtimeName != "" {
				rt, err = runtime.ForName(runtimeName)
			} else {
				rt, err = runtime.Detect(cmd.Context())
			}
			if err != nil {
				return fmt.Errorf("container runtime: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Using runtime: %s\n", rt.Name())

			// Build environment map.
			env := make(map[string]string)
			for _, e := range envVars {
				k, v, _ := parseEnvVar(e)
				env[k] = v
			}

			// Build run options.
			opts := runtime.RunOptions{
				Image:       imageRef,
				Name:        name,
				Interactive: true,
				TTY:         true,
				Remove:      true,
				Env:         env,
				EnvPassthrough: secretEnvs,
				Mounts: []runtime.Mount{
					{
						Type:   "bind",
						Source: absWorkspace,
						Target: "/workspace",
					},
				},
				Workdir: "/workspace",
				ExtraArgs: make([]string, 0),
			}

			// Add env files as extra args to the runtime
			for _, ef := range envFiles {
				// Resolve path so it works regardless of working directory
				absEf, err := filepath.Abs(ef)
				if err == nil {
					opts.ExtraArgs = append(opts.ExtraArgs, "--env-file", absEf)
				} else {
					opts.ExtraArgs = append(opts.ExtraArgs, "--env-file", ef)
				}
			}

			// Run the container.
			return rt.Run(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVarP(&workspace, "workspace", "w", ".", "Host directory to mount as /workspace")
	cmd.Flags().StringArrayVarP(&envVars, "env", "e", nil, "Environment variables (KEY=VALUE)")
	cmd.Flags().StringArrayVar(&envFiles, "env-file", nil, "Read in a file of environment variables")
	cmd.Flags().StringArrayVar(&secretEnvs, "secret-env", nil, "Environment variable names to pass through from host")
	cmd.Flags().StringVar(&runtimeName, "runtime", "", "Container runtime (podman, docker; auto-detected if not set)")
	cmd.Flags().BoolVarP(&detach, "detach", "d", false, "Run container in background")
	cmd.Flags().StringVar(&name, "name", "", "Container name")

	return cmd
}

// parseEnvVar splits a KEY=VALUE string. If no = is present, the value
// is looked up from the host environment.
func parseEnvVar(s string) (string, string, bool) {
	for i, c := range s {
		if c == '=' {
			return s[:i], s[i+1:], true
		}
	}
	// No =, look up from environment.
	val, ok := os.LookupEnv(s)
	return s, val, ok
}
