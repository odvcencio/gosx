package server

import "testing"

func TestRemoveNonceSourcesPreservesDirectiveBoundaries(t *testing.T) {
	tests := []struct {
		name   string
		policy string
		want   string
	}{
		{
			name:   "semicolon without surrounding whitespace",
			policy: "default-src 'self';script-src 'self' 'nonce-{nonce}';style-src 'self';",
			want:   "default-src 'self'; script-src 'self'; style-src 'self';",
		},
		{
			name:   "multiple nonce sources",
			policy: "script-src 'self' 'nonce-one' 'nonce-two'; img-src https:",
			want:   "script-src 'self'; img-src https:",
		},
		{
			name:   "nonce-only directive fails closed",
			policy: "default-src 'none'; script-src 'nonce-{nonce}';",
			want:   "default-src 'none'; script-src 'none';",
		},
		{
			name:   "nonce-only script directive cannot fall back to unsafe default",
			policy: "default-src 'self' 'unsafe-inline'; script-src 'nonce-{nonce}'",
			want:   "default-src 'self' 'unsafe-inline'; script-src 'none'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := removeNonceSources(tt.policy); got != tt.want {
				t.Fatalf("removeNonceSources(%q) = %q, want %q", tt.policy, got, tt.want)
			}
		})
	}
}

func TestNormalizeSecurityPolicySharedNonceOnlyFailsClosed(t *testing.T) {
	policy := normalizeSecurityPolicy(SecurityPolicy{
		ContentSecurityPolicy: "script-src 'nonce-{nonce}'",
	})
	if got, want := policy.SharedContentSecurityPolicy, "script-src 'none'"; got != want {
		t.Fatalf("shared policy = %q, want %q", got, want)
	}
}
