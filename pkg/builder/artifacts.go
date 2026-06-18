package builder

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"gopkg.in/yaml.v3"

	"github.com/nag-sh/agentbox/pkg/manifest"
	"github.com/nag-sh/agentbox/pkg/registry"
)

// artifactLayer creates a layer for a skill, plugin, or MCP server.
// If source is set the artifact is pulled from an OCI registry; otherwise the
// local path is packed into the layer.
func artifactLayer(ctx context.Context, store *registry.ArtifactStore, item manifest.SkillSpec, targetRoot string) (v1.Layer, error) {
	targetPath := filepath.Join(targetRoot, item.Name)

	if item.Source != "" {
		return layerFromArtifactSource(ctx, store, item.Source, targetPath)
	}

	return CreateLayerFromDir(item.Path, targetPath)
}

// pluginLayer creates a layer for a plugin.
func pluginLayer(ctx context.Context, store *registry.ArtifactStore, item manifest.PluginSpec, targetRoot string) (v1.Layer, error) {
	targetPath := filepath.Join(targetRoot, item.Name)

	if item.Source != "" {
		return layerFromArtifactSource(ctx, store, item.Source, targetPath)
	}

	return CreateLayerFromDir(item.Path, targetPath)
}

// mcpLayer creates a layer for an MCP server. When the manifest provides a
// command, a run.sh wrapper is generated so both the init system and harness
// configs can reference a single executable path.
func mcpLayer(ctx context.Context, store *registry.ArtifactStore, srv manifest.MCPServerSpec, targetRoot string) (v1.Layer, error) {
	targetPath := filepath.Join(targetRoot, srv.Name)
	staging, err := os.MkdirTemp("", "agentbox-mcp-")
	if err != nil {
		return nil, fmt.Errorf("creating mcp staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	if srv.Source != "" {
		if _, err := store.PullArtifact(ctx, srv.Source, staging); err != nil {
			return nil, fmt.Errorf("pulling mcp artifact %q: %w", srv.Source, err)
		}
	}

	command := srv.Command
	args := srv.Args
	if command == "" {
		cmd, cmdArgs, err := commandFromMCPMeta(staging)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q has no command and no mcp.yaml: %w", srv.Name, err)
		}
		command = cmd
		args = cmdArgs
	}

	if command == "" {
		return nil, fmt.Errorf("mcp server %q has no executable command", srv.Name)
	}

	runScript := generateRunScript(command, args)
	if err := os.WriteFile(filepath.Join(staging, "run.sh"), runScript, 0755); err != nil {
		return nil, fmt.Errorf("writing run.sh for mcp %q: %w", srv.Name, err)
	}

	return CreateLayerFromDir(staging, targetPath)
}

// layerFromArtifactSource pulls an OCI artifact into a temporary directory and
// packs it as an image layer at the given target path.
func layerFromArtifactSource(ctx context.Context, store *registry.ArtifactStore, source, targetPath string) (v1.Layer, error) {
	staging, err := os.MkdirTemp("", "agentbox-artifact-")
	if err != nil {
		return nil, fmt.Errorf("creating artifact staging dir: %w", err)
	}
	defer os.RemoveAll(staging)

	if _, err := store.PullArtifact(ctx, source, staging); err != nil {
		return nil, fmt.Errorf("pulling artifact %q: %w", source, err)
	}

	return CreateLayerFromDir(staging, targetPath)
}

// mcpMeta is the subset of an mcp.yaml file needed to construct a run script.
type mcpMeta struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`
}

// commandFromMCPMeta reads an mcp.yaml in the staging directory and returns the
// configured command and arguments.
func commandFromMCPMeta(dir string) (string, []string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "mcp.yaml"))
	if err != nil {
		return "", nil, err
	}

	var meta mcpMeta
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return "", nil, err
	}

	return meta.Command, meta.Args, nil
}

// generateRunScript creates a POSIX shell wrapper that execs the given command.
func generateRunScript(command string, args []string) []byte {
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("exec ")
	b.WriteString(command)
	for _, a := range args {
		b.WriteString(" ")
		b.WriteString(shellQuote(a))
	}
	b.WriteString("\n")
	return []byte(b.String())
}

// shellQuote escapes a single argument for POSIX shell usage.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, "'\" \\n$|&;<>()`)") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
