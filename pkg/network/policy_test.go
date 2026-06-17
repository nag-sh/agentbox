package network

import (
	"slices"
	"testing"
)

func TestNetworkPolicy_RuntimeFlags(t *testing.T) {
	cases := []struct {
		name string
		policy NetworkPolicy
		want []string
	}{
		{
			name: "deny all egress without ingress",
			policy: NetworkPolicy{
				Egress: EgressPolicy{DefaultPolicy: "deny"},
			},
			want: []string{"--network=none"},
		},
		{
			name: "deny all egress with ingress",
			policy: NetworkPolicy{
				Egress:  EgressPolicy{DefaultPolicy: "deny"},
				Ingress: IngressPolicy{Rules: []IngressRule{{Port: 8080}}},
			},
			want: []string{"-p 8080:8080"},
		},
		{
			name: "egress allow",
			policy: NetworkPolicy{
				Egress: EgressPolicy{DefaultPolicy: "allow"},
			},
			want: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.policy.RuntimeFlags()
			if !slices.Equal(got, tc.want) {
				t.Errorf("RuntimeFlags() = %v, want %v", got, tc.want)
			}
		})
	}
}
