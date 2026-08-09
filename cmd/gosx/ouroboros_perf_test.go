package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseViewport(t *testing.T) {
	width, height, err := parseViewport("1440x900")
	if err != nil {
		t.Fatal(err)
	}
	if width != 1440 || height != 900 {
		t.Fatalf("viewport = %dx%d", width, height)
	}
}

func TestParseViewportRejectsInvalidValue(t *testing.T) {
	if _, _, err := parseViewport("wide"); err == nil {
		t.Fatal("parseViewport accepted invalid value")
	}
}

func TestSplitCSVTrimsEmptyValues(t *testing.T) {
	got := splitCSV(" R00, R01,,R08 ")
	want := []string{"R00", "R01", "R08"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitCSV() = %#v, want %#v", got, want)
	}
}

func TestPerfOuroborosUsageMentionsCanonicalEvidenceFlags(t *testing.T) {
	var buf bytes.Buffer
	perfOuroborosUsage(&buf)
	out := buf.String()
	for _, want := range []string{"--evidence-root", "--pixel-manifest", "--source-identity", "pixel-evidence.json"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage missing %q:\n%s", want, out)
		}
	}
}

func TestOuroborosSourceIdentityUsageMentionsNoMutationContract(t *testing.T) {
	var buf bytes.Buffer
	ouroborosSourceIdentityUsage(&buf)
	out := buf.String()
	for _, want := range []string{"source-identity", "--artifact-root", "does not create or mutate", "refuses to overwrite"} {
		if !strings.Contains(out, want) {
			t.Fatalf("usage missing %q:\n%s", want, out)
		}
	}
}

func TestRejectSourceIdentityOutUnderArtifactRoot(t *testing.T) {
	root := t.TempDir()
	artifactRoot := filepath.Join(root, "browser-out")
	cases := []struct {
		name string
		out  string
	}{
		{name: "same", out: artifactRoot},
		{name: "contained", out: filepath.Join(artifactRoot, "source-identity.json")},
		{name: "clean-contained", out: filepath.Join(artifactRoot, "..", "browser-out", "source-identity.json")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := rejectSourceIdentityOutUnderArtifactRoot(tc.out, artifactRoot); err == nil {
				t.Fatalf("accepted out path %s under artifact root %s", tc.out, artifactRoot)
			}
		})
	}
	if err := rejectSourceIdentityOutUnderArtifactRoot(filepath.Join(root, "source-identity.json"), artifactRoot); err != nil {
		t.Fatalf("rejected sibling handoff output: %v", err)
	}
}

func TestRejectSourceIdentityOutUnderArtifactRootThroughSymlink(t *testing.T) {
	root := t.TempDir()
	artifactRoot := filepath.Join(root, "browser-out")
	if err := os.MkdirAll(artifactRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "handoff-link")
	if err := os.Symlink(artifactRoot, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	out := filepath.Join(link, "source-identity.json")
	if err := rejectSourceIdentityOutUnderArtifactRoot(out, artifactRoot); err == nil {
		t.Fatalf("accepted symlink-contained out path %s under artifact root %s", out, artifactRoot)
	}
}
