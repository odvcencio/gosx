package ouroboros

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const inventoryReconstructionMethod = "gosx.ouroboros.inventory-reconstruction.replay.v1"

type ReconstructionEvidence struct {
	Method              string `json:"method"`
	BaseRevision        string `json:"baseRevision"`
	ObservedOverlayHash string `json:"observedOverlayHash"`
	PatchSHA            string `json:"patchSHA"`
	UntrackedCount      int    `json:"untrackedCount"`
	Isolated            bool   `json:"isolated"`
	Applied             bool   `json:"applied"`
	Verified            bool   `json:"verified"`
}

func ReplayInventoryReconstruction(ctx context.Context, repoRoot string, inv *Inventory) (ReconstructionEvidence, error) {
	evidence := ReconstructionEvidence{Method: inventoryReconstructionMethod}
	if err := ctx.Err(); err != nil {
		return evidence, err
	}
	if inv == nil {
		return evidence, fmt.Errorf("inventory is nil")
	}
	evidence.BaseRevision = inv.BaseRevision
	if !validGitRevision(inv.BaseRevision) {
		return evidence, fmt.Errorf("unsafe base revision %q", inv.BaseRevision)
	}
	if inv.Overlay.BaseRevision != "" && inv.Overlay.BaseRevision != inv.BaseRevision {
		return evidence, fmt.Errorf("overlay baseRevision = %q, want %q", inv.Overlay.BaseRevision, inv.BaseRevision)
	}
	if inv.Overlay.Hash != "" && inv.Overlay.Hash != inv.OverlayHash {
		return evidence, fmt.Errorf("overlay hash = %q, want %q", inv.Overlay.Hash, inv.OverlayHash)
	}
	if inv.OverlayHash == "" {
		return evidence, fmt.Errorf("inventory overlayHash is empty")
	}
	if _, err := gitOutput(ctx, repoRoot, "git", "cat-file", "-e", inv.BaseRevision+"^{commit}"); err != nil {
		return evidence, fmt.Errorf("base revision unavailable: %w", err)
	}

	tempRoot, err := os.MkdirTemp("", "gosx-ouroboros-reconstruction-*")
	if err != nil {
		return evidence, err
	}
	defer os.RemoveAll(tempRoot)

	cloneRoot := filepath.Join(tempRoot, "repo")
	if _, err := gitOutput(ctx, "", "git", "clone", "--no-checkout", "--no-local", repoRoot, cloneRoot); err != nil {
		return evidence, fmt.Errorf("clone isolated repository: %w", err)
	}
	evidence.Isolated = true
	if _, err := gitOutput(ctx, cloneRoot, "git", "checkout", "--detach", inv.BaseRevision); err != nil {
		return evidence, fmt.Errorf("checkout base revision: %w", err)
	}

	patch, err := readAndVerifyReconstructionPatch(repoRoot, inv)
	if err != nil {
		return evidence, err
	}
	evidence.PatchSHA = sha256String(string(patch))
	if len(patch) > 0 {
		patchPath, err := containedReconstructionEvidencePath(repoRoot, inv.Overlay.PatchPath)
		if err != nil {
			return evidence, err
		}
		if _, err := gitOutput(ctx, cloneRoot, "git", "apply", "--index", "--binary", patchPath); err != nil {
			return evidence, fmt.Errorf("apply tracked overlay patch: %w", err)
		}
	}
	if err := restoreReconstructionUntracked(repoRoot, cloneRoot, inv.Overlay.ArchivePath, inv.Overlay.UntrackedSources); err != nil {
		return evidence, err
	}
	evidence.Applied = true

	rebuilt, err := BuildOverlayEvidence(ctx, cloneRoot, inv.BaseRevision)
	if err != nil {
		return evidence, fmt.Errorf("build isolated overlay evidence: %w", err)
	}
	evidence.ObservedOverlayHash = rebuilt.Hash
	evidence.UntrackedCount = len(rebuilt.UntrackedSources)
	if err := verifyReconstructedOverlay(inv, rebuilt); err != nil {
		return evidence, err
	}
	evidence.Verified = true
	return evidence, nil
}

func readAndVerifyReconstructionPatch(repoRoot string, inv *Inventory) ([]byte, error) {
	if inv.OverlayHash == OverlayClean && inv.Overlay.PatchPath == "" {
		return nil, nil
	}
	if inv.Overlay.PatchPath == "" {
		return nil, fmt.Errorf("reconstruction patchPath is empty")
	}
	patchPath, err := containedReconstructionEvidencePath(repoRoot, inv.Overlay.PatchPath)
	if err != nil {
		return nil, err
	}
	patch, err := os.ReadFile(patchPath)
	if err != nil {
		return nil, fmt.Errorf("read reconstruction patch: %w", err)
	}
	if inv.Overlay.TrackedDiffHash != "" {
		if got := sha256String(string(patch)); got != inv.Overlay.TrackedDiffHash {
			return nil, fmt.Errorf("reconstruction patch hash = %s, want %s", got, inv.Overlay.TrackedDiffHash)
		}
	}
	return patch, nil
}

func restoreReconstructionUntracked(repoRoot, cloneRoot, archivePath string, sources []UntrackedSourceHash) error {
	if len(sources) == 0 {
		return nil
	}
	if archivePath == "" {
		return fmt.Errorf("reconstruction archivePath is empty")
	}
	archiveRoot, err := containedReconstructionEvidencePath(repoRoot, archivePath)
	if err != nil {
		return err
	}
	if err := rejectUnexpectedArchiveSources(archiveRoot, sources); err != nil {
		return err
	}
	for _, src := range sources {
		body, err := verifyArchivedUntrackedSource(archiveRoot, src)
		if err != nil {
			return err
		}
		target, err := safeJoin(cloneRoot, src.Path)
		if err != nil {
			return err
		}
		if err := preflightReconstructionRestoreTarget(cloneRoot, target); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		switch src.Type {
		case "file":
			mode, err := parseOverlayFileMode(src)
			if err != nil {
				return err
			}
			if err := rejectReconstructionFinalSymlink(target); err != nil {
				return err
			}
			if err := os.WriteFile(target, body, mode); err != nil {
				return err
			}
			if err := os.Chmod(target, mode); err != nil {
				return err
			}
		case "symlink":
			targetText := string(body)
			if err := validateSafeSymlinkTarget(targetText); err != nil {
				return err
			}
			_ = os.Remove(target)
			if err := os.Symlink(targetText, target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported untracked source type %q for %s", src.Type, src.Path)
		}
	}
	return nil
}

func preflightReconstructionRestoreTarget(cloneRoot, target string) error {
	cloneAbs, err := filepath.Abs(cloneRoot)
	if err != nil {
		return err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(cloneAbs, targetAbs)
	if err != nil {
		return err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("reconstructed untracked source path escapes clone root")
	}
	dirRel := filepath.Dir(rel)
	if dirRel == "." {
		return nil
	}
	current := cloneAbs
	for _, part := range strings.Split(dirRel, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("reconstructed untracked source path has symlink ancestor")
		}
		if !info.IsDir() {
			return fmt.Errorf("reconstructed untracked source path ancestor is not a directory")
		}
	}
	return verifyReconstructionTargetParent(cloneRoot, target)
}

func rejectReconstructionFinalSymlink(target string) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("reconstructed untracked source target is a symlink")
	}
	return nil
}

func rejectUnexpectedArchiveSources(archiveRoot string, sources []UntrackedSourceHash) error {
	want := map[string]bool{}
	for _, src := range sources {
		clean, err := cleanOverlayPath(src.Path)
		if err != nil {
			return err
		}
		want[clean] = true
	}
	return filepath.WalkDir(archiveRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(archiveRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !want[rel] {
			return fmt.Errorf("unexpected archived untracked source %s", rel)
		}
		return nil
	})
}

func verifyArchivedUntrackedSource(archiveRoot string, src UntrackedSourceHash) ([]byte, error) {
	path, err := safeJoin(archiveRoot, src.Path)
	if err != nil {
		return nil, err
	}
	if err := verifyReconstructionArchiveParent(archiveRoot, path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read archived untracked source %s: %w", src.Path, err)
	}
	gotMode := fmt.Sprintf("%04o", info.Mode().Perm())
	if gotMode != src.Mode {
		return nil, fmt.Errorf("archived untracked source %s mode = %s, want %s", src.Path, gotMode, src.Mode)
	}
	var body []byte
	switch src.Type {
	case "file":
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("archived untracked source %s is %s, want file", src.Path, info.Mode().String())
		}
		if err := verifyReconstructionArchiveFile(archiveRoot, path); err != nil {
			return nil, err
		}
		body, err = os.ReadFile(path)
	case "symlink":
		if info.Mode()&os.ModeSymlink == 0 {
			return nil, fmt.Errorf("archived untracked source %s is %s, want symlink", src.Path, info.Mode().String())
		}
		var target string
		target, err = os.Readlink(path)
		body = []byte(target)
		if err == nil {
			err = validateSafeSymlinkTarget(target)
		}
	default:
		return nil, fmt.Errorf("unsupported untracked source type %q for %s", src.Type, src.Path)
	}
	if err != nil {
		return nil, err
	}
	if int64(len(body)) != src.Bytes {
		return nil, fmt.Errorf("archived untracked source %s bytes = %d, want %d", src.Path, len(body), src.Bytes)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != src.SHA256 {
		return nil, fmt.Errorf("archived untracked source %s sha256 = %s, want %s", src.Path, got, src.SHA256)
	}
	return body, nil
}

func containedReconstructionEvidencePath(root, path string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, filepath.FromSlash(candidate))
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !isSubpath(base, candidate) {
		return "", fmt.Errorf("evidence path escapes root")
	}
	return candidate, nil
}

func verifyReconstructionArchiveParent(archiveRoot, path string) error {
	base, err := filepath.EvalSymlinks(archiveRoot)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !samePath(base, parent) && !isSubpath(base, parent) {
		return fmt.Errorf("archived untracked source path escapes archive root")
	}
	return nil
}

func verifyReconstructionArchiveFile(archiveRoot, path string) error {
	base, err := filepath.EvalSymlinks(archiveRoot)
	if err != nil {
		return err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if !isSubpath(base, realPath) {
		return fmt.Errorf("archived untracked source file escapes archive root")
	}
	return nil
}

func verifyReconstructionTargetParent(cloneRoot, path string) error {
	base, err := filepath.EvalSymlinks(cloneRoot)
	if err != nil {
		return err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !samePath(base, parent) && !isSubpath(base, parent) {
		return fmt.Errorf("reconstructed untracked source path escapes clone root")
	}
	return nil
}

func verifyReconstructedOverlay(inv *Inventory, rebuilt OverlayEvidence) error {
	if rebuilt.Hash != inv.OverlayHash {
		return fmt.Errorf("isolated overlay hash = %s, want %s", rebuilt.Hash, inv.OverlayHash)
	}
	if rebuilt.TrackedDiffHash != inv.Overlay.TrackedDiffHash {
		return fmt.Errorf("isolated tracked diff hash = %s, want %s", rebuilt.TrackedDiffHash, inv.Overlay.TrackedDiffHash)
	}
	if rebuilt.TrackedCachedDiffHash != inv.Overlay.TrackedCachedDiffHash {
		return fmt.Errorf("isolated cached diff hash = %s, want %s", rebuilt.TrackedCachedDiffHash, inv.Overlay.TrackedCachedDiffHash)
	}
	if !sameUntrackedSources(rebuilt.UntrackedSources, inv.Overlay.UntrackedSources) {
		return fmt.Errorf("isolated untracked source identities differ")
	}
	return nil
}

func sameUntrackedSources(left, right []UntrackedSourceHash) bool {
	left = sortedUntrackedSources(left)
	right = sortedUntrackedSources(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sortedUntrackedSources(in []UntrackedSourceHash) []UntrackedSourceHash {
	out := append([]UntrackedSourceHash(nil), in...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func cleanOverlayPath(path string) (string, error) {
	full, err := safeJoin("/", path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel("/", full)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func parseOverlayFileMode(src UntrackedSourceHash) (os.FileMode, error) {
	mode, err := strconv.ParseUint(src.Mode, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid mode %q for %s", src.Mode, src.Path)
	}
	return os.FileMode(mode), nil
}
