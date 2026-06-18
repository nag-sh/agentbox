package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"github.com/nag-sh/agentbox/pkg/guardrails"
	"github.com/nag-sh/agentbox/pkg/harness"
	"github.com/nag-sh/agentbox/pkg/manifest"
	"github.com/nag-sh/agentbox/pkg/network"
	"github.com/nag-sh/agentbox/pkg/ocx"
	"github.com/nag-sh/agentbox/pkg/registry"
	"github.com/nag-sh/agentbox/pkg/runtime"
)

// Options configure the image builder.
type Options struct {
	Manifest *manifest.Manifest
	Tag      string
	Platform string
	NoCache  bool
	Registry *registry.Client
	LogFn    func(string, ...interface{})
}

// Result contains information about a built image.
type Result struct {
	Image  v1.Image
	Digest string
	Tag    string
	Client *registry.Client
}

// Push pushes the built image to its registry.
func (r *Result) Push(ctx context.Context) error {
	return r.Client.PushImage(ctx, r.Tag, r.Image)
}

// SaveLocal saves the built image as an OCI tarball on the local filesystem.
func (r *Result) SaveLocal(ctx context.Context, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating output file: %w", err)
	}
	defer f.Close()

	ref, err := name.ParseReference(r.Tag)
	if err != nil {
		return fmt.Errorf("parsing tag: %w", err)
	}

	if err := tarball.Write(ref, r.Image, f); err != nil {
		return fmt.Errorf("writing tarball: %w", err)
	}

	return nil
}

// Builder orchestrates the creation of an agentbox OCI image.
type Builder struct {
	opts Options
}

// New creates a new Builder with the given options.
func New(opts Options) (*Builder, error) {
	if opts.Manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}
	if opts.Registry == nil {
		return nil, fmt.Errorf("registry client is required")
	}

	return &Builder{
		opts: opts,
	}, nil
}

// Build executes the image build pipeline.
func (b *Builder) Build(ctx context.Context) (*Result, error) {
	// 1. Pull base image
	if b.opts.LogFn != nil {
		b.opts.LogFn("Pulling base image: %s", b.opts.Manifest.Spec.OS.Base)
	}
	baseImg, err := b.opts.Registry.PullImage(ctx, b.opts.Manifest.Spec.OS.Base)
	if err != nil {
		return nil, fmt.Errorf("pulling base image %q: %w", b.opts.Manifest.Spec.OS.Base, err)
	}

	// 2. Resolve Harness and fetch it
	harnessRef := b.opts.Manifest.Spec.Harness.Source
	if harnessRef != "" {
		arch := "amd64"
		if b.opts.Platform != "" {
			parts := strings.Split(b.opts.Platform, "/")
			arch = parts[len(parts)-1]
		}
		harnessRef = fmt.Sprintf("%s-%s", harnessRef, arch)
		
		if b.opts.LogFn != nil {
			b.opts.LogFn("Pulling harness image: %s", harnessRef)
		}
		harnessImg, err := b.opts.Registry.PullImage(ctx, harnessRef)
		if err != nil {
			return nil, fmt.Errorf("pulling harness image %q: %w", harnessRef, err)
		}
		
		if b.opts.LogFn != nil {
			b.opts.LogFn("Resolving harness layers...")
		}
		harnessLayers, err := harnessImg.Layers()
		if err != nil {
			return nil, fmt.Errorf("reading harness layers: %w", err)
		}
		cleanLayers, err := CleanLayers(harnessLayers)
		if err != nil {
			return nil, fmt.Errorf("cleaning harness layers: %w", err)
		}
		baseImg, err = AppendLayers(baseImg, cleanLayers...)
		if err != nil {
			return nil, fmt.Errorf("appending harness layers: %w", err)
		}
	}

	resolved, err := b.resolveOCXComponents(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving OCX components: %w", err)
	}
	if resolved != nil {
		defer resolved.Cleanup()
	}

	store := registry.NewArtifactStore(b.opts.Registry)

	if b.opts.LogFn != nil {
		b.opts.LogFn("Resolving skills, plugins, and MCP servers...")
	}
	artifactLayers, err := b.processArtifacts(ctx, store)
	if err != nil {
		return nil, fmt.Errorf("processing artifacts: %w", err)
	}
	if len(artifactLayers) > 0 {
		baseImg, err = AppendLayers(baseImg, artifactLayers...)
		if err != nil {
			return nil, fmt.Errorf("appending artifact layers: %w", err)
		}
	}

	ocxLayers, err := b.processOCXLayers(resolved)
	if err != nil {
		return nil, fmt.Errorf("processing OCX layers: %w", err)
	}
	if len(ocxLayers) > 0 {
		baseImg, err = AppendLayers(baseImg, ocxLayers...)
		if err != nil {
			return nil, fmt.Errorf("appending OCX layers: %w", err)
		}
	}

	// 4. Construct Agentbox specific layers
	if b.opts.LogFn != nil {
		b.opts.LogFn("Generating configuration files (runtime.yaml, guardrails.yaml)...")
	}
	files := make(map[string][]byte)

	// Note: The base OS image and the harnesses are pulled from the registry.
	// We dynamically generate configuration files (runtime.yaml, guardrails.yaml)
	// from the unified manifest and inject them as a configuration layer.
	configGen := NewConfigGenerator()
	configGen.RegisterAdapter(string(manifest.HarnessOpenCode), &harness.OpenCodeAdapter{})
	configGen.RegisterAdapter(string(manifest.HarnessGoose), &harness.GooseAdapter{})
	
	generatedFiles, err := configGen.Generate(b.opts.Manifest, resolved)
	if err != nil {
		return nil, fmt.Errorf("generating configuration: %w", err)
	}
	
	for k, v := range generatedFiles {
		files[k] = v
	}
	
	if b.opts.LogFn != nil {
		b.opts.LogFn("Creating OCI configuration layer...")
	}
	configLayer, err := CreateLayerFromFiles(files)
	if err != nil {
		return nil, fmt.Errorf("creating config layer: %w", err)
	}

	// 5. Apply layers to base image
	if b.opts.LogFn != nil {
		b.opts.LogFn("Assembling OCI image layers...")
	}
	img, err := AppendLayers(baseImg, configLayer)
	if err != nil {
		return nil, fmt.Errorf("appending config layer: %w", err)
	}

	// 6. Mutate image config (env, entrypoint, etc)
	if b.opts.LogFn != nil {
		b.opts.LogFn("Applying metadata configurations (env, entrypoint)...")
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("reading image config: %w", err)
	}
	
	// Set working directory
	cfg.Config.WorkingDir = b.opts.Manifest.Spec.Runtime.Workdir
	
	// Set entrypoint to our init binary
	cfg.Config.Entrypoint = []string{"/opt/agentbox/bin/agentbox-init"}
	
	// Apply environment variables
	if cfg.Config.Env == nil {
		cfg.Config.Env = []string{}
	}
	for k, v := range b.opts.Manifest.Spec.Runtime.Env {
		cfg.Config.Env = append(cfg.Config.Env, fmt.Sprintf("%s=%s", k, v))
	}
	
	// Set user if specified
	if b.opts.Manifest.Spec.Runtime.User != "" {
		cfg.Config.User = b.opts.Manifest.Spec.Runtime.User
	}
	
	// Apply Labels
	if cfg.Config.Labels == nil {
		cfg.Config.Labels = make(map[string]string)
	}
	for k, v := range b.opts.Manifest.Metadata.Labels {
		cfg.Config.Labels[k] = v
	}
	if policyJSON, err := json.Marshal(runtimePolicyFromManifest(b.opts.Manifest)); err == nil {
		cfg.Config.Labels[runtime.PolicyLabel] = string(policyJSON)
	}

	img, err = mutate.Config(img, cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("mutating image config: %w", err)
	}
	
	// Get final digest
	if b.opts.LogFn != nil {
		b.opts.LogFn("Calculating final image digest...")
	}
	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("calculating image digest: %w", err)
	}

	return &Result{
		Image:  img,
		Digest: digest.String(),
		Tag:    b.opts.Tag,
		Client: b.opts.Registry,
	}, nil
}

// resolveOCXComponents fetches and resolves any OCX components declared in the
// manifest. It returns nil when no components are declared.
func (b *Builder) resolveOCXComponents(ctx context.Context) (*ocx.ResolvedSet, error) {
	refs := b.opts.Manifest.Spec.OCX.Components
	if len(refs) == 0 {
		return nil, nil
	}

	sources := make([]string, 0, len(refs))
	for _, ref := range refs {
		sources = append(sources, b.resolveOCXSource(ref))
	}

	fetcher := ocx.NewOCIFetcher(b.opts.Registry)
	resolver := ocx.NewResolver(fetcher)

	if b.opts.LogFn != nil {
		b.opts.LogFn("Resolving OCX components: %v", sources)
	}
	resolved, err := resolver.Resolve(ctx, sources)
	if err != nil {
		return nil, err
	}

	return resolved, nil
}

func (b *Builder) resolveOCXSource(ref manifest.OCXComponentRef) string {
	alias, rest, found := strings.Cut(ref.Source, "/")
	if found {
		if reg, ok := b.opts.Manifest.Spec.OCX.Registries[alias]; ok {
			base := strings.TrimSuffix(reg.URL, "/") + "/" + rest
			if strings.Contains(base, ":") {
				return base
			}
			version := ref.Version
			if version == "" {
				version = "latest"
			}
			return fmt.Sprintf("%s:%s", base, version)
		}
	}

	if ref.Version != "" && !strings.Contains(ref.Source, ":") {
		return fmt.Sprintf("%s:%s", ref.Source, ref.Version)
	}
	return ref.Source
}

func (b *Builder) processOCXLayers(resolved *ocx.ResolvedSet) ([]v1.Layer, error) {
	if resolved == nil {
		return nil, nil
	}

	var layers []v1.Layer
	for _, c := range resolved.Components {
		root := ocxComponentRoot(c.Manifest.Type)
		if root == "" {
			continue
		}
		target := fmt.Sprintf("%s/%s", root, c.Manifest.Name)
		if b.opts.LogFn != nil {
			b.opts.LogFn("Adding OCX %s: %s", c.Manifest.Type, c.Manifest.Name)
		}
		layer, err := CreateLayerFromDir(c.StagingDir, target)
		if err != nil {
			return nil, fmt.Errorf("layer for OCX component %q: %w", c.Manifest.Name, err)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

func ocxComponentRoot(t ocx.ComponentType) string {
	switch t {
	case ocx.ComponentSkill:
		return "/opt/agentbox/skills"
	case ocx.ComponentPlugin:
		return "/opt/agentbox/plugins"
	case ocx.ComponentAgent:
		return "/opt/agentbox/agents"
	case ocx.ComponentCommand:
		return "/opt/agentbox/commands"
	case ocx.ComponentTool:
		return "/opt/agentbox/tools"
	case ocx.ComponentBundle, ocx.ComponentProfile:
		return ""
	}
	return ""
}

// processArtifacts resolves all skills, plugins, and MCP servers declared in the
// manifest and returns their layers.
func (b *Builder) processArtifacts(ctx context.Context, store *registry.ArtifactStore) ([]v1.Layer, error) {
	var layers []v1.Layer

	for _, skill := range b.opts.Manifest.Spec.Skills {
		if b.opts.LogFn != nil {
			b.opts.LogFn("Adding skill: %s", skill.Name)
		}
		layer, err := artifactLayer(ctx, store, skill, "/opt/agentbox/skills")
		if err != nil {
			return nil, fmt.Errorf("skill %q: %w", skill.Name, err)
		}
		layers = append(layers, layer)
	}

	for _, plugin := range b.opts.Manifest.Spec.Plugins {
		if b.opts.LogFn != nil {
			b.opts.LogFn("Adding plugin: %s", plugin.Name)
		}
		layer, err := pluginLayer(ctx, store, plugin, "/opt/agentbox/plugins")
		if err != nil {
			return nil, fmt.Errorf("plugin %q: %w", plugin.Name, err)
		}
		layers = append(layers, layer)
	}

	for _, srv := range b.opts.Manifest.Spec.MCP.Servers {
		if b.opts.LogFn != nil {
			b.opts.LogFn("Adding MCP server: %s", srv.Name)
		}
		layer, err := mcpLayer(ctx, store, srv, "/opt/agentbox/mcp")
		if err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", srv.Name, err)
		}
		layers = append(layers, layer)
	}

	return layers, nil
}

func runtimePolicyFromManifest(m *manifest.Manifest) runtime.Policy {
	netPolicy := network.FromManifest(m.Spec.Network)
	gr := guardrails.FromManifest(m.Spec.Guardrails)

	memBytes, _ := guardrails.ParseSize(gr.Resources.Memory)
	return runtime.Policy{
		NetworkFlags: netPolicy.RuntimeFlags(),
		CPUs:         gr.Resources.CPU,
		MemoryBytes:  memBytes,
		PidsLimit:    int64(gr.Resources.Pids),
	}
}
