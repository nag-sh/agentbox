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

func buildCmd() *cobra.Command {
	var (
		manifestFile string
		tag          string
		push         bool
		local        bool
		load         bool
		platform     string
		noCache      bool
		dryRun       bool
		output       string
		runtimeName  string
	)

	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build an OCI container image from a manifest",
		Long: `Build an immutable OCI container image from an agentbox.yaml manifest.

The build process:
  1. Resolves all source references to pinned digests
  2. Pulls base image, harness, skills, plugins, and MCP servers
  3. Constructs image layers with generated configurations
  4. Applies guardrails and network policy configuration
  5. Outputs a tagged OCI image (push to registry or save locally)`,
		Example: `  # Build from default manifest
  agentbox build

  # Build with explicit tag and push
  agentbox build -f agent.yaml -t ghcr.io/myorg/my-agent:v1.0.0 --push

  # Build and save as local OCI tarball
  agentbox build --local -o my-agent.tar

  # Build and load into local docker/podman daemon
  agentbox build --load

  # Dry run to see what would be built
  agentbox build --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load and validate manifest.
			m, err := manifest.LoadFile(manifestFile)
			if err != nil {
				return fmt.Errorf("loading manifest: %w", err)
			}

			// Resolve local paths relative to the manifest file's directory.
			baseDir := filepath.Dir(manifestFile)
			manifest.ResolveLocalPaths(m, baseDir)

			// Validate the manifest.
			result := manifest.Validate(m)
			if !result.IsValid() {
				return fmt.Errorf("%s", result.Error())
			}

			// Auto-generate tag from metadata if not specified.
			if tag == "" {
				tag = fmt.Sprintf("agentbox/%s:%s", m.Metadata.Name, m.Metadata.Version)
			}

			if dryRun {
				fmt.Fprintf(os.Stderr, "Dry run — no image will be built.\n")
				fmt.Fprintf(os.Stderr, "  Base:    %s\n", m.Spec.OS.Base)
				fmt.Fprintf(os.Stderr, "  Harness: %s@%s\n", m.Spec.Harness.Name, m.Spec.Harness.Version)
				fmt.Fprintf(os.Stderr, "  Skills:  %d\n", len(m.Spec.Skills))
				fmt.Fprintf(os.Stderr, "  MCP:     %d servers\n", len(m.Spec.MCP.Servers))
				fmt.Fprintf(os.Stderr, "  Plugins: %d\n", len(m.Spec.Plugins))
				return nil
			}

			// Create registry client.
			regClient, err := registry.NewClient(registry.ClientOptions{})
			if err != nil {
				return fmt.Errorf("creating registry client: %w", err)
			}

			// Create builder.
			// Run the build.
			spinner := NewSpinner(fmt.Sprintf("Building OCI image %s", tag))
			spinner.Start()

			// Create builder.
			b, err := builder.New(builder.Options{
				Manifest:  m,
				Tag:       tag,
				Platform:  platform,
				NoCache:   noCache,
				Registry:  regClient,
				LogFn: func(format string, a ...interface{}) {
					spinner.SetMessage(fmt.Sprintf(format, a...))
				},
			})
			if err != nil {
				spinner.Stop(false, fmt.Sprintf("Builder initialization failed: %v", err))
				return fmt.Errorf("creating builder: %w", err)
			}

			start := time.Now()
			result2, err := b.Build(cmd.Context())
			elapsed := time.Since(start)
			if err != nil {
				spinner.Stop(false, fmt.Sprintf("Build failed: %v", err))
				return fmt.Errorf("build failed: %w", err)
			}
			spinner.Stop(true, fmt.Sprintf("Built image %s (%s)", tag, elapsed.Round(time.Millisecond)))

			if local {
				outPath := output
				if outPath == "" {
					outPath = fmt.Sprintf("%s-%s.tar", m.Metadata.Name, m.Metadata.Version)
				}
				saveSpinner := NewSpinner(fmt.Sprintf("Saving image to OCI tarball %s", outPath))
				saveSpinner.Start()
				if err := result2.SaveLocal(cmd.Context(), outPath); err != nil {
					saveSpinner.Stop(false, fmt.Sprintf("Save failed: %v", err))
					return fmt.Errorf("saving local image: %w", err)
				}
				saveSpinner.Stop(true, fmt.Sprintf("Image saved to %s", outPath))
			}

			if push {
				pushSpinner := NewSpinner(fmt.Sprintf("Pushing image %s to registry", tag))
				pushSpinner.Start()
				if err := result2.Push(cmd.Context()); err != nil {
					pushSpinner.Stop(false, fmt.Sprintf("Push failed: %v", err))
					return fmt.Errorf("pushing image: %w", err)
				}
				pushSpinner.Stop(true, fmt.Sprintf("Image pushed to %s", tag))
			}

			if load {
				loadSpinner := NewSpinner("Loading image into local runtime daemon")
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
				if err := result2.SaveLocal(cmd.Context(), tmpPath); err != nil {
					loadSpinner.Stop(false, fmt.Sprintf("Saving image failed: %v", err))
					return fmt.Errorf("saving image for load: %w", err)
				}

				// Import into runtime
				importRuntime, err := runtime.Detect(cmd.Context())
				if err != nil {
					if runtimeName != "" {
						importRuntime, err = runtime.ForName(runtimeName)
					}
					if err != nil {
						loadSpinner.Stop(false, fmt.Sprintf("Runtime detection failed: %v", err))
						return fmt.Errorf("detecting runtime for load: %w", err)
					}
				}

				if err := importRuntime.Import(cmd.Context(), tmpPath, tag); err != nil {
					loadSpinner.Stop(false, fmt.Sprintf("Runtime import failed: %v", err))
					return fmt.Errorf("importing image into runtime %s: %w", importRuntime.Name(), err)
				}
				loadSpinner.Stop(true, fmt.Sprintf("Image loaded into %s: %s", importRuntime.Name(), tag))
			}

			if !local && !push && !load {
				LogInfo("Image built in memory: %s", tag)
				LogInfo("Digest: %s", result2.Digest)
				LogInfo("Tip: Use --load to import into your local docker/podman runtime.")
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&manifestFile, "file", "f", "agentbox.yaml", "Path to the manifest file")
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "Image tag (default: agentbox/<name>:<version>)")
	cmd.Flags().BoolVar(&push, "push", false, "Push image to registry after building")
	cmd.Flags().BoolVar(&local, "local", false, "Save as local OCI tarball")
	cmd.Flags().BoolVar(&load, "load", false, "Load the built image into local podman/docker daemon")
	cmd.Flags().StringVar(&platform, "platform", "linux/amd64", "Target platform")
	cmd.Flags().BoolVar(&noCache, "no-cache", false, "Disable layer caching")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be built without building")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output path for local tarball")
	cmd.Flags().StringVar(&runtimeName, "runtime", "", "Container runtime to use for --load (podman, docker)")

	return cmd
}
