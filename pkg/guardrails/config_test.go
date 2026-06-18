package guardrails

import (
	"testing"
	"time"

	"github.com/nag-sh/agentbox/pkg/manifest"
)

func TestFromManifest(t *testing.T) {
	g := manifest.GuardrailsSpec{
		Commands: manifest.CommandGuardrails{
			Allow:           []string{"git *"},
			Deny:            []string{"sudo *"},
			DefaultPolicy:   "deny",
			MaxExecutionTime: manifest.Duration{Duration: 5 * time.Minute},
		},
		Filesystem: manifest.FilesystemGuardrails{
			Writable:      []string{"/workspace"},
			Readable:      []string{"/usr/local/bin"},
			Deny:          []string{"/etc/shadow"},
			DefaultPolicy: "deny",
		},
		Resources: manifest.ResourceGuardrails{
			MaxMemory:    "512Mi",
			MaxCPUs:      1.5,
			MaxProcesses: 100,
			MaxOpenFiles: 1024,
		},
	}

	cfg := FromManifest(g)

	if len(cfg.Commands.Allow) != 1 || cfg.Commands.Allow[0] != "git *" {
		t.Errorf("command allow not mapped: %v", cfg.Commands.Allow)
	}
	if cfg.Commands.DefaultPolicy != "deny" {
		t.Errorf("command default policy not mapped: %q", cfg.Commands.DefaultPolicy)
	}
	if cfg.Commands.MaxExecutionTime != 5*time.Minute {
		t.Errorf("command max execution time not mapped: %v", cfg.Commands.MaxExecutionTime)
	}
	if len(cfg.Filesystem.WritablePaths) != 1 || cfg.Filesystem.WritablePaths[0] != "/workspace" {
		t.Errorf("writable paths not mapped: %v", cfg.Filesystem.WritablePaths)
	}
	if cfg.Resources.Memory != "512Mi" {
		t.Errorf("memory not mapped: %q", cfg.Resources.Memory)
	}
	if cfg.Resources.CPU != "1.5" {
		t.Errorf("cpu not mapped: %q", cfg.Resources.CPU)
	}
	if cfg.Resources.Pids != 100 {
		t.Errorf("pids not mapped: %d", cfg.Resources.Pids)
	}
	if cfg.Resources.Nofile != 1024 {
		t.Errorf("nofile not mapped: %d", cfg.Resources.Nofile)
	}
}

func TestToRuntimeFlags(t *testing.T) {
	g := manifest.GuardrailsSpec{
		Resources: manifest.ResourceGuardrails{
			MaxMemory:    "256Mi",
			MaxCPUs:      0.5,
			MaxProcesses: 50,
			MaxOpenFiles: 512,
		},
	}

	flags := ToRuntimeFlags(g)
	joined := ""
	for _, f := range flags {
		joined += f + " "
	}

	for _, expected := range []string{"--memory", "--cpus", "--pids-limit", "--ulimit"} {
		if !contains(flags, expected) {
			t.Errorf("expected flag %q in %v", expected, flags)
		}
	}
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
