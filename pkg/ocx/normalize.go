package ocx

import (
	"fmt"
	"path/filepath"

	"github.com/nag-sh/agentbox/pkg/guardrails"
	"github.com/nag-sh/agentbox/pkg/harness"
	"github.com/nag-sh/agentbox/pkg/manifest"
	"github.com/nag-sh/agentbox/pkg/network"
)

// Normalizer converts an agentbox manifest plus a resolved set of OCX
// components into a harness-agnostic HarnessConfig.
type Normalizer struct{}

// NewNormalizer creates a new Normalizer.
func NewNormalizer() *Normalizer {
	return &Normalizer{}
}

// Normalize builds a HarnessConfig from the manifest and resolved OCX components.
func (n *Normalizer) Normalize(m *manifest.Manifest, resolved *ResolvedSet) (*harness.HarnessConfig, error) {
	if m == nil {
		return nil, fmt.Errorf("manifest must not be nil")
	}

	h := &harness.HarnessConfig{
		APIVersion: m.APIVersion,
		Kind:       m.Kind,
		Metadata: harness.MetadataConfig{
			Name:        m.Metadata.Name,
			Version:     m.Metadata.Version,
			Description: m.Metadata.Description,
			Labels:      m.Metadata.Labels,
			Annotations: m.Metadata.Annotations,
		},
		OS: harness.OSConfig{
			Base:     m.Spec.OS.Base,
			Packages: m.Spec.OS.Packages,
			Shell:    m.Spec.OS.Shell,
		},
		Harness: harness.HarnessInfo{
			Name:    string(m.Spec.Harness.Name),
			Version: m.Spec.Harness.Version,
			Source:  m.Spec.Harness.Source,
		},
		Model: harness.ModelConfig{
			Provider:   string(m.Spec.Model.Provider),
			Name:       m.Spec.Model.Name,
			APIKeyEnv:  m.Spec.Model.APIKeyEnv,
			BaseURL:    m.Spec.Model.BaseURL,
			Parameters: m.Spec.Model.Parameters,
		},
		GPU: harness.GPUConfig{
			Enabled:      m.Spec.GPU.Enabled,
			Devices:      m.Spec.GPU.Devices,
			Runtime:      m.Spec.GPU.Runtime,
			Capabilities: m.Spec.GPU.Capabilities,
		},
		Runtime: harness.RuntimeConfig{
			Workdir:     m.Spec.Runtime.Workdir,
			Env:         m.Spec.Runtime.Env,
			Interactive: m.Spec.Runtime.Interactive,
			User:        m.Spec.Runtime.User,
		},
		Guardrails: guardrails.FromManifest(m.Spec.Guardrails),
		Network:    network.FromManifest(m.Spec.Network),
	}

	for _, pkg := range m.Spec.OS.Packages {
		if pkg != "" {
			h.OS.Packages = appendIfMissing(h.OS.Packages, pkg)
		}
	}

	for _, s := range m.Spec.Skills {
		h.Skills = append(h.Skills, harness.SkillConfig{
			Name:   s.Name,
			Source: s.Source,
			Path:   s.Path,
			Config: s.Config,
		})
	}

	for _, p := range m.Spec.Plugins {
		h.Plugins = append(h.Plugins, harness.PluginConfig{
			Name:   p.Name,
			Source: p.Source,
			Path:   p.Path,
			Config: p.Config,
		})
	}

	for _, srv := range m.Spec.MCP.Servers {
		cfg := harness.MCPConfig{
			Name:      srv.Name,
			Source:    srv.Source,
			Command:   srv.Command,
			Args:      srv.Args,
			Transport: string(srv.Transport),
			Env:       srv.Env,
			Config:    srv.Config,
		}
		if srv.HealthCheck != nil {
			cfg.HealthCheck = &harness.HealthCheckConfig{
				Command:     srv.HealthCheck.Command,
				Interval:    srv.HealthCheck.Interval.Duration,
				Timeout:     srv.HealthCheck.Timeout.Duration,
				Retries:     srv.HealthCheck.Retries,
				StartPeriod: srv.HealthCheck.StartPeriod.Duration,
			}
		}
		h.MCPs = append(h.MCPs, cfg)
	}

	for _, p := range m.Spec.Tools.Permissions {
		h.Tools.Permissions = append(h.Tools.Permissions, harness.ToolPermissionConfig{
			Tool:  p.Tool,
			Allow: p.Allow,
			Scope: p.Scope,
		})
	}

	for _, t := range m.Spec.Tools.Custom {
		h.Tools.Custom = append(h.Tools.Custom, harness.CustomToolConfig{
			Name:        t.Name,
			Command:     t.Command,
			Args:        t.Args,
			Description: t.Description,
			WorkDir:     t.WorkDir,
		})
	}

	for _, f := range m.Spec.Secrets.Files {
		h.Secrets.Files = append(h.Secrets.Files, harness.SecretFileConfig{
			Source: f.Source,
			Target: f.Target,
			Env:    f.Env,
			Mode:   f.Mode,
		})
	}
	for _, e := range m.Spec.Secrets.Env {
		h.Secrets.Env = appendIfMissing(h.Secrets.Env, e)
	}

	for _, mnt := range m.Spec.Runtime.Mounts {
		h.Runtime.Mounts = append(h.Runtime.Mounts, harness.MountConfig{
			Type:     mnt.Type,
			Source:   mnt.Source,
			Target:   mnt.Target,
			ReadOnly: mnt.ReadOnly,
			Options:  mnt.Options,
		})
	}

	for _, p := range m.Spec.Runtime.Ports {
		h.Runtime.Ports = append(h.Runtime.Ports, harness.PortConfig{
			Host:      p.Host,
			Container: p.Container,
			Protocol:  p.Protocol,
		})
	}

	if len(m.Spec.Opencode) > 0 {
		h.Opencode = mergeOpencode(h.Opencode, OpencodeBlock(m.Spec.Opencode))
	}

	if resolved != nil {
		if err := n.applyResolved(h, resolved); err != nil {
			return nil, err
		}
	}

	return h, nil
}

func (n *Normalizer) applyResolved(h *harness.HarnessConfig, resolved *ResolvedSet) error {
	for _, c := range resolved.Components {
		switch c.Manifest.Type {
		case ComponentSkill:
			h.Skills = append(h.Skills, harness.SkillConfig{
				Name:   c.Manifest.Name,
				Source: c.Source,
				Path:   c.StagingDir,
				Files:  fileTargets(c.Manifest.Files),
			})
		case ComponentPlugin:
			h.Plugins = append(h.Plugins, harness.PluginConfig{
				Name:   c.Manifest.Name,
				Source: c.Source,
				Path:   c.StagingDir,
				Files:  fileTargets(c.Manifest.Files),
			})
		case ComponentAgent:
			h.Agents = append(h.Agents, harness.AgentConfig{
				Name:   c.Manifest.Name,
				Config: map[string]interface{}(c.Manifest.Opencode),
			})
		case ComponentCommand:
			h.Commands = append(h.Commands, harness.CommandConfig{
				Name:   c.Manifest.Name,
				Config: map[string]interface{}(c.Manifest.Opencode),
			})
		case ComponentBundle:
			h.Bundles = append(h.Bundles, harness.BundleConfig{
				Name:       c.Manifest.Name,
				Components: c.Manifest.Dependencies,
			})
		}
	}

	if resolved.Opencode != nil {
		h.Opencode = mergeOpencode(h.Opencode, resolved.Opencode)
	}

	return nil
}

func fileTargets(files []FileSpec) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if f.Target != "" {
			out = append(out, filepath.Clean(f.Target))
		} else if f.Path != "" {
			out = append(out, filepath.Clean(f.Path))
		}
	}
	return out
}

func appendIfMissing(slice []string, s string) []string {
	for _, existing := range slice {
		if existing == s {
			return slice
		}
	}
	return append(slice, s)
}
