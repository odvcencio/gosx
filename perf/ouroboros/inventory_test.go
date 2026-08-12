package ouroboros

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCollectRecordsScopeRatchetsAndOverlayEvidence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\nJSON.parse(\"{}\");\nwindow.__gosx_runtime_ready = true;\n")
	writeFile(t, root, "client/js/patch.js", "window.__gosx_patch_sidecar = true;\n")
	writeFile(t, root, "client/js/stripe-bridge.js", "window.__gosx_stripe_sidecar = true;\n")
	writeFile(t, root, "client/wasm/main.go", "package main\nfunc register() { setRuntimeFunc(\"__gosx_host_callback\", nil) }\n")
	writeFile(t, root, "client/wasm/main_test.go", "package main\nconst testOnly = \"__gosx_test_only\"\n")
	writeFile(t, root, "client/runtime/generated/runtime-abi.ts", "export const generated = true;\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\nJSON.parse(\"{}\");\n")
	writeFile(t, root, "client/js/bootstrap-src/10-overlay.js", "globalThis.__gosx_overlay = 1;\n")
	writeFile(t, root, "client/js/relay.js", "window.__gosx_relay_sidecar = true;\n")
	if err := os.Chmod(filepath.Join(root, "client/js/relay.js"), 0o755); err != nil {
		t.Fatalf("chmod relay sidecar: %v", err)
	}
	writeFile(t, root, "tmp/proof.txt", "noise\n")

	inv, err := Collect(context.Background(), CollectOptions{
		RepoRoot:     root,
		GeneratedAt:  time.Unix(0, 0).UTC(),
		ArtifactRoot: "build/ouroboros/o0.2/test",
		Git:          true,
		Canopy:       false,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if err := ValidateInventory(inv); err != nil {
		t.Fatalf("ValidateInventory: %v", err)
	}
	if err := ValidateInventoryFresh(context.Background(), root, inv); err != nil {
		t.Fatalf("ValidateInventoryFresh: %v", err)
	}
	if inv.BaseRevision == "" || inv.BaseRevision == "unknown" {
		t.Fatalf("base revision not recorded: %q", inv.BaseRevision)
	}
	if !strings.HasPrefix(inv.OverlayHash, "sha256:") || inv.OverlayHash == OverlayClean {
		t.Fatalf("dirty overlay hash not recorded: %q", inv.OverlayHash)
	}
	if got := len(inv.Overlay.UntrackedSources); got != 2 {
		t.Fatalf("untracked source count = %d, want 2: %+v", got, inv.Overlay.UntrackedSources)
	}
	if !hasUntrackedPath(inv.Overlay.UntrackedSources, "client/js/bootstrap-src/10-overlay.js") || !hasUntrackedPath(inv.Overlay.UntrackedSources, "client/js/relay.js") {
		t.Fatalf("untracked source paths = %+v", inv.Overlay.UntrackedSources)
	}
	if len(inv.Files.Sidecars) != 3 {
		t.Fatalf("sidecar count = %d, want 3: %+v", len(inv.Files.Sidecars), inv.Files.Sidecars)
	}
	if len(inv.Files.Embedded) != 0 {
		t.Fatalf("embedded count = %d, want 0", len(inv.Files.Embedded))
	}
	if got := inv.Surface.GosxNameCount; got != 4 {
		t.Fatalf("gosx raw-all count = %d, want 4", got)
	}
	if got := inv.Surface.GosxProductionNameCount; got != 3 {
		t.Fatalf("gosx production count = %d, want 3", got)
	}
	if !containsGosxName(inv, "__gosx_test_only") {
		t.Fatal("test-only WASM token did not enter raw-all compatibility surface")
	}
	if containsGosxName(inv, "__gosx_relay_sidecar") || containsGosxName(inv, "__gosx_stripe_sidecar") {
		t.Fatalf("sidecar-only tokens entered historical denominator: %+v", inv.Surface.GosxNames)
	}
	for _, want := range []string{"__gosx_relay_sidecar", "__gosx_stripe_sidecar"} {
		if !containsBroaderGosxName(inv, want, "sidecar") {
			t.Fatalf("broader sidecar token %s missing from %+v", want, inv.Surface.BroaderBrowserGosxNames)
		}
	}
	if inv.Drift.Status != "fail-closed" {
		t.Fatalf("drift status = %q, want fail-closed", inv.Drift.Status)
	}
	if ratchetStatus(inv, "serialization-candidates") != "fail-closed" {
		t.Fatalf("serialization ratchet did not fail closed: %+v", inv.Ratchets)
	}
	artifactDir := filepath.Join(root, "perf", "ouroboros")
	if err := WriteOverlayArtifacts(context.Background(), root, artifactDir, inv.Overlay); err != nil {
		t.Fatalf("WriteOverlayArtifacts: %v", err)
	}
	for _, rel := range []string{"tracked-overlay.patch", "overlay.untracked.json", "untracked-sources/client/js/bootstrap-src/10-overlay.js"} {
		if _, err := os.Stat(filepath.Join(artifactDir, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("overlay artifact %s missing: %v", rel, err)
		}
	}
	clone := filepath.Join(t.TempDir(), "clone")
	runGit(t, "", "clone", root, clone)
	runGit(t, clone, "apply", "--binary", filepath.Join(artifactDir, "tracked-overlay.patch"))
	copyTree(t, filepath.Join(artifactDir, "untracked-sources"), clone)
	rebuilt, err := BuildOverlayEvidence(context.Background(), clone, inv.BaseRevision)
	if err != nil {
		t.Fatalf("BuildOverlayEvidence clone: %v", err)
	}
	if rebuilt.Hash != inv.OverlayHash {
		t.Fatalf("rebuilt overlay hash = %s, want %s", rebuilt.Hash, inv.OverlayHash)
	}
	stagedRoot := t.TempDir()
	runGit(t, "", "clone", root, stagedRoot)
	writeFile(t, stagedRoot, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\nJSON.parse(\"{}\");\n")
	runGit(t, stagedRoot, "add", "client/js/bootstrap-src/00-runtime.js")
	writeFile(t, stagedRoot, "client/js/bootstrap-src/10-overlay.js", "globalThis.__gosx_overlay = 1;\n")
	writeFile(t, stagedRoot, "client/js/relay.js", "window.__gosx_relay_sidecar = true;\n")
	if err := os.Chmod(filepath.Join(stagedRoot, "client/js/relay.js"), 0o755); err != nil {
		t.Fatalf("chmod staged relay sidecar: %v", err)
	}
	staged, err := BuildOverlayEvidence(context.Background(), stagedRoot, inv.BaseRevision)
	if err != nil {
		t.Fatalf("BuildOverlayEvidence staged: %v", err)
	}
	if staged.Hash != inv.OverlayHash {
		t.Fatalf("staged overlay hash = %s, want %s", staged.Hash, inv.OverlayHash)
	}
}

func TestCollectEmbeddedBrowserSources(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\n")
	writeFile(t, root, "server/navigation.go", "package server\nimport _ \"embed\"\n//go:embed navigation_runtime.js\nvar navigationRuntime string\n")
	writeFile(t, root, "server/navigation_runtime.js", "window.__gosx_navigation_runtime = true;\nJSON.stringify({ok:true});\n")
	writeFile(t, root, "auth/webauthn_script.go", "package auth\nimport _ \"embed\"\n//go:embed webauthn_runtime.js\nvar webAuthnRuntime string\n")
	writeFile(t, root, "auth/webauthn_runtime.js", "window.GoSXWebAuthn = {};\n")
	writeFile(t, root, "engine/surface/runtime_handler.go", "package surface\nimport _ \"embed\"\n//go:embed runtime/bootstrap.js\nvar bootstrapJS []byte\n")
	writeFile(t, root, "engine/surface/runtime/bootstrap.js", "window.__gosx_surface_event = function(){};\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")

	inv, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := map[string]bool{}
	for _, src := range inv.Files.Embedded {
		got[src.Path] = true
	}
	for _, want := range []string{"server/navigation_runtime.js", "auth/webauthn_runtime.js", "engine/surface/runtime/bootstrap.js"} {
		if !got[want] {
			t.Fatalf("embedded source %s missing from %+v", want, inv.Files.Embedded)
		}
	}
	if inv.Surface.BroaderBrowserNameCount == 0 || inv.Surface.BroaderSerializationSiteCount == 0 {
		t.Fatalf("broader evidence not recorded: %+v", inv.Surface)
	}
	if !containsBroaderGosxName(inv, "__gosx_navigation_runtime", "embedded") || !containsBroaderGosxName(inv, "__gosx_surface_event", "embedded") {
		t.Fatalf("embedded broader owner records missing: %+v", inv.Surface.BroaderBrowserGosxNames)
	}
}

func TestDefaultScopeIncludesEmbeddedBrowserSourceRule(t *testing.T) {
	scope := DefaultScope()
	for _, rule := range scope.Included {
		if rule.Pattern == "//go:embed browser-source patterns (*.js, *.ts, *.tsx)" {
			return
		}
	}
	t.Fatalf("embedded browser source include rule missing: %+v", scope.Included)
}

func TestSplitLinesMatchesCRLFLineSemantics(t *testing.T) {
	got := splitLines("one\r\ntwo\nthree\r")
	want := []string{"one", "two", "three\r"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitLines() = %#v, want %#v", got, want)
	}
}

func TestBrowserSourceCandidatesClassifiedOnce(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\n")
	writeFile(t, root, "client/js/patch.js", "window.__gosx_patch_sidecar = true;\n")
	writeFile(t, root, "server/embed.go", "package server\nimport _ \"embed\"\n//go:embed navigation_runtime.js\nvar navigationRuntime string\n")
	writeFile(t, root, "server/navigation_runtime.js", "window.__gosx_navigation_runtime = true;\n")
	writeFile(t, root, "client/js/runtime-01.test.js", "window.__gosx_test = true;\n")
	writeFile(t, root, "editor/assets/editor-tool.js", "window.editorTool = true;\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")

	inv, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, path := range []string{
		"client/js/bootstrap-src/00-runtime.js",
		"client/js/patch.js",
		"server/navigation_runtime.js",
		"client/js/runtime-01.test.js",
		"editor/assets/editor-tool.js",
	} {
		if got := classificationCount(inv, path); got != 1 {
			t.Fatalf("%s classification count = %d, want 1", path, got)
		}
	}
}

func hasUntrackedPath(sources []UntrackedSourceHash, path string) bool {
	for _, src := range sources {
		if src.Path == path {
			return true
		}
	}
	return false
}

func classificationCount(inv *Inventory, path string) int {
	count := 0
	for _, src := range inv.Files.Included {
		if src.Path == path {
			count++
		}
	}
	for _, src := range inv.Files.Sidecars {
		if src.Path == path {
			count++
		}
	}
	for _, src := range inv.Files.Embedded {
		if src.Path == path {
			count++
		}
	}
	for _, src := range inv.Files.Excluded {
		if src.Path == path {
			count++
		}
	}
	for _, src := range inv.Files.Audit {
		if src.Path == path {
			count++
		}
	}
	return count
}

func TestOverlayArtifactsPreserveUntrackedModes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	writeFile(t, root, "scripts/probe-tool.sh", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(root, "scripts/probe-tool.sh"), 0o755); err != nil {
		t.Fatalf("chmod probe tool: %v", err)
	}
	inv, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(inv.Overlay.UntrackedSources) != 1 {
		t.Fatalf("untracked sources = %+v", inv.Overlay.UntrackedSources)
	}
	src := inv.Overlay.UntrackedSources[0]
	if src.Mode != "0755" || src.Type != "file" {
		t.Fatalf("mode/type = %s/%s, want 0755/file", src.Mode, src.Type)
	}
	artifactDir := filepath.Join(root, "build", "o02")
	if err := WriteOverlayArtifacts(context.Background(), root, artifactDir, inv.Overlay); err != nil {
		t.Fatalf("WriteOverlayArtifacts: %v", err)
	}
	info, err := os.Stat(filepath.Join(artifactDir, "untracked-sources", "scripts", "probe-tool.sh"))
	if err != nil {
		t.Fatalf("stat archived tool: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("archived mode = %v, want 0755", info.Mode().Perm())
	}
}

func TestValidateInventoryRequiresGitAnchor(t *testing.T) {
	inv := &Inventory{
		SchemaVersion: SchemaVersion,
		Overlay:       OverlayEvidence{Hash: OverlayClean},
		Ratchets:      []ScopeRatchet{{ID: "test", Status: "pass"}},
	}
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "baseRevision") {
		t.Fatalf("ValidateInventory error = %v, want missing baseRevision", err)
	}
	inv.BaseRevision = "abcdef1"
	inv.OverlayHash = OverlayClean
	inv.Overlay.BaseRevision = "abcdef1"
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("ValidateInventory error = %v, want missing manifest anchor", err)
	}
}

func TestValidateInventoryReadyFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Inventory)
		want string
	}{
		{
			name: "parser summary",
			edit: func(inv *Inventory) {
				inv.Structural.Gotreesitter.Failed = 1
				inv.Structural.Gotreesitter.Failures = []Location{{Path: "client/js/bootstrap-src/broken.js", Line: 1}}
			},
			want: "parse failures",
		},
		{
			name: "source parser",
			edit: func(inv *Inventory) {
				inv.Files.Included = []SourceFile{{Path: "client/js/bootstrap-src/broken.js", ParseOK: false, ParseError: "syntax error"}}
				inv.Totals.IncludedFiles = 1
			},
			want: "source parser failed",
		},
		{
			name: "canonical drift",
			edit: func(inv *Inventory) { inv.Drift.Status = "fail-closed" },
			want: "drift validation failed closed",
		},
		{
			name: "ratchet",
			edit: func(inv *Inventory) { inv.Ratchets[0].Status = "fail-closed" },
			want: "ratchet test failed closed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := minimalValidInventory()
			tc.edit(inv)
			if err := ValidateInventory(inv); err != nil {
				t.Fatalf("inventory shape should remain inspectable: %v", err)
			}
			if err := ValidateInventoryReady(inv); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateInventoryReady error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestStrictDecodeRejectsUnknownAndTamperedAnchors(t *testing.T) {
	inv := minimalValidInventory()
	var buf bytes.Buffer
	if err := WriteJSON(&buf, inv); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	body := bytes.Replace(buf.Bytes(), []byte(`"schemaVersion"`), []byte(`"unknownCritical": true, "schemaVersion"`), 1)
	if _, err := DecodeInventoryStrictBytes(body); err == nil {
		t.Fatal("strict decode accepted unknown field")
	}
	tampered := *inv
	tampered.Manifest.OverlayHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if err := ValidateInventory(&tampered); err == nil {
		t.Fatal("ValidateInventory accepted tampered manifest overlay hash")
	}
}

func TestValidateInventoryRejectsMalformedSurfaceItems(t *testing.T) {
	inv := minimalValidInventory()
	inv.Surface.GosxNames = []GosxName{{Name: "__gosx_bad", Owners: []string{"client/js/bootstrap-src/a.js"}, CompatibilityClass: "internal"}}
	inv.Surface.GosxNameCount = 1
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "gosxNames") {
		t.Fatalf("ValidateInventory error = %v, want malformed gosxNames", err)
	}
	inv = minimalValidInventory()
	inv.Surface.BroaderBrowserGosxNames = []GosxName{{Name: "__gosx_bad", Owners: []string{"client/js/relay.js"}, SourceFamilies: []string{"sidecar"}, CompatibilityClass: "internal"}}
	inv.Surface.BroaderBrowserNameCount = 1
	inv.Surface.SerializationSites = []SerializationSite{{Path: "client/js/relay.js", Line: 0, Kind: "JSON.parse", Phase: "runtime", Text: "JSON.parse(x)"}}
	inv.Surface.SerializationSiteCount = 1
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "serialization site") {
		t.Fatalf("ValidateInventory error = %v, want malformed serialization site", err)
	}
}

func TestCompatibilityAuditReceiptAndReconciliation(t *testing.T) {
	receipt, err := loadCompatibilityReceipt()
	if err != nil {
		t.Fatalf("loadCompatibilityReceipt: %v", err)
	}
	if receipt.Count != 209 || receipt.NameSetHash != compatibilityReceiptHash {
		t.Fatalf("receipt count/hash = %d/%s", receipt.Count, receipt.NameSetHash)
	}
	root := findRepoRoot(t)
	inv, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	audit := inv.Surface.CompatibilityAudit
	if audit.Receipt.Count != 209 {
		t.Fatalf("receipt count = %d", audit.Receipt.Count)
	}
	if audit.Receipt.MethodVersion != compatibilityReceiptMethod || audit.Receipt.ClassifierVersion != compatibilityReceiptClassifier || audit.Receipt.Scope != compatibilityAuditScope {
		t.Fatalf("receipt metadata = %q/%q/%q, want raw receipt metadata", audit.Receipt.MethodVersion, audit.Receipt.ClassifierVersion, audit.Receipt.Scope)
	}
	if audit.Anchor.MethodVersion != compatibilityFullMethod || audit.Anchor.ClassifierVersion != compatibilityFullClassifier {
		t.Fatalf("anchor metadata = %q/%q, want full E metadata", audit.Anchor.MethodVersion, audit.Anchor.ClassifierVersion)
	}
	if len(audit.Reconciliation.AddedSinceAnchor) != 0 || len(audit.Reconciliation.RemovedSinceAnchor) != 0 {
		t.Fatalf("anchor/current changed unexpectedly: %+v", audit.Reconciliation)
	}
	wantReceiptOnly := []string{"__gosx_", "__gosx_capability_probe__", "__gosx_crdt_apply", "__gosx_handled", "__gosx_motion_mixer_", "__gosx_surface_event", "__gosx_video_prefs_probe__", "__gosx_video_sync_"}
	wantFullOnly := []string{"__gosx_bench_exports", "__gosx_current_event", "__gosx_current_handler", "__gosx_loaded_scripts", "__gosx_mount_late_engine_factory", "__gosx_page_cache", "__gosx_relay_enabled", "__gosx_relay_register_peer", "__gosx_scene3d_html", "__gosx_stop_island_fanout", "__gosx_stripe", "__gosx_submit_action", "__gosx_surface_discover"}
	if !equalStrings(audit.Reconciliation.MissingFromAnchor, wantReceiptOnly) {
		t.Fatalf("receipt-only names = %+v, want %+v", audit.Reconciliation.MissingFromAnchor, wantReceiptOnly)
	}
	if !equalStrings(audit.Reconciliation.RecoveredPreexisting, wantFullOnly) {
		t.Fatalf("full-anchor-only names = %+v, want %+v", audit.Reconciliation.RecoveredPreexisting, wantFullOnly)
	}
	if audit.Anchor.Count == 0 || audit.Current.Count == 0 {
		t.Fatalf("anchor/current audit did not run: %+v", audit)
	}
	if audit.Anchor.Scope != compatibilityFullRuntimeScope || audit.Current.Scope != compatibilityFullRuntimeScope {
		t.Fatalf("anchor/current scope = %q/%q, want full runtime scope", audit.Anchor.Scope, audit.Current.Scope)
	}
	if !audit.CanonicalAvailable || audit.Status != "pass" {
		t.Fatalf("canonical availability = %v/%s, want pass with receipt/full scope differences recorded", audit.CanonicalAvailable, audit.Status)
	}
}

func TestCompatibilityAuditRejectsUnsafeRevisionPathAndStaleIdentity(t *testing.T) {
	for _, revision := range []string{"HEAD", "../main", "-bad", "abc123;touch"} {
		if validGitRevision(revision) {
			t.Fatalf("validGitRevision accepted %q", revision)
		}
	}
	for _, path := range []string{"../escape.js", "/tmp/escape.js", "client/../../escape.js", ""} {
		if _, err := safeArchivePath(t.TempDir(), path); err == nil {
			t.Fatalf("safeArchivePath accepted %q", path)
		}
	}
	inv := minimalValidInventory()
	inv.Surface.CompatibilityAudit.Receipt.MethodVersion = compatibilityFullMethod
	inv.Surface.CompatibilityAudit.Receipt.ClassifierVersion = compatibilityFullClassifier
	inv.Surface.CompatibilityAudit.Receipt.EvidenceHash = compatibilityEvidenceHash(inv.Surface.CompatibilityAudit.Receipt)
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "malformed compatibilityAudit receipt set") {
		t.Fatalf("ValidateInventory error = %v, want mislabeled receipt metadata failure", err)
	}
	inv = minimalValidInventory()
	inv.Surface.CompatibilityAudit.Anchor.MethodVersion = compatibilityReceiptMethod
	inv.Surface.CompatibilityAudit.Anchor.ClassifierVersion = compatibilityReceiptClassifier
	inv.Surface.CompatibilityAudit.Anchor.EvidenceHash = compatibilityEvidenceHash(inv.Surface.CompatibilityAudit.Anchor)
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "malformed compatibilityAudit anchor set") {
		t.Fatalf("ValidateInventory error = %v, want mislabeled anchor metadata failure", err)
	}
	inv = minimalValidInventory()
	inv.Surface.CompatibilityAudit.Current.SourceIdentity.OverlayHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	inv.Surface.CompatibilityAudit.Current.EvidenceHash = compatibilityEvidenceHash(inv.Surface.CompatibilityAudit.Current)
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "current identity") {
		t.Fatalf("ValidateInventory error = %v, want stale current identity", err)
	}
	inv = minimalValidInventory()
	inv.Surface.CompatibilityAudit.Current.Names = append(inv.Surface.CompatibilityAudit.Current.Names, "__gosx_added_after_anchor")
	sort.Strings(inv.Surface.CompatibilityAudit.Current.Names)
	inv.Surface.CompatibilityAudit.Current.Count = len(inv.Surface.CompatibilityAudit.Current.Names)
	inv.Surface.CompatibilityAudit.Current.NameSetHash = nameSetHash(inv.Surface.CompatibilityAudit.Current.Names)
	inv.Surface.CompatibilityAudit.Current.RuntimeJSONGlobalNameHash = RuntimeJSONStaticGlobalNameHash(inv.Surface.CompatibilityAudit.Current.Names)
	inv.Surface.CompatibilityAudit.Current.EvidenceHash = compatibilityEvidenceHash(inv.Surface.CompatibilityAudit.Current)
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "reconciliation mismatch") {
		t.Fatalf("ValidateInventory error = %v, want injected current-only failure", err)
	}
	inv = minimalValidInventory()
	inv.Surface.CompatibilityAudit.Reconciliation.AddedSinceAnchor = []string{"__gosx_added_after_anchor"}
	inv.Surface.CompatibilityAudit.CanonicalAvailable = false
	inv.Surface.CompatibilityAudit.Status = "fail-closed"
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "reconciliation mismatch") {
		t.Fatalf("ValidateInventory error = %v, want forged current-only failure", err)
	}
	inv = minimalValidInventory()
	inv.Surface.CompatibilityAudit.Anchor.EvidenceHash = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	if err := ValidateInventory(inv); err == nil || !strings.Contains(err.Error(), "evidenceHash mismatch") {
		t.Fatalf("ValidateInventory error = %v, want evidenceHash tamper failure", err)
	}
}

func TestArchiveRevisionRejectsUnsupportedMemberWithoutDeadlock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\n")
	if err := os.Symlink("00-runtime.js", filepath.Join(root, "client/js/bootstrap-src/link.js")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runGitOutput(t, root, "rev-parse", "HEAD"))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := archiveRevisionToTempDir(ctx, root, base)
	if err == nil || !strings.Contains(err.Error(), "unsupported archive member") {
		t.Fatalf("archiveRevisionToTempDir error = %v, want unsupported member", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("archiveRevisionToTempDir took %s, want prompt unsupported-member return", elapsed)
	}
}

func TestFullRuntimeArchivePathspecsStayBounded(t *testing.T) {
	for _, spec := range archiveFullRuntimeCandidatePathspecs() {
		if spec == "." {
			t.Fatalf("full runtime archive pathspecs include whole repository")
		}
	}
	joined := strings.Join(archiveFullRuntimeCandidatePathspecs(), "\n")
	for _, want := range []string{":(glob)**/*.go", ":(glob)**/*.js", ":(glob)**/*.ts", ":(glob)**/*.tsx"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("full runtime archive pathspecs missing %q: %s", want, joined)
		}
	}
}

func TestValidateInventoryPixelsReference(t *testing.T) {
	inv := minimalValidInventory()
	inv.Pixels = &PixelArtifactRef{
		SchemaVersion: "gosx.ouroboros.pixels.v1",
		ManifestPath:  "pixels/manifest.json",
		Initial:       []PixelCaptureRef{{RouteID: "R08", Backend: "webgpu", Path: "pixels/r08-initial.png"}},
		Settled:       []PixelCaptureRef{{RouteID: "R08", Backend: "webgpu", Path: "pixels/r08-settled.png"}},
	}
	if err := ValidateInventory(inv); err != nil {
		t.Fatalf("ValidateInventory rejected pixels ref: %v", err)
	}
	inv.Pixels.SchemaVersion = "wrong"
	if err := ValidateInventory(inv); err == nil {
		t.Fatal("ValidateInventory accepted bad pixels schema")
	}
}

func TestValidateInventoryFreshRejectsStaleOverlay(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = false;\n")
	inv, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = 2;\n")
	if err := ValidateInventoryFresh(context.Background(), root, inv); err == nil {
		t.Fatal("ValidateInventoryFresh accepted stale overlay")
	}
}

func TestValidateInventoryFreshRejectsCompatibilityAuditTampering(t *testing.T) {
	root := newCompatibilityAuditTestRepo(t)
	inv, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if err := ValidateInventoryFresh(context.Background(), root, inv); err != nil {
		t.Fatalf("ValidateInventoryFresh clean audit: %v", err)
	}
	for _, tc := range []struct {
		name string
		edit func(*Inventory)
		want string
	}{
		{
			name: "anchor semantic hash",
			edit: func(inv *Inventory) {
				inv.Surface.CompatibilityAudit.Anchor.RuntimeJSONSemanticHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				inv.Surface.CompatibilityAudit.Anchor.EvidenceHash = compatibilityEvidenceHash(inv.Surface.CompatibilityAudit.Anchor)
			},
			want: "anchor runtimeJSONSemanticHash",
		},
		{
			name: "current counts hash",
			edit: func(inv *Inventory) {
				inv.Surface.CompatibilityAudit.Current.RuntimeJSONCountsHash = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
				inv.Surface.CompatibilityAudit.Current.EvidenceHash = compatibilityEvidenceHash(inv.Surface.CompatibilityAudit.Current)
			},
			want: "current runtimeJSONCountsHash",
		},
		{
			name: "current names",
			edit: func(inv *Inventory) {
				inv.Surface.CompatibilityAudit.Current.Names = append(inv.Surface.CompatibilityAudit.Current.Names, "__gosx_added_after_anchor")
				sort.Strings(inv.Surface.CompatibilityAudit.Current.Names)
				inv.Surface.CompatibilityAudit.Current.Count = len(inv.Surface.CompatibilityAudit.Current.Names)
				inv.Surface.CompatibilityAudit.Current.NameSetHash = nameSetHash(inv.Surface.CompatibilityAudit.Current.Names)
				inv.Surface.CompatibilityAudit.Current.RuntimeJSONGlobalNameHash = RuntimeJSONStaticGlobalNameHash(inv.Surface.CompatibilityAudit.Current.Names)
				inv.Surface.CompatibilityAudit.Current.EvidenceHash = compatibilityEvidenceHash(inv.Surface.CompatibilityAudit.Current)
				inv.Surface.CompatibilityAudit.Reconciliation.AddedSinceAnchor = []string{"__gosx_added_after_anchor"}
				inv.Surface.CompatibilityAudit.CanonicalAvailable = false
				inv.Surface.CompatibilityAudit.Status = "fail-closed"
			},
			want: "current nameSetHash",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tampered := *inv
			tc.edit(&tampered)
			if err := ValidateInventory(&tampered); err != nil {
				t.Fatalf("ValidateInventory structural tamper = %v", err)
			}
			err := ValidateInventoryFresh(context.Background(), root, &tampered)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateInventoryFresh error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestCompatibilityAuditFreshHashesAreDeterministic(t *testing.T) {
	root := newCompatibilityAuditTestRepo(t)
	first, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	second, err := Collect(context.Background(), CollectOptions{RepoRoot: root, Git: true, Canopy: false})
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	for _, tc := range []struct {
		name string
		a    CompatibilityNameSetEvidence
		b    CompatibilityNameSetEvidence
	}{
		{name: "anchor", a: first.Surface.CompatibilityAudit.Anchor, b: second.Surface.CompatibilityAudit.Anchor},
		{name: "current", a: first.Surface.CompatibilityAudit.Current, b: second.Surface.CompatibilityAudit.Current},
	} {
		if err := compareCompatibilityEvidence(tc.name, tc.a, tc.b); err != nil {
			t.Fatalf("%s evidence is not deterministic: %v", tc.name, err)
		}
	}
	if !equalStrings(first.Surface.CompatibilityAudit.Reconciliation.RecoveredPreexisting, second.Surface.CompatibilityAudit.Reconciliation.RecoveredPreexisting) ||
		!equalStrings(first.Surface.CompatibilityAudit.Reconciliation.MissingFromAnchor, second.Surface.CompatibilityAudit.Reconciliation.MissingFromAnchor) {
		t.Fatalf("compatibility reconciliation is not deterministic")
	}
}

func newCompatibilityAuditTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\nJSON.parse(\"{}\");\n")
	writeFile(t, root, "client/js/patch.js", "window.__gosx_patch_sidecar = true;\n")
	writeFile(t, root, "client/wasm/main.go", "package main\nfunc register() { setRuntimeFunc(\"__gosx_host_callback\", nil) }\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	return root
}

func TestUnsafePathsAndSymlinksRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "client/js/bootstrap-src/00-runtime.js", "window.__gosx_runtime_ready = true;\n")
	runGit(t, root, "init")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	runGit(t, root, "config", "user.name", "test")
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-m", "base")
	if _, _, err := readUntrackedSource(root, "../escape.js"); err == nil {
		t.Fatal("readUntrackedSource accepted escaping path")
	}
	if _, _, err := readUntrackedSource(root, "/tmp/escape.js"); err == nil {
		t.Fatal("readUntrackedSource accepted absolute path")
	}
	if err := os.Symlink("../escape.js", filepath.Join(root, "unsafe-link.js")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, _, err := readUntrackedSource(root, "unsafe-link.js"); err == nil {
		t.Fatal("readUntrackedSource accepted escaping symlink")
	}
	if err := os.Symlink("client/js/bootstrap-src/00-runtime.js", filepath.Join(root, "safe-link.js")); err != nil {
		t.Fatalf("safe symlink: %v", err)
	}
	src, _, err := readUntrackedSource(root, "safe-link.js")
	if err != nil {
		t.Fatalf("safe symlink rejected: %v", err)
	}
	if src.Type != "symlink" || src.Mode != "0777" {
		t.Fatalf("safe symlink type/mode = %s/%s", src.Type, src.Mode)
	}
	if err := WriteOverlayArtifacts(context.Background(), root, filepath.Join(root, "artifacts"), OverlayEvidence{UntrackedSources: []UntrackedSourceHash{{Path: "../escape.js", Type: "file", Mode: "0644"}}}); err == nil {
		t.Fatal("WriteOverlayArtifacts accepted archive escape")
	}
}

func TestSchemaAndCorpusRequireGitAnchor(t *testing.T) {
	for _, rel := range []string{"baseline.schema.json", "corpus.v1.json"} {
		body, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Fatalf("%s is invalid JSON: %v", rel, err)
		}
		encoded := string(body)
		for _, want := range []string{"baseRevision", "overlayHash"} {
			if !strings.Contains(encoded, want) {
				t.Fatalf("%s does not contain %s", rel, want)
			}
		}
	}
}

func TestHistoricalDriftRatchetsAreMonotonicCeilings(t *testing.T) {
	inv := minimalValidInventory()
	inv.Totals.IncludedJavaScriptLines = canonicalLines - 1
	inv.Totals.IncludedBytes = 3815610 - 1
	inv.Surface.GosxNameCount = canonicalGosx - 1
	inv.Surface.GosxProductionNameCount = 207
	inv.Surface.GosxJavaScriptNameCount = 177
	inv.Surface.AssignedBrowserRootCount = 120
	inv.Surface.AssignedWindowCount = 119
	inv.Surface.GoPublishedABICount = 63
	inv.Surface.HostCallbackCount = 5
	inv.Surface.SerializationSiteCount = canonicalJSON
	fillDrift(inv)
	for _, id := range []string{
		"js-source-lines",
		"js-source-bytes",
		"compat-gosx-raw-all",
		"compat-gosx-production-raw",
		"compat-gosx-js-raw",
		"compat-assigned-browser-root",
		"compat-assigned-window",
		"compat-go-published-abi",
		"compat-host-callbacks",
	} {
		if got := ratchetStatus(inv, id); got != "pass" {
			t.Fatalf("reduction ratchet %s = %q, want pass", id, got)
		}
	}

	inv.Totals.IncludedJavaScriptLines = canonicalLines + 1
	fillDrift(inv)
	if got := ratchetStatus(inv, "js-source-lines"); got != "fail-closed" {
		t.Fatalf("source growth ratchet = %q, want fail-closed", got)
	}
}

func TestSchemaClosesCompatibilityMetadataEnums(t *testing.T) {
	body, err := os.ReadFile("baseline.schema.json")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(body, &schema); err != nil {
		t.Fatalf("schema is invalid JSON: %v", err)
	}
	defs := schema["$defs"].(map[string]any)
	nameSet := defs["compatibilityNameSet"].(map[string]any)
	props := nameSet["properties"].(map[string]any)
	for field, wants := range map[string][]string{
		"methodVersion":     {compatibilityReceiptMethod, compatibilityFullMethod},
		"classifierVersion": {compatibilityReceiptClassifier, compatibilityFullClassifier},
	} {
		prop := props[field].(map[string]any)
		enum, ok := prop["enum"].([]any)
		if !ok {
			t.Fatalf("%s is not schema-closed by enum: %+v", field, prop)
		}
		got := make([]string, 0, len(enum))
		for _, value := range enum {
			got = append(got, value.(string))
		}
		sort.Strings(got)
		sort.Strings(wants)
		if !equalStrings(got, wants) {
			t.Fatalf("%s enum = %+v, want %+v", field, got, wants)
		}
	}
}

func TestCorpusTinyGoCurrentAndFutureRows(t *testing.T) {
	manifest := DefaultCorpusManifest()
	current := map[string]string{}
	future := map[string]string{}
	for _, route := range manifest.FixtureRoutes {
		current[route.ID] = route.ExpectedTinyGoCurrent
		future[route.ID] = route.ExpectedTinyGoFuture
	}
	if len(manifest.FixtureRoutes) != 12 {
		t.Fatalf("fixture route count = %d, want 12", len(manifest.FixtureRoutes))
	}
	if _, ok := future["R09"]; ok {
		t.Fatal("stale canonical R09 route survived")
	}
	for _, id := range []string{"R04", "R09A", "R09B"} {
		if current[id] != "none" || future[id] != "core" {
			t.Fatalf("%s TinyGo = current %q future %q, want none/core", id, current[id], future[id])
		}
	}
	if future["R01"] != "core" || future["R06"] != "collab" || future["R10"] != "engine" {
		t.Fatalf("future variants not assigned as expected: R06=%q R10=%q", future["R06"], future["R10"])
	}
	planned := map[string]int{}
	currentVariants := map[string]int{}
	for _, variant := range manifest.Variants {
		switch variant.Generation {
		case "current":
			currentVariants[variant.ID] = len(variant.SelectedByRoutes)
			if variant.Status != "measured" || variant.SizeArtifact == nil || variant.WASMArtifact == nil {
				t.Fatalf("current variant %s is not measured with artifacts", variant.ID)
			}
		case "future":
			planned[variant.ID] = len(variant.SelectedByRoutes)
			if variant.Status != "planned" || variant.SizeBytes != nil || variant.BudgetBytes != nil {
				t.Fatalf("future variant %s is not planned with null sizes", variant.ID)
			}
		}
	}
	for _, id := range []string{"runtime", "islands"} {
		if currentVariants[id] == 0 {
			t.Fatalf("current variant %s has no selected routes", id)
		}
	}
	for _, id := range []string{"core", "engine", "collab"} {
		if planned[id] == 0 {
			t.Fatalf("planned variant %s has no selected routes", id)
		}
	}
	if planned["full"] != 0 {
		t.Fatalf("planned full variant has selected routes: %d", planned["full"])
	}
	manifest.Variants = append(manifest.Variants, RuntimeVariant{ID: "future-full", Generation: "future", Status: "planned", SelectedByRoutes: []string{}})
	inv := minimalValidInventory()
	inv.Manifest = manifest
	if err := ValidateInventory(inv); err == nil {
		t.Fatal("ValidateInventory accepted future-full variant")
	}
}

func containsGosxName(inv *Inventory, name string) bool {
	for _, got := range inv.Surface.GosxNames {
		if got.Name == name {
			return true
		}
	}
	return false
}

func containsBroaderGosxName(inv *Inventory, name, family string) bool {
	for _, got := range inv.Surface.BroaderBrowserGosxNames {
		if got.Name != name {
			continue
		}
		for _, gotFamily := range got.SourceFamilies {
			if gotFamily == family {
				return true
			}
		}
	}
	return false
}

func containsAuditString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("could not find repo root")
		}
		dir = next
	}
}

func ratchetStatus(inv *Inventory, id string) string {
	for _, ratchet := range inv.Ratchets {
		if ratchet.ID == id {
			return ratchet.Status
		}
	}
	return ""
}

func minimalValidInventory() *Inventory {
	inv := &Inventory{
		SchemaVersion: SchemaVersion,
		Contract:      ContractO02,
		Initiative:    Initiative,
		Spec:          Spec,
		CorpusID:      CorpusID,
		BaseRevision:  "abcdef1",
		OverlayHash:   OverlayClean,
		GeneratedAt:   "1970-01-01T00:00:00Z",
		ArtifactRoot:  "build/test",
		Scope:         DefaultScope(),
		Overlay: OverlayEvidence{
			Status:                "clean",
			Hash:                  OverlayClean,
			BaseRevision:          "abcdef1",
			TrackedDiffHash:       sha256String(""),
			TrackedCachedDiffHash: "not-hashed",
			UntrackedSources:      []UntrackedSourceHash{},
			Recreate:              []string{"git checkout abcdef1"},
		},
		Files: FileInventory{
			Included: []SourceFile{},
			Sidecars: []SourceFile{},
			Embedded: []SourceFile{},
			Excluded: []ExcludedFile{},
			Audit:    []ExcludedFile{},
		},
		Totals: Totals{ByExtension: map[string]int{}},
		Structural: Structural{
			Gotreesitter:     ParseSummary{Language: "javascript"},
			ImportsExports:   []Location{},
			FreeGlobalReads:  []string{},
			FreeGlobalWrites: []string{},
		},
		Drift: DriftReport{Status: "pass"},
		Surface: Surface{
			GosxNames:               []GosxName{},
			BroaderBrowserGosxNames: []GosxName{},
			SerializationSites:      []SerializationSite{},
		},
		Ratchets: []ScopeRatchet{{ID: "test", Scope: "test", Status: "pass", Definition: "test"}},
		Manifest: DefaultCorpusManifest(),
	}
	inv.Manifest.BaseRevision = inv.BaseRevision
	inv.Manifest.OverlayHash = inv.OverlayHash
	inv.Manifest.GeneratedAt = inv.GeneratedAt
	inv.Manifest.ArtifactRoot = inv.ArtifactRoot
	anchorManifestArtifacts(&inv.Manifest, inv.BaseRevision, inv.OverlayHash)
	inv.Surface.CompatibilityAudit = minimalCompatibilityAudit(inv.BaseRevision, inv.OverlayHash)
	return inv
}

func minimalCompatibilityAudit(baseRevision, overlayHash string) CompatibilityAudit {
	receipt, err := loadCompatibilityReceipt()
	if err != nil {
		panic(err)
	}
	receiptEvidence := compatibilityEvidenceFromNames(receipt.Names, CompatibilitySourceIdentity{
		Kind:         "pinned-receipt",
		ArtifactPath: "perf/ouroboros/compatibility_receipt.v1.json",
	})
	anchor := minimalRuntimeJSONCompatibilityEvidence(receipt.Names, CompatibilitySourceIdentity{Kind: "clean-anchor", Revision: baseRevision, OverlayHash: OverlayClean})
	current := minimalRuntimeJSONCompatibilityEvidence(receipt.Names, CompatibilitySourceIdentity{Kind: "current-overlay", Revision: baseRevision, OverlayHash: overlayHash})
	return CompatibilityAudit{
		SchemaVersion:      compatibilityAuditSchemaVersion,
		Status:             "pass",
		CanonicalAvailable: true,
		Receipt:            receiptEvidence,
		Anchor:             anchor,
		Current:            current,
		Reconciliation: CompatibilityReconciliation{
			RecoveredPreexisting: []string{},
			AddedSinceAnchor:     []string{},
			RemovedSinceAnchor:   []string{},
			MissingFromAnchor:    []string{},
		},
	}
}

func minimalRuntimeJSONCompatibilityEvidence(names []string, identity CompatibilitySourceIdentity) CompatibilityNameSetEvidence {
	set := compatibilityEvidenceFromNamesWithEvidenceAndScope(names, identity, compatibilityFullRuntimeScope, nil)
	set.RuntimeJSONSourceIdentityHash = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	set.RuntimeJSONSemanticHash = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	set.RuntimeJSONCountsHash = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	set.RuntimeJSONGlobalNameHash = RuntimeJSONStaticGlobalNameHash(set.Names)
	set.EvidenceHash = compatibilityEvidenceHash(set)
	return set
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if root != "" {
		cmd.Dir = root
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runGitOutput(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if root != "" {
		cmd.Dir = root
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		var body []byte
		var mode os.FileMode = 0o644
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			targetPath, err := os.Readlink(path)
			if err != nil {
				return err
			}
			_ = os.Remove(target)
			return os.Symlink(targetPath, target)
		}
		body, err = os.ReadFile(path)
		if err != nil {
			return err
		}
		mode = info.Mode().Perm()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, body, mode); err != nil {
			return err
		}
		return os.Chmod(target, mode)
	})
	if err != nil {
		t.Fatalf("copy tree %s -> %s: %v", from, to, err)
	}
}
