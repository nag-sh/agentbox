package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/nag-sh/agentbox/pkg/builder"
	"github.com/nag-sh/agentbox/pkg/manifest"
	"github.com/nag-sh/agentbox/pkg/registry"
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

			// Check if the argument is a YAML manifest
			ext := filepath.Ext(imageRef)
			if ext == ".yaml" || ext == ".yml" {
				manifestFile := imageRef
				LogInfo("Detected manifest file %s. Performing runtime build...", manifestFile)
				
				// Load and validate manifest
				m, err := manifest.LoadFile(manifestFile)
				if err != nil {
					return fmt.Errorf("loading manifest: %w", err)
				}
				
				baseDir := filepath.Dir(manifestFile)
				manifest.ResolveLocalPaths(m, baseDir)
				
				result := manifest.Validate(m)
				if !result.IsValid() {
					return fmt.Errorf("%s", result.Error())
				}
				
				// Auto-generate tag
				tag := fmt.Sprintf("agentbox/%s:%s", m.Metadata.Name, m.Metadata.Version)
				
				// Create registry client
				regClient, err := registry.NewClient(registry.ClientOptions{})
				if err != nil {
					return fmt.Errorf("creating registry client: %w", err)
				}
				
				spinner := NewSpinner(fmt.Sprintf("Building OCI image %s", tag))
				spinner.Start()

				// Create builder
				b, err := builder.New(builder.Options{
					Manifest: m,
					Tag:      tag,
					Registry: regClient,
					LogFn: func(format string, a ...interface{}) {
						spinner.SetMessage(fmt.Sprintf(format, a...))
					},
				})
				if err != nil {
					spinner.Stop(false, fmt.Sprintf("Builder initialization failed: %v", err))
					return fmt.Errorf("creating builder: %w", err)
				}
				
				start := time.Now()
				buildRes, err := b.Build(cmd.Context())
				elapsed := time.Since(start)
				if err != nil {
					spinner.Stop(false, fmt.Sprintf("Runtime build failed: %v", err))
					return fmt.Errorf("runtime build failed: %w", err)
				}
				spinner.Stop(true, fmt.Sprintf("Built image %s (%s)", tag, elapsed.Round(time.Millisecond)))
				
				// Detect runtime
				var rt runtime.Runtime
				if runtimeName != "" {
					rt, err = runtime.ForName(runtimeName)
				} else {
					rt, err = runtime.Detect(cmd.Context())
				}
				if err != nil {
					return fmt.Errorf("container runtime: %w", err)
				}
				
				loadSpinner := NewSpinner(fmt.Sprintf("Importing image into local %s runtime", rt.Name()))
				loadSpinner.Start()
				
				// Create a temporary file for the tarball
				tmpFile, err := os.CreateTemp("", "agentbox-*.tar")
				if err != nil {
					loadSpinner.Stop(false, fmt.Sprintf("Temp file creation failed: %v", err))
					return fmt.Errorf("creating temp file for load: %w", err)
				}
				tmpPath := tmpFile.Name()
				tmpFile.Close()
				defer os.Remove(tmpPath)
				
				// Save the image
				if err := buildRes.SaveLocal(cmd.Context(), tmpPath); err != nil {
					loadSpinner.Stop(false, fmt.Sprintf("Saving image failed: %v", err))
					return fmt.Errorf("saving image for load: %w", err)
				}
				
				// Import into runtime
				if err := rt.Import(cmd.Context(), tmpPath, tag); err != nil {
					loadSpinner.Stop(false, fmt.Sprintf("Runtime import failed: %v", err))
					return fmt.Errorf("importing image into runtime %s: %w", rt.Name(), err)
				}
				loadSpinner.Stop(true, fmt.Sprintf("Image imported into %s: %s", rt.Name(), tag))
				
				// Use the built tag as the image reference to run
				imageRef = tag
			}

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

			LogInfo("Launching container image %s using %s runtime...", imageRef, rt.Name())

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
