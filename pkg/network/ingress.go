package network

import (
	"fmt"
	"net"
	"strings"

	"github.com/charmbracelet/log"
)

// IngressChecker evaluates inbound connection attempts against the configured
// ingress rules. It supports port-based rules and source address filtering
// including localhost and CIDR ranges.
//
// IngressChecker is safe for concurrent use once constructed.
type IngressChecker struct {
	policy IngressPolicy
	logger *log.Logger
}

// NewIngressChecker constructs a new [IngressChecker] from the given
// [IngressPolicy]. It normalizes the default policy.
func NewIngressChecker(policy IngressPolicy) *IngressChecker {
	if policy.DefaultPolicy == "" {
		policy.DefaultPolicy = "deny"
	}

	return &IngressChecker{
		policy: policy,
		logger: log.Default().With("component", "network.ingress"),
	}
}

// Check evaluates whether inbound traffic on the given port from the given
// source address is permitted. It returns whether the connection is allowed
// and a human-readable reason.
func (ic *IngressChecker) Check(port int, sourceAddr string) (allowed bool, reason string) {
	for _, rule := range ic.policy.Rules {
		if rule.Port != port {
			continue
		}

		if matchSource(rule.Source, sourceAddr) {
			return true, fmt.Sprintf("matches ingress rule: port=%d source=%q", rule.Port, rule.Source)
		}
	}

	if ic.policy.DefaultPolicy == "allow" {
		return true, "no matching ingress rule; default policy is allow"
	}
	return false, fmt.Sprintf("no matching ingress rule for port %d from %s; default policy is deny", port, sourceAddr)
}

// matchSource tests whether a source address matches an ingress rule's source
// filter. It supports:
//   - Empty source: matches any address.
//   - "localhost" or "127.0.0.1": matches loopback addresses.
//   - CIDR notation (e.g., "10.0.0.0/8"): matches addresses within the range.
//   - Exact IP match: matches the specific address.
func matchSource(ruleSource, addr string) bool {
	ruleSource = strings.TrimSpace(ruleSource)

	// Empty source matches everything.
	if ruleSource == "" {
		return true
	}

	addr = strings.TrimSpace(addr)

	// Strip port from addr if present (e.g., "127.0.0.1:54321" → "127.0.0.1").
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}

	// Localhost matching.
	if ruleSource == "localhost" || ruleSource == "127.0.0.1" {
		return isLoopback(addr)
	}

	// CIDR range matching.
	if strings.Contains(ruleSource, "/") {
		_, cidr, err := net.ParseCIDR(ruleSource)
		if err != nil {
			return false
		}
		ip := net.ParseIP(addr)
		if ip == nil {
			return false
		}
		return cidr.Contains(ip)
	}

	// Exact IP match.
	return addr == ruleSource
}

// isLoopback checks whether the given address is a loopback address.
func isLoopback(addr string) bool {
	ip := net.ParseIP(addr)
	if ip == nil {
		return addr == "localhost"
	}
	return ip.IsLoopback()
}

// generateIngressIPTables generates iptables rules for the INPUT chain based
// on the ingress policy.
func generateIngressIPTables(policy *IngressPolicy) ([]string, error) {
	var rules []string
	logger := log.Default().With("component", "network.ingress.iptables")

	// Always allow loopback traffic.
	rules = append(rules, "iptables -A INPUT -i lo -j ACCEPT")

	// Always allow established/related connections.
	rules = append(rules, "iptables -A INPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT")

	// Generate rules for each ingress entry.
	for _, rule := range policy.Rules {
		entry, err := ingressRuleToIPTable(rule)
		if err != nil {
			return nil, fmt.Errorf("generating iptables for ingress rule (port=%d): %w", rule.Port, err)
		}
		rules = append(rules, entry...)
	}

	// Set default policy for the INPUT chain.
	defaultAction := "DROP"
	if policy.DefaultPolicy == "allow" {
		defaultAction = "ACCEPT"
	}
	rules = append(rules, fmt.Sprintf("iptables -P INPUT %s", defaultAction))

	logger.Debug("generated ingress iptables rules", "count", len(rules))

	return rules, nil
}

// ingressRuleToIPTable converts a single ingress rule into iptables commands.
func ingressRuleToIPTable(rule IngressRule) ([]string, error) {
	var rules []string

	base := fmt.Sprintf("iptables -A INPUT -p tcp --dport %d", rule.Port)

	source := strings.TrimSpace(rule.Source)
	switch {
	case source == "":
		// No source filter — accept from anywhere.
		rules = append(rules, base+" -j ACCEPT")

	case source == "localhost" || source == "127.0.0.1":
		// Loopback only.
		rules = append(rules, base+" -s 127.0.0.0/8 -j ACCEPT")

	case strings.Contains(source, "/"):
		// CIDR range.
		_, _, err := net.ParseCIDR(source)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", source, err)
		}
		rules = append(rules, fmt.Sprintf("%s -s %s -j ACCEPT", base, source))

	default:
		// Exact IP.
		if ip := net.ParseIP(source); ip == nil {
			return nil, fmt.Errorf("invalid source address %q", source)
		}
		rules = append(rules, fmt.Sprintf("%s -s %s -j ACCEPT", base, source))
	}

	return rules, nil
}

// ingressRuleToFlag converts an ingress rule to a container runtime port
// publish flag (compatible with both docker and podman).
//
// Examples:
//
//	{Port: 8080, Source: ""} → "-p 8080:8080"
//	{Port: 3000, Source: "localhost"} → "-p 127.0.0.1:3000:3000"
//	{Port: 9090, Source: "10.0.0.1"} → "-p 10.0.0.1:9090:9090"
func ingressRuleToFlag(rule IngressRule) string {
	source := strings.TrimSpace(rule.Source)

	switch {
	case source == "" || strings.Contains(source, "/"):
		// No source or CIDR — publish on all interfaces.
		// CIDR filtering cannot be done via -p flag; it requires iptables.
		return fmt.Sprintf("-p %d:%d", rule.Port, rule.Port)

	case source == "localhost":
		return fmt.Sprintf("-p 127.0.0.1:%d:%d", rule.Port, rule.Port)

	default:
		// Exact IP — bind to that address.
		return fmt.Sprintf("-p %s:%d:%d", source, rule.Port, rule.Port)
	}
}
