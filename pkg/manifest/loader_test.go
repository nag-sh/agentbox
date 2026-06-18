package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFile_interpolatesEnvVars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentbox.yaml")
	content := `
apiVersion: agentbox/v1
kind: AgentImage
metadata:
  name: ${NAME}
  version: 1.0.0
spec:
  os:
    base: alpine:latest
  harness:
    name: opencode
    version: "1.0"
  model:
    provider: anthropic
    name: claude
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	t.Setenv("NAME", "test-agent")
	m, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if m.Metadata.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", m.Metadata.Name)
	}
}

func TestLoadFile_defaultValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentbox.yaml")
	content := `
apiVersion: agentbox/v1
kind: AgentImage
metadata:
  name: agent
  version: 1.0.0
spec:
  os:
    base: alpine:latest
  harness:
    name: opencode
    version: "1.0"
  model:
    provider: anthropic
    name: claude
  mcp:
    servers:
      - name: fs
        command: npx
        transport: ""
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	m, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if m.Spec.OS.Shell != "/bin/sh" {
		t.Errorf("expected default shell /bin/sh, got %q", m.Spec.OS.Shell)
	}
	if m.Spec.Runtime.Workdir != "/workspace" {
		t.Errorf("expected default workdir /workspace, got %q", m.Spec.Runtime.Workdir)
	}
	if m.Spec.Network.Egress.DefaultPolicy != DefaultPolicyDeny {
		t.Errorf("expected default egress deny, got %q", m.Spec.Network.Egress.DefaultPolicy)
	}
	if m.Spec.MCP.Servers[0].Transport != MCPTransportStdio {
		t.Errorf("expected default mcp transport stdio, got %q", m.Spec.MCP.Servers[0].Transport)
	}
}

func TestResolveLocalPaths(t *testing.T) {
	base := "/project"
	m := &Manifest{
		Spec: Spec{
			Skills: []SkillSpec{{Name: "s", Path: "skills/s"}},
			Plugins: []PluginSpec{{Name: "p", Path: "plugins/p"}},
			Runtime: RuntimeSpec{
				Mounts: []MountSpec{{Type: "bind", Source: "./data", Target: "/data"}},
			},
			Secrets: SecretsSpec{
				Files: []SecretFile{{Source: "secrets/key", Target: "/run/key"}},
			},
		},
	}

	ResolveLocalPaths(m, base)

	if m.Spec.Skills[0].Path != "/project/skills/s" {
		t.Errorf("skill path not resolved: %q", m.Spec.Skills[0].Path)
	}
	if m.Spec.Plugins[0].Path != "/project/plugins/p" {
		t.Errorf("plugin path not resolved: %q", m.Spec.Plugins[0].Path)
	}
	if m.Spec.Runtime.Mounts[0].Source != "/project/./data" {
		t.Errorf("mount source not resolved: %q", m.Spec.Runtime.Mounts[0].Source)
	}
	if m.Spec.Secrets.Files[0].Source != "/project/secrets/key" {
		t.Errorf("secret source not resolved: %q", m.Spec.Secrets.Files[0].Source)
	}
}
