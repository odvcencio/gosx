package docs

import (
	"os"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestSiteShellExposesFunctionalSearchAndCurrentVersion(t *testing.T) {
	source, err := os.ReadFile("layout.gsx")
	if err != nil {
		t.Fatalf("read layout: %v", err)
	}
	body := string(source)
	for _, want := range []string{
		`action="/docs"`,
		`name="q"`,
		`href="/api/site"`,
		`site.frameworkVersion`,
		`href="/docs/typed-live"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("layout is missing %q", want)
		}
	}
	if got := SiteBuildInfo()["frameworkVersion"]; got != "v"+gosx.Version {
		t.Fatalf("frameworkVersion = %q, want v%s", got, gosx.Version)
	}
}
