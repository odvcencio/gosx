package main

import (
	"flag"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/ouroboros"
)

func TestPixelSourceIdentityFromHandoffRejectsManualConflicts(t *testing.T) {
	fs := flag.NewFlagSet("visual", flag.ContinueOnError)
	fs.String("ouroboros-base-revision", "", "")
	if err := fs.Parse([]string{"--ouroboros-base-revision", "abc1234"}); err != nil {
		t.Fatal(err)
	}
	_, err := pixelSourceIdentityFromFlags(fs, filepath.Join(t.TempDir(), "source-identity.json"), "", "", "")
	if err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("pixelSourceIdentityFromFlags error = %v, want conflict", err)
	}
}

func TestPixelSourceIdentityFromHandoffDerivesSourceFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source-identity.json")
	handoff := ouroboros.SourceIdentityHandoff{
		SchemaVersion: ouroboros.SourceIdentityHandoffSchemaVersion,
		Contract:      ouroboros.ContractO02,
		ArtifactRoot:  filepath.Join(dir, "browser"),
		InventoryRef:  ouroboros.CanonicalSourceInventoryRef,
		Source: ouroboros.SourceIdentityHandoffSource{
			BaseRevision:                "abc1234",
			OverlayHash:                 ouroboros.OverlayClean,
			TrackedDiffHash:             "sha256:" + strings.Repeat("a", 64),
			UntrackedIncludedSourceHash: "sha256:" + strings.Repeat("b", 64),
			InventoryRef:                ouroboros.CanonicalSourceInventoryRef,
			InventorySHA256:             "sha256:" + strings.Repeat("c", 64),
		},
	}
	if err := ouroboros.WriteNewJSONFile(path, handoff); err != nil {
		t.Fatal(err)
	}
	fs := flag.NewFlagSet("visual", flag.ContinueOnError)
	got, err := pixelSourceIdentityFromFlags(fs, path, "manual", "manual", "manual")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseRevision != handoff.Source.BaseRevision || got.OverlayHash != handoff.Source.OverlayHash || got.InventorySHA256 != handoff.Source.InventorySHA256 {
		t.Fatalf("source = %#v, want handoff %#v", got, handoff.Source)
	}
}

func TestVisualUsageMentionsSourceIdentityHandoff(t *testing.T) {
	var b strings.Builder
	visualUsage(&b)
	if !strings.Contains(b.String(), "--ouroboros-source-identity") {
		t.Fatalf("visual usage missing source identity handoff:\n%s", b.String())
	}
}
