package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx"
)

func TestInvalidFrameAncestorsApplyPolicyWithFramingDenied(t *testing.T) {
	for _, invalid := range []struct {
		name    string
		sources []string
		options string
	}{
		{"trailing slash", []string{"https://frames.example/"}, ""},
		{"injected directive", []string{"https://frames.example; script-src *"}, ""},
		{"mixed none", []string{"'none'", "'self'"}, ""},
		{"conflicting same origin", []string{"https://frames.example"}, "SAMEORIGIN"},
		{"conflicting deny", []string{"https://frames.example"}, "DENY"},
	} {
		for _, shared := range []bool{false, true} {
			for _, reportOnly := range []bool{false, true} {
				for _, ignored := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/shared=%t/reportOnly=%t/ignored=%t", invalid.name, shared, reportOnly, ignored), func(t *testing.T) {
						app := New()
						if err := app.EnableSecurityPolicy(SecurityPolicy{FrameAncestors: []string{"https://old.example"}}); err != nil {
							t.Fatal(err)
						}
						policy := SecurityPolicy{
							FrameAncestors: invalid.sources, FrameOptions: invalid.options, ReportOnly: reportOnly,
							ContentSecurityPolicy:       "default-src 'none'; script-src 'self' 'nonce-{nonce}'; img-src data:; frame-ancestors *",
							SharedContentSecurityPolicy: "default-src 'none'; script-src 'none'; img-src 'self'; frame-ancestors https://old.example",
							ReferrerPolicy:              "no-referrer", PermissionsPolicy: "camera=()", StrictTransportSecurity: "max-age=63072000",
						}
						if ignored {
							app.EnableSecurityPolicy(policy)
						} else if err := app.EnableSecurityPolicy(policy); err == nil {
							t.Fatal("invalid framing policy returned no error")
						}
						app.Page("/", func(ctx *Context) gosx.Node {
							if shared {
								ctx.CachePublic(time.Minute)
							}
							return gosx.Text("policy test")
						})
						w := httptest.NewRecorder()
						app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
						header := "Content-Security-Policy"
						if reportOnly {
							header += "-Report-Only"
							if got := w.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'none'" {
								t.Fatalf("enforced framing = %q", got)
							}
						}
						csp := w.Header().Get(header)
						if !strings.Contains(csp, "default-src 'none'") || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Count(csp, "frame-ancestors") != 1 {
							t.Fatalf("custom CSP or framing lost: %q", csp)
						}
						if shared {
							if csp != "default-src 'none'; script-src 'none'; img-src 'self'; frame-ancestors 'none'" {
								t.Fatalf("shared CSP = %q", csp)
							}
						} else if !strings.Contains(csp, "script-src 'self' 'nonce-") || strings.Contains(csp, NoncePlaceholder) || !strings.Contains(csp, "img-src data:") {
							t.Fatalf("request CSP = %q", csp)
						}
						for name, want := range map[string]string{"X-Frame-Options": "DENY", "Referrer-Policy": "no-referrer", "Permissions-Policy": "camera=()", "Strict-Transport-Security": "max-age=63072000"} {
							if got := w.Header().Get(name); got != want {
								t.Errorf("%s = %q, want %q", name, got, want)
							}
						}
					})
				}
			}
		}
	}
}

func TestInvalidFrameAncestorsDeriveSharedPolicyWithFramingDenied(t *testing.T) {
	app := New()
	app.EnableSecurityPolicy(SecurityPolicy{FrameAncestors: []string{"https://frames.example/"}, ContentSecurityPolicy: "default-src 'none'; script-src 'nonce-{nonce}'; frame-ancestors *"})
	app.Page("/", func(ctx *Context) gosx.Node {
		ctx.CachePublic(time.Minute)
		return gosx.Text("shared policy")
	})
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if got, want := w.Header().Get("Content-Security-Policy"), "default-src 'none'; script-src 'none'; frame-ancestors 'none'"; got != want {
		t.Fatalf("derived shared CSP = %q, want %q", got, want)
	}
}

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
