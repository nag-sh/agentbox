package network

import (
	"testing"

	"github.com/nag-sh/agentbox/pkg/manifest"
)

func TestFromManifest(t *testing.T) {
	n := manifest.NetworkSpec{
		Egress: manifest.EgressSpec{
			DefaultPolicy: manifest.DefaultPolicyDeny,
			Allow: []manifest.EgressRule{
				{Host: "*.github.com"},
				{Host: "api.example.com", Ports: []int{443}},
			},
		},
		Ingress: manifest.IngressSpec{
			DefaultPolicy: manifest.DefaultPolicyDeny,
			Allow: []manifest.IngressRule{
				{Port: 8080, Source: "localhost"},
			},
		},
	}

	policy := FromManifest(n)

	if policy.Egress.DefaultPolicy != "deny" {
		t.Errorf("egress default policy not mapped: %q", policy.Egress.DefaultPolicy)
	}
	if len(policy.Egress.Rules) != 2 {
		t.Fatalf("expected 2 egress rules, got %d", len(policy.Egress.Rules))
	}
	if policy.Egress.Rules[1].Ports[0] != 443 {
		t.Errorf("egress port not mapped: %v", policy.Egress.Rules[1].Ports)
	}
	if len(policy.Ingress.Rules) != 1 || policy.Ingress.Rules[0].Port != 8080 {
		t.Errorf("ingress rules not mapped: %v", policy.Ingress.Rules)
	}
}

func TestNetworkPolicy_GenerateIPTables(t *testing.T) {
	policy := NetworkPolicy{
		Egress: EgressPolicy{
			DefaultPolicy: "deny",
			Rules: []EgressRule{
				{Host: "127.0.0.1", Ports: []int{8080}},
			},
		},
		Ingress: IngressPolicy{
			DefaultPolicy: "deny",
			Rules: []IngressRule{
				{Port: 8080, Source: ""},
			},
		},
	}

	rules, err := policy.GenerateIPTables()
	if err != nil {
		t.Fatalf("GenerateIPTables failed: %v", err)
	}
	if len(rules) == 0 {
		t.Error("expected iptables rules, got none")
	}
}
