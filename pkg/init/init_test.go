package init

import (
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

func TestParseRuntimeConfig(t *testing.T) {
	data := []byte(`
harness:
  command: opencode
  args: []
  env:
    FOO: bar
  workdir: /workspace
mcp_servers:
  - name: fetch
    command: /opt/agentbox/mcp/fetch/run.sh
    args: []
    env: {}
    health_check:
      command: ["/opt/agentbox/mcp/fetch/run.sh", "--health"]
      interval: 10s
      timeout: 5s
      retries: 3
    max_restarts: 3
required_env:
  - API_KEY
secrets:
  - path: /run/secrets/key
    env_var: API_KEY
network:
  allowed_hosts:
    - api.example.com
  allowed_ports:
    - 443
  deny_all: true
health_timeout: 45s
shutdown_timeout: 15s
`)

	cfg, err := parseRuntimeConfig(data)
	if err != nil {
		t.Fatalf("parseRuntimeConfig failed: %v", err)
	}

	if cfg.Harness.Command != "opencode" {
		t.Errorf("unexpected harness command: %q", cfg.Harness.Command)
	}
	if len(cfg.MCPServers) != 1 || cfg.MCPServers[0].Name != "fetch" {
		t.Fatalf("unexpected mcp servers: %v", cfg.MCPServers)
	}
	if cfg.MCPServers[0].HealthCheck.Interval.Duration != 10*time.Second {
		t.Errorf("unexpected health interval: %v", cfg.MCPServers[0].HealthCheck.Interval.Duration)
	}
	if cfg.HealthTimeout.Duration != 45*time.Second {
		t.Errorf("unexpected health timeout: %v", cfg.HealthTimeout.Duration)
	}
	if cfg.ShutdownTimeout.Duration != 15*time.Second {
		t.Errorf("unexpected shutdown timeout: %v", cfg.ShutdownTimeout.Duration)
	}
}

func TestParseRuntimeConfig_minimal(t *testing.T) {
	data := []byte(`
harness:
  command: opencode
`)

	cfg, err := parseRuntimeConfig(data)
	if err != nil {
		t.Fatalf("parseRuntimeConfig failed: %v", err)
	}
	if cfg.Harness.Command != "opencode" {
		t.Errorf("unexpected harness command: %q", cfg.Harness.Command)
	}
}

func TestLoadSecrets(t *testing.T) {
	secretFile := createTempSecret(t, "super-secret")
	init_ := &Init{logger: log.Default()}
	init_.config = &RuntimeConfig{
		Secrets: []SecretConfig{
			{Path: secretFile, EnvVar: "MY_SECRET"},
		},
	}

	if err := init_.loadSecrets(); err != nil {
		t.Fatalf("loadSecrets failed: %v", err)
	}

	if v := os.Getenv("MY_SECRET"); v != "super-secret" {
		t.Errorf("expected secret value 'super-secret', got %q", v)
	}
}

func createTempSecret(t *testing.T, value string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "secret-")
	if err != nil {
		t.Fatalf("create temp secret: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(value); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	return f.Name()
}
