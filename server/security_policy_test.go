package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

func TestDefaultCSPPreservesExplicitFrameOptions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy SecurityPolicy
		want   string
	}{
		{"default", SecurityPolicy{}, "frame-ancestors 'self'"},
		{"same origin", SecurityPolicy{FrameOptions: "SAMEORIGIN"}, "frame-ancestors 'self'"},
		{"deny", SecurityPolicy{FrameOptions: "DENY"}, "frame-ancestors 'none'"},
		{"normalized deny", SecurityPolicy{FrameOptions: " deny "}, "frame-ancestors 'none'"},
		{"custom policy", SecurityPolicy{FrameOptions: "DENY", ContentSecurityPolicy: "frame-ancestors https://frames.example"}, "frame-ancestors https://frames.example"},
		{"report only deny", SecurityPolicy{FrameOptions: "DENY", ReportOnly: true}, "frame-ancestors 'none'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			securityHeadersMiddleware(normalizeSecurityPolicy(tc.policy))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			header := "Content-Security-Policy"
			if tc.policy.ReportOnly {
				header += "-Report-Only"
			}
			if got := w.Header().Get(header); got != tc.want {
				t.Fatalf("CSP = %q, want %q", got, tc.want)
			}
			if got := w.Header().Get("X-Frame-Options"); got != strings.TrimSpace(tc.policy.FrameOptions) {
				t.Fatalf("frame options changed: %q", got)
			}
		})
	}
}
