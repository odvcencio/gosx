package visual

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortablePixelManifestSurvivesRelocation(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "hardware-capture")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	initial := filepath.Join(root, "R08-initial-00.png")
	diff := filepath.Join(root, "R08-initial-00.diff.png")
	for _, path := range []string{initial, diff} {
		if err := os.WriteFile(path, []byte("portable pixel fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := PixelEvidenceManifest{
		ArtifactRoot: root,
		BaselineRoot: "/private/capture/baseline",
		Mode:         string(PixelModeRecordBaseline),
		States: []PixelStateEvidence{{
			State: "initial",
			Captures: []PixelCaptureEvidence{{
				Path: initial,
				Comparison: &PixelComparison{
					BaselinePath: initial,
					DiffPath:     diff,
				},
			}},
		}},
	}
	portable, err := portablePixelManifest(manifest, PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	if portable.ArtifactRoot != portablePixelArtifactRoot || portable.BaselineRoot != "" {
		t.Fatalf("roots = %q/%q, want portable %q and empty", portable.ArtifactRoot, portable.BaselineRoot, portablePixelArtifactRoot)
	}
	capture := portable.States[0].Captures[0]
	if capture.Path != "R08-initial-00.png" || capture.Comparison.BaselinePath != "R08-initial-00.png" || capture.Comparison.DiffPath != "R08-initial-00.diff.png" {
		t.Fatalf("portable refs = %+v", capture)
	}

	moved := filepath.Join(parent, "committed", "baseline")
	if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	resolved, err := containedBaselinePNGPath(moved, capture.Path)
	if err != nil {
		t.Fatalf("resolve relocated capture: %v", err)
	}
	if resolved != filepath.Join(moved, "R08-initial-00.png") {
		t.Fatalf("resolved relocated capture = %s", resolved)
	}
}

func TestPortablePixelManifestRejectsCaptureOutsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "capture")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(parent, "outside.png")
	if err := os.WriteFile(escape, []byte("escape"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := PixelEvidenceManifest{
		Mode: string(PixelModeRecordBaseline),
		States: []PixelStateEvidence{{
			State:    "initial",
			Captures: []PixelCaptureEvidence{{Path: escape}},
		}},
	}
	if _, err := portablePixelManifest(manifest, PixelEvidenceOptions{Mode: PixelModeRecordBaseline, ArtifactRoot: root}); err == nil {
		t.Fatal("portablePixelManifest accepted capture outside artifact root")
	}
}
