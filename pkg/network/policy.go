// Package network provides a network policy engine for defining and enforcing
// network access rules within agentbox containers. It supports egress filtering
// (controlling which external hosts the container may reach), ingress filtering
// (controlling which ports are exposed and from which sources), and generates
// both iptables rules and container runtime flags for enforcement.
package network

import (
	"fmt"
	"strings"
)

// NetworkPolicy holds the complete network policy configuration for a
// container. It defines both egress (outbound) and ingress (inbound) rules.
type NetworkPolicy struct {
	// Egress defines the outbound network policy.
	Egress EgressPolicy `yaml:"egress"`
	// Ingress defines the inbound network policy.
	Ingress IngressPolicy `yaml:"ingress"`
}

// EgressPolicy controls outbound network traffic from the container.
type EgressPolicy struct {
	// DefaultPolicy is the default action when no rules match.
	// Valid values are "allow" and "deny". Defaults to "deny".
	DefaultPolicy string `yaml:"default_policy"`

	// Rules is the list of egress rules. Each rule specifies a host (or
	// wildcard pattern) and optional ports that are allowed.
	Rules []EgressRule `yaml:"rules"`
}

// EgressRule defines a single egress (outbound) rule. Traffic matching this
// rule is permitted.
type EgressRule struct {
	// Host is the destination hostname or glob pattern.
	// Examples: "api.github.com", "*.github.com", "pypi.org"
	Host string `yaml:"host"`

	// Ports is an optional list of allowed destination ports. If empty,
	// all ports are permitted for the matched host.
	Ports []int `yaml:"ports"`
}

// IngressPolicy controls inbound network traffic to the container.
type IngressPolicy struct {
	// DefaultPolicy is the default action when no rules match.
	// Valid values are "allow" and "deny". Defaults to "deny".
	DefaultPolicy string `yaml:"default_policy"`

	// Rules is the list of ingress rules. Each rule specifies a port and
	// optional source filter.
	Rules []IngressRule `yaml:"rules"`
}

// IngressRule defines a single ingress (inbound) rule. Traffic matching this
// rule is permitted.
type IngressRule struct {
	// Port is the container port to allow inbound traffic on.
	Port int `yaml:"port"`

	// Source is an optional source address filter. It may be:
	//   - "localhost" or "127.0.0.1" for loopback-only access
	//   - A CIDR range (e.g., "10.0.0.0/8")
	//   - Empty string for any source
	Source string `yaml:"source"`
}

// Validate checks the network policy for logical errors and invalid
// configuration. It returns an error describing all problems found.
func (p *NetworkPolicy) Validate() error {
	var errs []string

	// Validate egress policy.
	if err := validateDefaultPolicy(p.Egress.DefaultPolicy, "egress"); err != nil {
		errs = append(errs, err.Error())
	}
	for i, rule := range p.Egress.Rules {
		if rule.Host == "" {
			errs = append(errs, fmt.Sprintf("egress rule %d: host is required", i))
		}
		for j, port := range rule.Ports {
			if port < 1 || port > 65535 {
				errs = append(errs, fmt.Sprintf("egress rule %d: port[%d] %d is out of range (1-65535)", i, j, port))
			}
		}
	}

	// Validate ingress policy.
	if err := validateDefaultPolicy(p.Ingress.DefaultPolicy, "ingress"); err != nil {
		errs = append(errs, err.Error())
	}
	for i, rule := range p.Ingress.Rules {
		if rule.Port < 1 || rule.Port > 65535 {
			errs = append(errs, fmt.Sprintf("ingress rule %d: port %d is out of range (1-65535)", i, rule.Port))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("network policy validation failed:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// GenerateIPTables generates a complete set of iptables commands that enforce
// this network policy. The commands should be executed in order within the
// container's network namespace. Both egress and ingress rules are included.
func (p *NetworkPolicy) GenerateIPTables() ([]string, error) {
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("cannot generate iptables for invalid policy: %w", err)
	}

	var rules []string

	// Generate egress (OUTPUT chain) rules.
	egressRules, err := generateEgressIPTables(&p.Egress)
	if err != nil {
		return nil, fmt.Errorf("generating egress iptables: %w", err)
	}
	rules = append(rules, egressRules...)

	// Generate ingress (INPUT chain) rules.
	ingressRules, err := generateIngressIPTables(&p.Ingress)
	if err != nil {
		return nil, fmt.Errorf("generating ingress iptables: %w", err)
	}
	rules = append(rules, ingressRules...)

	return rules, nil
}

// RuntimeFlags returns container runtime flags (compatible with both docker
// and podman) that implement the network policy. This includes port publish
// flags for ingress rules and network mode settings.
func (p *NetworkPolicy) RuntimeFlags() []string {
	var flags []string

	if p.Egress.DefaultPolicy == "deny" && len(p.Egress.Rules) == 0 && len(p.Ingress.Rules) == 0 {
		flags = append(flags, "--network=none")
	}

	// Generate port publish flags for ingress rules.
	for _, rule := range p.Ingress.Rules {
		flags = append(flags, ingressRuleToFlag(rule))
	}

	return flags
}

// validateDefaultPolicy validates that a default policy string is either
// "allow", "deny", or empty (which is treated as "deny").
func validateDefaultPolicy(policy, name string) error {
	switch policy {
	case "", "allow", "deny":
		return nil
	default:
		return fmt.Errorf("%s: invalid default_policy %q (must be \"allow\" or \"deny\")", name, policy)
	}
}
