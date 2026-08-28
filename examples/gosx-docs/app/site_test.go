package docs

import (
	"testing"

	"m31labs.dev/gosx"
)

func TestPublicSiteURLUsesValidatedPublicOrigin(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://docs.example.test/base/?ignored=yes")
	if got, want := PublicBaseURL(), "https://docs.example.test/base"; got != want {
		t.Fatalf("PublicBaseURL() = %q; want %q", got, want)
	}
	if got, want := PublicSiteURL("docs/../docs/routing?tab=bad"), "https://docs.example.test/base/docs/routing"; got != want {
		t.Fatalf("PublicSiteURL() = %q; want %q", got, want)
	}
}

func TestPublicBaseURLRejectsUntrustedOrRelativeValues(t *testing.T) {
	for _, value := range []string{"", "/relative", "javascript:alert(1)"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("PUBLIC_URL", value)
			if got := PublicBaseURL(); got != defaultPublicBaseURL {
				t.Fatalf("PublicBaseURL() = %q; want fallback %q", got, defaultPublicBaseURL)
			}
		})
	}
}

func TestSiteBuildInfoReportsReleaseAndSanitizedDeploymentIdentity(t *testing.T) {
	t.Setenv("PUBLIC_URL", "https://docs.example.test/")
	t.Setenv("GOSX_DOCS_REVISION", " deadbeef ")
	t.Setenv("GOSX_DOCS_BUILT_AT", "2026-08-12T20:30:00-07:00")

	got := SiteBuildInfo()
	if got["frameworkVersion"] != "v"+gosx.Version || got["revision"] != "deadbeef" {
		t.Fatalf("unexpected build identity: %#v", got)
	}
	if got["builtAt"] != "2026-08-13T03:30:00Z" || got["publicURL"] != "https://docs.example.test" {
		t.Fatalf("unexpected normalized build metadata: %#v", got)
	}
}
