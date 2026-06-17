package network

import "github.com/nag-sh/agentbox/pkg/manifest"

// FromManifest converts the manifest network specification into the network
// policy engine's configuration format.
func FromManifest(n manifest.NetworkSpec) NetworkPolicy {
	egressRules := make([]EgressRule, 0, len(n.Egress.Allow))
	for _, r := range n.Egress.Allow {
		egressRules = append(egressRules, EgressRule{
			Host:  r.Host,
			Ports: r.Ports,
		})
	}

	ingressRules := make([]IngressRule, 0, len(n.Ingress.Allow))
	for _, r := range n.Ingress.Allow {
		ingressRules = append(ingressRules, IngressRule{
			Port:   r.Port,
			Source: r.Source,
		})
	}

	return NetworkPolicy{
		Egress: EgressPolicy{
			DefaultPolicy: string(n.Egress.DefaultPolicy),
			Rules:         egressRules,
		},
		Ingress: IngressPolicy{
			DefaultPolicy: string(n.Ingress.DefaultPolicy),
			Rules:         ingressRules,
		},
	}
}
