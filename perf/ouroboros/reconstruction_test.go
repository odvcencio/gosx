package ouroboros

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplayInventoryReconstructionTrackedAndUntracked(t *testing.T) {
	root, inv := buildReconstructionInventory(t)

	evidence, err := ReplayInventoryReconstruction(context.Background(), root, inv)
	if err != nil {
		t.Fatalf("ReplayInventoryReconstruction: %v", err)
	}
	if evidence.Method != inventoryReconstructionMethod {
		t.Fatalf("method = %q", evidence.Method)
	}
	if evidence.BaseRevision != inv.BaseRevision {
		t.Fatalf("base revision = %q, want %q", evidence.BaseRevision, inv.BaseRevision)
	}
	if evidence.ObservedOverlayHash != inv.OverlayHash {
		t.Fatalf("observed overlay hash = %s, want %s", evidence.ObservedOverlayHash, inv.OverlayHash)
	}
	if evidence.PatchSHA != inv.Overlay.TrackedDiffHash {
		t.Fatalf("patch SHA = %s, want %s", evidence.PatchSHA, inv.Overlay.TrackedDiffHash)
	}
	if evidence.UntrackedCount != len(inv.Overlay.UntrackedSources) {
		t.Fatalf("untracked count = %d, want %d", evidence.UntrackedCount, len(inv.Overlay.UntrackedSources))
	}
	if !evidence.Isolated || !evidence.Applied || !evidence.Verified {
		t.Fatalf("evidence flags = isolated:%v applied:%v verified:%v", evidence.Isolated, evidence.Applied, evidence.Verified)
	}
}

func TestReplayInventoryReconstructionPreservesStagedNewSource(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	const stagedPath = "client/js/bootstrap-src/05-staged.js"
	if hasUntrackedPath(inv.Overlay.UntrackedSources, stagedPath) {
		t.Fatalf("staged source entered untracked set: %+v", inv.Overlay.UntrackedSources)
	}
	patch, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(inv.Overlay.PatchPath)))
	if err != nil {
		t.Fatalf("read patch: %v", err)
	}
	if !strings.Contains(string(patch), stagedPath) {
		t.Fatalf("tracked patch does not include staged source %s", stagedPath)
	}

	evidence, err := ReplayInventoryReconstruction(context.Background(), root, inv)
	if err != nil {
		t.Fatalf("ReplayInventoryReconstruction: %v", err)
	}
	if evidence.ObservedOverlayHash != inv.OverlayHash {
		t.Fatalf("observed overlay hash = %s, want %s", evidence.ObservedOverlayHash, inv.OverlayHash)
	}
	if evidence.UntrackedCount != len(inv.Overlay.UntrackedSources) {
		t.Fatalf("untracked count = %d, want %d", evidence.UntrackedCount, len(inv.Overlay.UntrackedSources))
	}
}

func TestReplayInventoryReconstructionFullIndexStableAcrossAbbrevMixedState(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	runGit(t, root, "config", "core.abbrev", "7")
	overlay, err := BuildOverlayEvidence(context.Background(), root, inv.BaseRevision)
	if err != nil {
		t.Fatalf("BuildOverlayEvidence source: %v", err)
	}
	artifactDir := filepath.Join(root, "perf/ouroboros-abbrev")
	if err := WriteOverlayArtifacts(context.Background(), root, artifactDir, overlay); err != nil {
		t.Fatalf("WriteOverlayArtifacts: %v", err)
	}
	patchPath := filepath.Join(artifactDir, "tracked-overlay.patch")
	patch, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("read patch: %v", err)
	}
	if err := requireFullIndexPatch(patch); err != nil {
		t.Fatalf("tracked overlay patch did not use full index headers: %v", err)
	}

	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", "--no-checkout", "--no-local", root, clone)
	runGit(t, clone, "checkout", "--detach", inv.BaseRevision)
	runGit(t, clone, "config", "core.abbrev", "12")
	runGit(t, clone, "apply", "--index", "--binary", patchPath)
	copyTree(t, filepath.Join(artifactDir, "untracked-sources"), clone)
	rebuilt, err := BuildOverlayEvidence(context.Background(), clone, inv.BaseRevision)
	if err != nil {
		t.Fatalf("BuildOverlayEvidence replay: %v", err)
	}
	if rebuilt.TrackedDiffHash != overlay.TrackedDiffHash {
		t.Fatalf("tracked diff hash = %s, want %s", rebuilt.TrackedDiffHash, overlay.TrackedDiffHash)
	}
	if rebuilt.Hash != overlay.Hash {
		t.Fatalf("overlay hash = %s, want %s", rebuilt.Hash, overlay.Hash)
	}
}

func TestReplayInventoryReconstructionRejectsTamperedPatch(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	patchPath := filepath.Join(root, filepath.FromSlash(inv.Overlay.PatchPath))
	f, err := os.OpenFile(patchPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open patch: %v", err)
	}
	if _, err := f.WriteString("\n# tamper\n"); err != nil {
		_ = f.Close()
		t.Fatalf("tamper patch: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close patch: %v", err)
	}

	_, err = ReplayInventoryReconstruction(context.Background(), root, inv)
	if err == nil || !strings.Contains(err.Error(), "patch hash") {
		t.Fatalf("ReplayInventoryReconstruction error = %v, want patch hash rejection", err)
	}
}

func TestReplayInventoryReconstructionRejectsPatchApplyFailure(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	runGit(t, root, "reset", "--hard", inv.BaseRevision)
	if err := os.Remove(filepath.Join(root, "client/js/bootstrap-src/00-runtime.js")); err != nil {
		t.Fatalf("remove base file: %v", err)
	}
	runGit(t, root, "add", "-u")
	runGit(t, root, "commit", "-m", "delete base file")
	baseWithoutFile := strings.TrimSpace(runGitOutput(t, root, "rev-parse", "HEAD"))
	inv.BaseRevision = baseWithoutFile
	inv.Overlay.BaseRevision = baseWithoutFile

	_, err := ReplayInventoryReconstruction(context.Background(), root, inv)
	if err == nil || !strings.Contains(err.Error(), "apply tracked overlay patch") {
		t.Fatalf("ReplayInventoryReconstruction error = %v, want apply failure", err)
	}
}

func TestReplayInventoryReconstructionRejectsUntrackedTamper(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	archivePath := filepath.Join(root, filepath.FromSlash(inv.Overlay.ArchivePath), "client/js/relay.js")
	if err := os.WriteFile(archivePath, []byte("window.__gosx_relay_tampered = true;\n"), 0o755); err != nil {
		t.Fatalf("tamper archive: %v", err)
	}

	_, err := ReplayInventoryReconstruction(context.Background(), root, inv)
	if err == nil || (!strings.Contains(err.Error(), "sha256") && !strings.Contains(err.Error(), "bytes")) {
		t.Fatalf("ReplayInventoryReconstruction error = %v, want untracked integrity rejection", err)
	}
}

func TestReplayInventoryReconstructionRejectsPathEscape(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	inv.Overlay.UntrackedSources[0].Path = "../escape.js"

	_, err := ReplayInventoryReconstruction(context.Background(), root, inv)
	if err == nil || !strings.Contains(err.Error(), "path escapes root") {
		t.Fatalf("ReplayInventoryReconstruction error = %v, want path escape rejection", err)
	}
}

func TestReplayInventoryReconstructionRejectsSymlinkedPatchEscape(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	patchPath := filepath.Join(root, filepath.FromSlash(inv.Overlay.PatchPath))
	outsidePatch := filepath.Join(t.TempDir(), "tracked-overlay.patch")
	body, err := os.ReadFile(patchPath)
	if err != nil {
		t.Fatalf("read patch: %v", err)
	}
	if err := os.WriteFile(outsidePatch, body, 0o644); err != nil {
		t.Fatalf("write outside patch: %v", err)
	}
	if err := os.Remove(patchPath); err != nil {
		t.Fatalf("remove patch: %v", err)
	}
	if err := os.Symlink(outsidePatch, patchPath); err != nil {
		t.Fatalf("symlink patch: %v", err)
	}

	_, err = ReplayInventoryReconstruction(context.Background(), root, inv)
	if err == nil || !strings.Contains(err.Error(), "evidence path escapes root") {
		t.Fatalf("ReplayInventoryReconstruction error = %v, want symlink escape rejection", err)
	}
}

func TestReplayInventoryReconstructionRejectsSymlinkedArchiveComponentEscape(t *testing.T) {
	root, inv := buildReconstructionInventory(t)
	archiveRoot := filepath.Join(root, filepath.FromSlash(inv.Overlay.ArchivePath))
	escapedDir := filepath.Join(t.TempDir(), "escaped")
	if err := os.MkdirAll(escapedDir, 0o755); err != nil {
		t.Fatalf("mkdir escaped: %v", err)
	}
	relayPath := filepath.Join(archiveRoot, "client/js/relay.js")
	body, err := os.ReadFile(relayPath)
	if err != nil {
		t.Fatalf("read relay archive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(escapedDir, "relay.js"), body, 0o755); err != nil {
		t.Fatalf("write escaped relay: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(archiveRoot, "client/js")); err != nil {
		t.Fatalf("remove archive dir: %v", err)
	}
	if err := os.Symlink(escapedDir, filepath.Join(archiveRoot, "client/js")); err != nil {
		t.Fatalf("symlink archive dir: %v", err)
	}

	_, err = ReplayInventoryReconstruction(context.Background(), root, inv)
	if err == nil || (!strings.Contains(err.Error(), "escapes archive root") && !strings.Contains(err.Error(), "unexpected archived untracked source")) {
		t.Fatalf("ReplayInventoryReconstruction error = %v, want archive symlink rejection", err)
	}
}

func TestParsePorcelainStatusPreservesLeadingAndTrailingPathSpaces(t *testing.T) {
	records := parsePorcelainStatus("??  leading.js\x00?? trailing.js \x00")
	if len(records) != 2 {
		t.Fatalf("records = %+v", records)
	}
	if records[0].Path != " leading.js" {
		t.Fatalf("leading-space path = %q", records[0].Path)
	}
	if records[1].Path != "trailing.js " {
		t.Fatalf("trailing-space path = %q", records[1].Path)
	}
}

func TestVerifyReconstructionTargetParentRejectsSymlinkEscape(t *testing.T) {
	cloneRoot := t.TempDir()
	escapedDir := filepath.Join(t.TempDir(), "escaped")
	if err := os.MkdirAll(filepath.Join(escapedDir, "js"), 0o755); err != nil {
		t.Fatalf("mkdir escaped: %v", err)
	}
	if err := os.Symlink(escapedDir, filepath.Join(cloneRoot, "client")); err != nil {
		t.Fatalf("symlink client: %v", err)
	}
	target := filepath.Join(cloneRoot, "client/js/relay.js")
	if err := verifyReconstructionTargetParent(cloneRoot, target); err == nil || !strings.Contains(err.Error(), "escapes clone root") {
		t.Fatalf("verifyReconstructionTargetParent error = %v, want escape rejection", err)
	}
}

func TestPreflightReconstructionRestoreTargetRejectsAncestorSymlinkBeforeMkdirAll(t *testing.T) {
	cloneRoot := t.TempDir()
	escapedDir := filepath.Join(t.TempDir(), "escaped")
	if err := os.MkdirAll(escapedDir, 0o755); err != nil {
		t.Fatalf("mkdir escaped: %v", err)
	}
	if err := os.Symlink(escapedDir, filepath.Join(cloneRoot, "client")); err != nil {
		t.Fatalf("symlink client: %v", err)
	}
	target := filepath.Join(cloneRoot, "client/js/relay.js")
	if err := preflightReconstructionRestoreTarget(cloneRoot, target); err == nil || !strings.Contains(err.Error(), "symlink ancestor") {
		t.Fatalf("preflightReconstructionRestoreTarget error = %v, want symlink ancestor rejection", err)
	}
	if _, err := os.Stat(filepath.Join(escapedDir, "js")); !os.IsNotExist(err) {
		t.Fatalf("preflight created escaped directory, stat error = %v", err)
	}
}

func TestRejectReconstructionFinalSymlinkBeforeFileWrite(t *testing.T) {
	cloneRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cloneRoot, "client/js"), 0o755); err != nil {
		t.Fatalf("mkdir client js: %v", err)
	}
	outsideFile := filepath.Join(t.TempDir(), "relay.js")
	if err := os.WriteFile(outsideFile, []byte("outside\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	target := filepath.Join(cloneRoot, "client/js/relay.js")
	if err := os.Symlink(outsideFile, target); err != nil {
		t.Fatalf("symlink target: %v", err)
	}
	if err := preflightReconstructionRestoreTarget(cloneRoot, target); err != nil {
		t.Fatalf("preflightReconstructionRestoreTarget: %v", err)
	}
	if err := rejectReconstructionFinalSymlink(target); err == nil || !strings.Contains(err.Error(), "target is a symlink") {
		t.Fatalf("rejectReconstructionFinalSymlink error = %v, want final symlink rejection", err)
	}
	body, err := os.ReadFile(outsideFile)
	if err != nil {
		t.Fatalf("read outside file: %v", err)
	}
	if string(body) != "outside\n" {
		t.Fatalf("outside file changed to %q", body)
	}
}

func buildReconstructionInventory(t *testing.T) (string, *Inventory) {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\n")
	writeFile(t, root, "client/js/patch.js", "window.__gosx_patch_sidecar = true;\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitOutput(t, root, "rev-parse", "HEAD"))

	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\nJSON.parse(\"{}\");\n")
	writeFile(t, root, "client/js/bootstrap-src/05-staged.js", "window.__gosx_staged_source = true;\n")
	runGit(t, root, "add", "client/js/bootstrap-src/05-staged.js")
	writeFile(t, root, "client/js/relay.js", "window.__gosx_relay_sidecar = true;\n")
	if err := os.Chmod(filepath.Join(root, "client/js/relay.js"), 0o755); err != nil {
		t.Fatalf("chmod relay: %v", err)
	}
	if err := os.Symlink("client/js/relay.js", filepath.Join(root, "client/js/bootstrap-src/10-relay-link.js")); err != nil {
		t.Fatalf("symlink relay: %v", err)
	}
	overlay, err := BuildOverlayEvidence(context.Background(), root, base)
	if err != nil {
		t.Fatalf("BuildOverlayEvidence: %v", err)
	}
	artifactDir := filepath.Join(root, "perf/ouroboros")
	if err := WriteOverlayArtifacts(context.Background(), root, artifactDir, overlay); err != nil {
		t.Fatalf("WriteOverlayArtifacts: %v", err)
	}
	overlay.PatchPath = "perf/ouroboros/tracked-overlay.patch"
	overlay.ArchivePath = "perf/ouroboros/untracked-sources"
	inv := &Inventory{
		BaseRevision: base,
		OverlayHash:  overlay.Hash,
		Overlay:      overlay,
	}
	return root, inv
}

func requireFullIndexPatch(patch []byte) error {
	indexLines := 0
	for _, line := range strings.Split(string(patch), "\n") {
		if !strings.HasPrefix(line, "index ") {
			continue
		}
		indexLines++
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return os.ErrInvalid
		}
		parts := strings.Split(fields[1], "..")
		if len(parts) != 2 || len(parts[0]) != 40 || len(parts[1]) != 40 {
			return os.ErrInvalid
		}
	}
	if indexLines == 0 {
		return os.ErrInvalid
	}
	return nil
}
