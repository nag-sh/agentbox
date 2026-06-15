package network

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// dnsResolver abstracts DNS lookups for testability. The default
// implementation uses [net.Resolver].
type dnsResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// defaultResolver is the production DNS resolver.
var defaultResolver dnsResolver = &net.Resolver{}

// dnsLookupTimeout is the maximum time allowed for a single DNS lookup
// during iptables rule generation.
const dnsLookupTimeout = 5 * time.Second

// EgressChecker evaluates outbound connection attempts against the configured
// egress rules. It supports DNS resolution for hostname-based rules and glob
// matching for wildcard domains.
//
// EgressChecker is safe for concurrent use once constructed.
type EgressChecker struct {
	policy   EgressPolicy
	resolver dnsResolver
	logger   *log.Logger
}

// NewEgressChecker constructs a new [EgressChecker] from the given
// [EgressPolicy]. It normalizes the default policy and uses the system DNS
// resolver for hostname lookups.
func NewEgressChecker(policy EgressPolicy) *EgressChecker {
	if policy.DefaultPolicy == "" {
		policy.DefaultPolicy = "deny"
	}

	return &EgressChecker{
		policy:   policy,
		resolver: defaultResolver,
		logger:   log.Default().With("component", "network.egress"),
	}
}

// Check evaluates whether outbound traffic to the given host and port is
// permitted. The host may be a domain name or IP address. It returns whether
// the connection is allowed and a human-readable reason.
func (ec *EgressChecker) Check(host string, port int) (allowed bool, reason string) {
	host = strings.TrimSpace(host)
	if host == "" {
		return false, "empty host"
	}

	for _, rule := range ec.policy.Rules {
		if matchHost(rule.Host, host) {
			if len(rule.Ports) == 0 || containsPort(rule.Ports, port) {
				return true, fmt.Sprintf("matches egress rule: host=%q ports=%v", rule.Host, rule.Ports)
			}
		}
	}

	if ec.policy.DefaultPolicy == "allow" {
		return true, "no matching egress rule; default policy is allow"
	}
	return false, fmt.Sprintf("no matching egress rule for %s:%d; default policy is deny", host, port)
}

// matchHost tests whether a rule's host pattern matches the given target host.
// It supports:
//   - Exact matches: "api.github.com" matches "api.github.com"
//   - Glob wildcards: "*.github.com" matches "api.github.com"
//   - Single star: "*" matches everything
func matchHost(pattern, host string) bool {
	pattern = strings.TrimSpace(strings.ToLower(pattern))
	host = strings.TrimSpace(strings.ToLower(host))

	if pattern == "*" {
		return true
	}

	// Exact match.
	if pattern == host {
		return true
	}

	// Glob match using filepath.Match. This handles patterns like
	// "*.github.com" matching "api.github.com".
	if matched, err := filepath.Match(pattern, host); err == nil && matched {
		return true
	}

	// Handle subdomain wildcard: "*.example.com" should match
	// "sub.sub.example.com" as well. filepath.Match doesn't handle this
	// case because it treats dots as segment separators differently.
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*")
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}

	return false
}

// containsPort checks whether the given port is in the allowed ports list.
func containsPort(ports []int, port int) bool {
	for _, p := range ports {
		if p == port {
			return true
		}
	}
	return false
}

// generateEgressIPTables generates iptables rules for the OUTPUT chain based
// on the egress policy. For hostname-based rules, it performs DNS resolution
// to obtain IP addresses.
func generateEgressIPTables(policy *EgressPolicy) ([]string, error) {
	var rules []string
	logger := log.Default().With("component", "network.egress.iptables")

	// Always allow loopback traffic.
	rules = append(rules, "iptables -A OUTPUT -o lo -j ACCEPT")

	// Always allow established/related connections.
	rules = append(rules, "iptables -A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT")

	// Always allow DNS lookups (needed for hostname resolution).
	rules = append(rules, "iptables -A OUTPUT -p udp --dport 53 -j ACCEPT")
	rules = append(rules, "iptables -A OUTPUT -p tcp --dport 53 -j ACCEPT")

	// Generate rules for each egress entry.
	for _, rule := range policy.Rules {
		ruleEntries, err := egressRuleToIPTables(rule, logger)
		if err != nil {
			return nil, fmt.Errorf("generating iptables for egress rule (host=%q): %w", rule.Host, err)
		}
		rules = append(rules, ruleEntries...)
	}

	// Set default policy for the OUTPUT chain.
	defaultAction := "DROP"
	if policy.DefaultPolicy == "allow" {
		defaultAction = "ACCEPT"
	}
	rules = append(rules, fmt.Sprintf("iptables -P OUTPUT %s", defaultAction))

	return rules, nil
}

// egressRuleToIPTables converts a single egress rule into one or more iptables
// commands. For hostname-based rules, it resolves the hostname to IP addresses
// and creates a rule for each resolved address.
func egressRuleToIPTables(rule EgressRule, logger *log.Logger) ([]string, error) {
	var rules []string

	// Determine target IPs. If the host is an IP address, use it directly.
	// If it's a hostname (or wildcard), resolve it.
	ips, err := resolveHostToIPs(rule.Host, logger)
	if err != nil {
		return nil, err
	}

	for _, ip := range ips {
		if len(rule.Ports) == 0 {
			// Allow all ports to this IP.
			rules = append(rules, fmt.Sprintf("iptables -A OUTPUT -d %s -j ACCEPT", ip))
		} else {
			// Allow specific ports to this IP.
			for _, port := range rule.Ports {
				rules = append(rules,
					fmt.Sprintf("iptables -A OUTPUT -d %s -p tcp --dport %d -j ACCEPT", ip, port),
				)
			}
		}
	}

	return rules, nil
}

// resolveHostToIPs resolves a hostname to its IP addresses. If the host is
// already an IP address, it returns a single-element slice. Wildcard patterns
// are logged as warnings because they cannot be directly translated to IP-
// based iptables rules; a comment-only rule is returned instead.
func resolveHostToIPs(host string, logger *log.Logger) ([]string, error) {
	// Check if the host is already an IP address.
	if ip := net.ParseIP(host); ip != nil {
		return []string{host}, nil
	}

	// Wildcard patterns cannot be resolved to IPs.
	if strings.Contains(host, "*") {
		logger.Warn("wildcard host patterns require DNS-based filtering; "+
			"iptables rules will use resolved IPs at generation time",
			"host", host,
		)
		// Attempt to resolve the base domain (e.g., "github.com" from "*.github.com").
		baseDomain := strings.TrimPrefix(host, "*.")
		if baseDomain == host || baseDomain == "" {
			// Pure wildcard or unparseable — cannot resolve.
			return nil, nil
		}
		host = baseDomain
	}

	ctx, cancel := context.WithTimeout(context.Background(), dnsLookupTimeout)
	defer cancel()

	addrs, err := defaultResolver.LookupHost(ctx, host)
	if err != nil {
		logger.Warn("DNS resolution failed for egress host", "host", host, "err", err)
		// Return empty — don't fail the entire generation for a DNS issue.
		// The caller may retry or the host may not be reachable.
		return nil, nil
	}

	logger.Debug("resolved egress host", "host", host, "ips", addrs)
	return addrs, nil
}
