package dev

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

func TestServerProxyInjectsReloadScriptIntoHTML(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><head><title>Docs</title></head><body><main>hello</main></body></html>")
	}))
	defer upstream.Close()

	srv := &Server{
		Dir:         t.TempDir(),
		BuildDir:    t.TempDir(),
		ProxyTarget: upstream.URL,
	}
	srv.SetProxyTarget(upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "http://gosx.test/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "data-gosx-dev-reload") {
		t.Fatalf("expected reload script in body, got %q", body)
	}
	if !strings.Contains(body, "/gosx/dev/events") {
		t.Fatalf("expected reload event stream in body, got %q", body)
	}
}

func TestServerProxyInjectsSceneInspectorConfigWhenEnabled(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><head><title>Scene</title></head><body><main>hello</main></body></html>")
	}))
	defer upstream.Close()

	srv := &Server{
		Dir:            t.TempDir(),
		BuildDir:       t.TempDir(),
		ProxyTarget:    upstream.URL,
		SceneInspector: true,
	}
	srv.SetProxyTarget(upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "http://gosx.test/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "data-gosx-dev-scene-inspector") {
		t.Fatalf("expected scene inspector config script in body, got %q", body)
	}
	if !strings.Contains(body, "window.__gosx_scene3d_inspector=true") {
		t.Fatalf("expected scene inspector global in body, got %q", body)
	}
	if !strings.Contains(body, "data-gosx-dev-reload") {
		t.Fatalf("expected reload script to remain in body, got %q", body)
	}
}

func TestServerProxySkipsReloadScriptForNavigationFetches(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><html><body><main>hello</main></body></html>")
	}))
	defer upstream.Close()

	srv := &Server{
		Dir:         t.TempDir(),
		BuildDir:    t.TempDir(),
		ProxyTarget: upstream.URL,
	}
	srv.SetProxyTarget(upstream.URL)

	req := httptest.NewRequest(http.MethodGet, "http://gosx.test/docs", nil)
	req.Header.Set("X-GoSX-Navigation", "1")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "data-gosx-dev-reload") {
		t.Fatalf("did not expect reload script in navigation response: %q", rec.Body.String())
	}
}

func TestServerServesBuildAssets(t *testing.T) {
	buildDir := t.TempDir()
	writeTestFile(t, filepath.Join(buildDir, "gosx-runtime.wasm"), []byte("wasm"))
	writeTestFile(t, filepath.Join(buildDir, "bootstrap.js"), []byte("bootstrap"))
	writeTestFile(t, filepath.Join(buildDir, "islands", "Counter.json"), []byte(`{"name":"Counter"}`))
	writeTestFile(t, filepath.Join(buildDir, "css", "page.css"), []byte("body{}"))

	srv := &Server{
		Dir:      t.TempDir(),
		BuildDir: buildDir,
	}

	cases := []struct {
		path string
		want string
	}{
		{path: "/gosx/runtime.wasm", want: "wasm"},
		{path: "/gosx/bootstrap.js", want: "bootstrap"},
		{path: "/gosx/islands/Counter.json", want: `{"name":"Counter"}`},
		{path: "/gosx/css/page.css", want: "body{}"},
	}

	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "http://gosx.test"+tc.path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d", tc.path, rec.Code)
		}
		if got := rec.Body.String(); got != tc.want {
			t.Fatalf("%s: expected %q, got %q", tc.path, tc.want, got)
		}
		if cache := rec.Header().Get("Cache-Control"); !strings.Contains(cache, "no-cache") {
			t.Fatalf("%s: expected no-cache headers, got %q", tc.path, cache)
		}
	}
}

func TestServerServesBuiltRuntimeAssetFromDistAssets(t *testing.T) {
	dir := t.TempDir()
	buildDir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "dist", "assets", "runtime", "bootstrap-feature-scene3d-webgpu.hash.js"), []byte("webgpu"))

	srv := &Server{
		Dir:      dir,
		BuildDir: buildDir,
	}

	req := httptest.NewRequest(http.MethodGet, "http://gosx.test/gosx/assets/runtime/bootstrap-feature-scene3d-webgpu.hash.js", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "webgpu" {
		t.Fatalf("expected runtime asset body, got %q", got)
	}
	if cache := rec.Header().Get("Cache-Control"); !strings.Contains(cache, "no-cache") {
		t.Fatalf("expected no-cache headers, got %q", cache)
	}
}

func TestSnapshotChangedDetectsDeletion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.gsx")
	writeTestFile(t, path, []byte("<Page />"))

	before, err := projectSnapshot(dir)
	if err != nil {
		t.Fatalf("snapshot before delete: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove watched file: %v", err)
	}
	after, err := projectSnapshot(dir)
	if err != nil {
		t.Fatalf("snapshot after delete: %v", err)
	}
	if !snapshotChanged(before, after) {
		t.Fatal("expected deleted file to change snapshot")
	}
}

func TestProjectSnapshotWatchesOnlyDevSourceFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "app", "page.gsx"), []byte("<Page />"))
	writeTestFile(t, filepath.Join(dir, "app", "page.go"), []byte("package app"))
	writeTestFile(t, filepath.Join(dir, "public", "site.css"), []byte("body{}"))
	writeTestFile(t, filepath.Join(dir, "public", "app.js"), []byte("console.log('ok')"))
	writeTestFile(t, filepath.Join(dir, "README.md"), []byte("# ignored"))
	writeTestFile(t, filepath.Join(dir, "build", "bootstrap.js"), []byte("ignored"))

	snapshot, err := projectSnapshot(dir)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	for _, path := range []string{
		"app/page.gsx",
		"app/page.go",
		"public/site.css",
		"public/app.js",
	} {
		if _, ok := snapshot[path]; !ok {
			t.Fatalf("expected watched path %s in snapshot %#v", path, snapshot)
		}
	}
	for _, path := range []string{"README.md", "build/bootstrap.js"} {
		if _, ok := snapshot[path]; ok {
			t.Fatalf("did not expect ignored path %s in snapshot", path)
		}
	}
}

func TestAddProjectWatchDirsSkipsGeneratedAndVendorDirs(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{
		filepath.Join(dir, "app"),
		filepath.Join(dir, "public", "nested"),
		filepath.Join(dir, "build", "generated"),
		filepath.Join(dir, "node_modules", "pkg"),
		filepath.Join(dir, ".tmp-cache", "work"),
	} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
	}

	var watched []string
	err := addProjectWatchDirs(dir, func(path string) error {
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		watched = append(watched, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("add watch dirs: %v", err)
	}

	for _, path := range []string{".", "app", "public", "public/nested"} {
		if !containsString(watched, path) {
			t.Fatalf("expected watched dir %s in %#v", path, watched)
		}
	}
	for _, path := range []string{"build", "build/generated", "node_modules", "node_modules/pkg", ".tmp-cache", ".tmp-cache/work"} {
		if containsString(watched, path) {
			t.Fatalf("did not expect skipped dir %s in %#v", path, watched)
		}
	}
}

func TestIsProjectWatchEventFiltersSourceFiles(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "app", "page.gsx")
	readme := filepath.Join(dir, "README.md")
	generated := filepath.Join(dir, "build", "bootstrap.js")
	writeTestFile(t, source, []byte("<Page />"))
	writeTestFile(t, readme, []byte("# docs"))
	writeTestFile(t, generated, []byte("ignored"))

	if !isProjectWatchEvent(dir, fsnotify.Event{Name: source, Op: fsnotify.Write}) {
		t.Fatal("expected source write event to be watched")
	}
	if isProjectWatchEvent(dir, fsnotify.Event{Name: source, Op: fsnotify.Chmod}) {
		t.Fatal("chmod-only event should not trigger rebuild")
	}
	if isProjectWatchEvent(dir, fsnotify.Event{Name: readme, Op: fsnotify.Write}) {
		t.Fatal("markdown write event should not trigger rebuild")
	}
	if isProjectWatchEvent(dir, fsnotify.Event{Name: generated, Op: fsnotify.Write}) {
		t.Fatal("generated build output should not trigger rebuild")
	}
}

func TestNormalizeDependencyWatchDirsUsesCanonicalExternalAllowlist(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "app")
	external := t.TempDir()
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "external-alias")
	if err := os.Symlink(external, alias); err != nil {
		t.Fatal(err)
	}

	dirs, err := normalizeDependencyWatchDirs(root, []string{inside, alias, external, "", external})
	if err != nil {
		t.Fatal(err)
	}
	canonicalExternal, err := canonicalExistingWatchDir(external)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := dirs, []string{canonicalExternal}; !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized watch dirs = %#v, want %#v", got, want)
	}
}

func TestNormalizeDependencyWatchTargetsAllowsOnlyExactCanonicalSymlinkSources(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	nested := filepath.Join(external, "physical")
	physical := filepath.Join(nested, "counter.gsx")
	sibling := filepath.Join(nested, "unrelated.gsx")
	writeTestFile(t, physical, []byte(islandGSX))
	writeTestFile(t, sibling, []byte(islandGSX))
	logical := filepath.Join(external, "counter.gsx")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	targets, err := normalizeDependencyWatchTargets(root, []string{external}, []string{physical}, nil)
	if err != nil {
		t.Fatal(err)
	}
	canonicalExternal, err := canonicalExistingWatchDir(external)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := targets.dirs, []string{canonicalExternal}; !reflect.DeepEqual(got, want) {
		t.Fatalf("watch dirs = %#v, want %#v", got, want)
	}
	if got, want := targets.files, []string{physical}; !reflect.DeepEqual(got, want) {
		t.Fatalf("exact watch files = %#v, want %#v", got, want)
	}
	if got, want := dependencyKernelWatchDirs(targets), []string{external, nested}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kernel watch dirs = %#v, want package root plus exact parent %#v", got, want)
	}
	for _, path := range []string{logical, physical} {
		if !isWatchedSourceEventForTargets(root, targets, fsnotify.Event{Name: path, Op: fsnotify.Write}) {
			t.Fatalf("accepted symlink source event %s was rejected", path)
		}
	}
	if isWatchedSourceEventForTargets(root, targets, fsnotify.Event{Name: sibling, Op: fsnotify.Write}) {
		t.Fatalf("unrelated nested sibling %s inherited exact source permission", sibling)
	}
	snapshot, err := watchedSourceSnapshotForTargets(root, targets)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot[physical]; !ok {
		t.Fatalf("polling snapshot %#v omitted canonical nested source %q", snapshot, physical)
	}
	if _, ok := snapshot[sibling]; ok {
		t.Fatalf("polling snapshot %#v included unrelated nested sibling %q", snapshot, sibling)
	}

	// Directory-only callers derive the same bounded exact target from the
	// direct symlink, without walking or trusting the nested directory.
	derived, err := normalizeDependencyWatchTargets(root, []string{external}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(derived, targets) {
		t.Fatalf("directory-only exact targets = %#v, want %#v", derived, targets)
	}

	outside := filepath.Join(t.TempDir(), "outside.gsx")
	writeTestFile(t, outside, []byte(islandGSX))
	if _, err := normalizeDependencyWatchTargets(root, []string{external}, []string{outside}, nil); err == nil || !strings.Contains(err.Error(), "outside every allowlisted") {
		t.Fatalf("explicit source escape error = %v", err)
	}
	escapeDir := t.TempDir()
	escapeLogical := filepath.Join(escapeDir, "escape.gsx")
	if err := os.Symlink(outside, escapeLogical); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeDependencyWatchTargets(root, []string{escapeDir}, nil, nil); err == nil || !strings.Contains(err.Error(), "resolves outside allowlisted") {
		t.Fatalf("direct symlink escape error = %v", err)
	}
}

func TestNormalizeDependencyWatchTargetsDedupesSameDirectorySymlink(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	physical := filepath.Join(external, "counter_source.gsx")
	writeTestFile(t, physical, []byte(islandGSX))
	if err := os.Symlink(physical, filepath.Join(external, "counter.gsx")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	targets, err := normalizeDependencyWatchTargets(root, []string{external}, []string{physical}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := targets.files, []string{physical}; !reflect.DeepEqual(got, want) {
		t.Fatalf("same-directory symlink exact files = %#v, want one physical identity %#v", got, want)
	}
	if got, want := dependencyKernelWatchDirs(targets), []string{external}; !reflect.DeepEqual(got, want) {
		t.Fatalf("same-directory symlink kernel dirs = %#v, want no extra nested watch %#v", got, want)
	}
}

func TestRefreshDependencyWatchDirsUnionsPartialInvalidClosureForRecovery(t *testing.T) {
	root := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	current, err := normalizeDependencyWatchDirs(root, []string{first})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Dir:  root,
		Logf: func(string, ...any) {},
		RefreshWatchDirs: func() ([]string, error) {
			return []string{second}, errors.New("ambiguous new dependency")
		},
	}
	canonicalSecond, err := canonicalExistingWatchDir(second)
	if err != nil {
		t.Fatal(err)
	}
	wantUnion := append(append([]string(nil), current...), canonicalSecond)
	if current[0] > canonicalSecond {
		wantUnion[0], wantUnion[1] = wantUnion[1], wantUnion[0]
	}
	if got := s.refreshDependencyWatchDirs(current); !reflect.DeepEqual(got, wantUnion) {
		t.Fatalf("partial invalid closure refresh = %#v, want old+new recovery allowlist %#v", got, wantUnion)
	}

	s.RefreshWatchDirs = func() ([]string, error) { return nil, errors.New("unresolved import") }
	if got := s.refreshDependencyWatchDirs(current); !reflect.DeepEqual(got, current) {
		t.Fatalf("unresolved closure refresh = %#v, want preserved %#v", got, current)
	}

	s.RefreshWatchDirs = func() ([]string, error) { return []string{second}, nil }
	if got, want := s.refreshDependencyWatchDirs(current), []string{canonicalSecond}; !reflect.DeepEqual(got, want) {
		t.Fatalf("valid closure refresh = %#v, want replacement %#v", got, want)
	}
}

func TestRefreshDependencyWatchTargetsUpdatesExactFilesAtomically(t *testing.T) {
	root := t.TempDir()
	first := t.TempDir()
	second := t.TempDir()
	firstFile := filepath.Join(first, "physical", "first.gsx")
	secondFile := filepath.Join(second, "physical", "second.gsx")
	firstGo := filepath.Join(first, "bridge.go")
	secondGo := filepath.Join(second, "bridge.go")
	writeTestFile(t, firstFile, []byte(islandGSX))
	writeTestFile(t, secondFile, []byte(islandGSX))
	writeTestFile(t, firstGo, []byte("package first\n"))
	writeTestFile(t, secondGo, []byte("package second\n"))
	if err := os.Symlink(firstFile, filepath.Join(first, "first.gsx")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(secondFile, filepath.Join(second, "second.gsx")); err != nil {
		t.Fatal(err)
	}
	current, err := normalizeDependencyWatchTargets(root, []string{first}, []string{firstFile, firstGo}, []string{firstGo})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Dir:  root,
		Logf: func(string, ...any) {},
		RefreshWatchTargets: func() ([]string, []string, []string, error) {
			return []string{second}, []string{secondFile, secondGo}, []string{secondGo}, errors.New("invalid new source")
		},
	}
	partial := s.refreshDependencyWatchTargets(current)
	if got, want := partial.dirs, sortedStringUnion([]string{first}, []string{second}); !reflect.DeepEqual(got, want) {
		t.Fatalf("partial target dirs = %#v, want old+new %#v", got, want)
	}
	if got, want := partial.files, sortedStringUnion([]string{firstFile, firstGo}, []string{secondFile, secondGo}); !reflect.DeepEqual(got, want) {
		t.Fatalf("partial exact files = %#v, want old+new %#v", got, want)
	}
	if got, want := partial.goFiles, sortedStringUnion([]string{firstGo}, []string{secondGo}); !reflect.DeepEqual(got, want) {
		t.Fatalf("partial active Go files = %#v, want old+new %#v", got, want)
	}

	s.RefreshWatchTargets = func() ([]string, []string, []string, error) {
		return []string{second}, []string{secondFile, secondGo}, []string{secondGo}, nil
	}
	valid := s.refreshDependencyWatchTargets(current)
	if got, want := valid.dirs, []string{second}; !reflect.DeepEqual(got, want) {
		t.Fatalf("valid target dirs = %#v, want replacement %#v", got, want)
	}
	if got, want := valid.files, sortedStringUnion([]string{secondFile, secondGo}); !reflect.DeepEqual(got, want) {
		t.Fatalf("valid exact files = %#v, want replacement %#v", got, want)
	}
	if got, want := valid.goFiles, []string{secondGo}; !reflect.DeepEqual(got, want) {
		t.Fatalf("valid active Go files = %#v, want replacement %#v", got, want)
	}

	s.RefreshWatchTargets = func() ([]string, []string, []string, error) {
		return nil, nil, nil, errors.New("unresolved closure")
	}
	if got := s.refreshDependencyWatchTargets(current); !reflect.DeepEqual(got, current) {
		t.Fatalf("unresolved target refresh = %#v, want preserved %#v", got, current)
	}
}

func TestRefreshDependencyWatchTargetsRetainsInvalidPackageRootWithoutFollowingEscape(t *testing.T) {
	root := t.TempDir()
	previous := t.TempDir()
	invalid := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.gsx")
	writeTestFile(t, outside, []byte(islandGSX))
	logical := filepath.Join(invalid, "counter.gsx")
	if err := os.Symlink(outside, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	current, err := normalizeDependencyWatchTargets(root, []string{previous}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	s := &Server{
		Dir:  root,
		Logf: func(string, ...any) {},
		RefreshWatchTargets: func() ([]string, []string, []string, error) {
			return []string{invalid}, nil, nil, errors.New("source resolves outside package root")
		},
	}
	partial := s.refreshDependencyWatchTargets(current)
	if got, want := partial.dirs, sortedStringUnion([]string{previous}, []string{invalid}); !reflect.DeepEqual(got, want) {
		t.Fatalf("partial invalid roots = %#v, want safe old+new roots %#v", got, want)
	}
	if containsString(partial.files, outside) {
		t.Fatalf("escaping source entered exact allowlist: %#v", partial.files)
	}
	if !isWatchedSourceEventForTargets(root, partial, fsnotify.Event{Name: logical, Op: fsnotify.Write}) {
		t.Fatal("direct invalid logical source was not observable for safe retarget recovery")
	}

	inside := filepath.Join(invalid, "physical", "counter.gsx")
	writeTestFile(t, inside, []byte(islandGSX))
	frozenRetarget(t, logical, inside)
	s.RefreshWatchTargets = func() ([]string, []string, []string, error) {
		return []string{invalid}, []string{inside}, nil, nil
	}
	recovered := s.refreshDependencyWatchTargets(partial)
	if got, want := recovered.dirs, []string{invalid}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recovered roots = %#v, want authoritative replacement %#v", got, want)
	}
	if got, want := recovered.files, []string{inside}; !reflect.DeepEqual(got, want) {
		t.Fatalf("recovered exact files = %#v, want %#v", got, want)
	}
}

func TestReconcileDependencyWatchDirsReaddsKernelDroppedWatch(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(t.TempDir(), "dependency")
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}
	current, err := normalizeDependencyWatchDirs(root, []string{external})
	if err != nil {
		t.Fatal(err)
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close()
	if err := watcher.Add(external); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(external); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(2 * time.Second)
	for len(watcher.WatchList()) != 0 {
		select {
		case <-watcher.Events:
		case watchErr := <-watcher.Errors:
			t.Fatalf("watcher error while removing dependency: %v", watchErr)
		case <-deadline:
			t.Fatalf("kernel did not drop removed watch; watch list=%v", watcher.WatchList())
		}
	}
	if err := os.MkdirAll(external, 0o755); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		Dir:  root,
		Logf: func(string, ...any) {},
		RefreshWatchDirs: func() ([]string, error) {
			return []string{external}, nil
		},
	}
	got := s.reconcileDependencyWatchDirs(watcher, current)
	if !reflect.DeepEqual(got, current) {
		t.Fatalf("reconciled logical membership = %v, want %v", got, current)
	}
	if len(watcher.WatchList()) == 0 {
		t.Fatal("reconciliation did not restore the kernel-dropped dependency watch")
	}
}

func TestFSNotifyDependencyRemovalRecreationEditRecoversQuarantine(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("directory self-removal watch semantics are verified on Linux")
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := filepath.Join(t.TempDir(), "dependency")
	writeTestFile(t, filepath.Join(external, "counter.gsx"), []byte(islandGSX))

	removed := make(chan struct{}, 1)
	recovered := make(chan struct{}, 1)
	var restarts atomic.Int32
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		PollInterval: 10 * time.Millisecond,
		Logf:         func(string, ...any) {},
		RefreshWatchDirs: func() ([]string, error) {
			return []string{external}, nil
		},
		PreflightChange: func([]string) error {
			if _, err := os.Stat(external); err != nil {
				select {
				case removed <- struct{}{}:
				default:
				}
				return fmt.Errorf("dependency unavailable: %w", err)
			}
			return nil
		},
		OnChange: func() error {
			if restarts.Add(1) == 1 {
				recovered <- struct{}{}
			}
			return nil
		},
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.watchWithFSNotify(stop)
	}()
	defer func() {
		close(stop)
		<-done
	}()

	time.Sleep(50 * time.Millisecond)
	if err := os.RemoveAll(external); err != nil {
		t.Fatal(err)
	}
	select {
	case <-removed:
	case <-time.After(2 * time.Second):
		t.Fatal("dependency removal did not enter quarantine preflight")
	}
	deadline := time.Now().Add(2 * time.Second)
	for !s.isQuarantined() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !s.isQuarantined() {
		t.Fatal("dependency removal did not quarantine the server")
	}

	writeTestFile(t, filepath.Join(external, "counter.gsx"), []byte(islandGSX))
	for attempt := 0; attempt < 20; attempt++ {
		select {
		case <-recovered:
			if s.isQuarantined() {
				t.Fatal("recovery restart left the server quarantined")
			}
			return
		default:
		}
		changed := []byte(islandGSX + fmt.Sprintf("\n// recovery edit %d\n", attempt))
		writeTestFile(t, filepath.Join(external, "counter.gsx"), changed)
		// Leave a full debounce window between writes. Rapid writes are
		// intentionally coalesced; this fixture is proving that at least one
		// post-recreation edit is delivered after the kernel watch is repaired.
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("recreated dependency edits were not observed; restarts=%d", restarts.Load())
}

func TestNestedDependencySymlinkFSNotifyLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fsnotify directory replacement and repair semantics are verified on Linux")
	}
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := t.TempDir()
	nested := filepath.Join(external, "physical")
	physical := filepath.Join(nested, "counter.gsx")
	sibling := filepath.Join(nested, "unrelated.gsx")
	writeTestFile(t, physical, []byte(islandGSX))
	writeTestFile(t, sibling, []byte(islandGSX))
	logical := filepath.Join(external, "counter.gsx")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	changes := make(chan []string, 64)
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		WatchFiles:   []string{physical},
		PollInterval: 10 * time.Millisecond,
		Logf:         func(string, ...any) {},
		PreflightChange: func(paths []string) error {
			changes <- append([]string(nil), paths...)
			return nil
		},
		OnChange: func() error { return nil },
	}
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- s.watchWithFSNotify(stop) }()
	stopped := false
	stopWatcher := func() {
		if stopped {
			return
		}
		stopped = true
		close(stop)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("watchWithFSNotify: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("fsnotify watcher did not stop")
		}
	}
	defer stopWatcher()

	time.Sleep(100 * time.Millisecond)
	writeTestFile(t, sibling, []byte(islandGSX+"\n// unrelated nested sibling\n"))
	assertNoNestedWatchChange(t, changes, 150*time.Millisecond, "unrelated nested sibling")

	writeTestFile(t, physical, []byte(islandGSX+"\n// canonical nested edit\n"))
	awaitNestedWatchChange(t, changes, physical, "canonical nested edit")
	drainNestedWatchChanges(changes)

	beforeReplace, err := os.Stat(physical)
	if err != nil {
		t.Fatal(err)
	}
	replacement := physical + ".replacement"
	replacementData := []byte(islandGSX + "\n// canonical nested edit\n")
	replacementData[len(replacementData)-2] = 'x'
	writeTestFile(t, replacement, replacementData)
	if err := os.Chtimes(replacement, beforeReplace.ModTime(), beforeReplace.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, physical); err != nil {
		t.Fatal(err)
	}
	awaitNestedWatchChange(t, changes, physical, "atomic replacement")
	drainNestedWatchChanges(changes)

	if err := os.Remove(physical); err != nil {
		t.Fatal(err)
	}
	awaitNestedWatchChange(t, changes, physical, "physical target removal")
	drainNestedWatchChanges(changes)
	writeTestFile(t, physical, []byte(islandGSX+"\n// target recreation\n"))
	awaitNestedWatchChange(t, changes, physical, "physical target recreation")
	drainNestedWatchChanges(changes)

	if err := os.RemoveAll(nested); err != nil {
		t.Fatal(err)
	}
	awaitNestedWatchChange(t, changes, physical, "exact parent removal")
	drainNestedWatchChanges(changes)
	writeTestFile(t, physical, []byte(islandGSX+"\n// parent recreation\n"))
	for attempt := 0; attempt < 20; attempt++ {
		writeTestFile(t, physical, []byte(islandGSX+fmt.Sprintf("\n// repaired nested watch %d\n", attempt)))
		if awaitOptionalNestedWatchChange(changes, physical, 125*time.Millisecond) {
			break
		}
		if attempt == 19 {
			t.Fatal("fsnotify did not repair the exact nested parent watch after recreation")
		}
	}

	stopWatcher()
	drainNestedWatchChanges(changes)
	writeTestFile(t, physical, []byte(islandGSX+"\n// edit after shutdown\n"))
	assertNoNestedWatchChange(t, changes, 150*time.Millisecond, "edit after watcher shutdown")
}

func TestNestedDependencySymlinkPollingLifecycle(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := t.TempDir()
	nested := filepath.Join(external, "physical")
	physical := filepath.Join(nested, "counter.gsx")
	sibling := filepath.Join(nested, "unrelated.gsx")
	writeTestFile(t, physical, []byte(islandGSX))
	writeTestFile(t, sibling, []byte(islandGSX))
	logical := filepath.Join(external, "counter.gsx")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	changes := make(chan []string, 64)
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		WatchFiles:   []string{physical},
		PollInterval: 10 * time.Millisecond,
		Logf:         func(string, ...any) {},
		PreflightChange: func(paths []string) error {
			changes <- append([]string(nil), paths...)
			return nil
		},
		OnChange: func() error { return nil },
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchWithPolling(stop)
	}()
	stopped := false
	stopWatcher := func() {
		if stopped {
			return
		}
		stopped = true
		close(stop)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("polling watcher did not stop")
		}
	}
	defer stopWatcher()

	time.Sleep(50 * time.Millisecond)
	writeTestFile(t, sibling, []byte(islandGSX+"\n// unrelated nested sibling\n"))
	assertNoNestedWatchChange(t, changes, 75*time.Millisecond, "unrelated nested sibling")

	writeTestFile(t, physical, []byte(islandGSX+"\n// canonical nested edit\n"))
	awaitNestedWatchChange(t, changes, physical, "canonical nested edit")
	drainNestedWatchChanges(changes)

	beforeReplace, err := os.Stat(physical)
	if err != nil {
		t.Fatal(err)
	}
	replacement := physical + ".replacement"
	replacementData := []byte(islandGSX + "\n// canonical nested edit\n")
	replacementData[len(replacementData)-2] = 'x'
	writeTestFile(t, replacement, replacementData)
	if err := os.Chtimes(replacement, beforeReplace.ModTime(), beforeReplace.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, physical); err != nil {
		t.Fatal(err)
	}
	awaitNestedWatchChange(t, changes, physical, "atomic replacement")
	drainNestedWatchChanges(changes)

	if err := os.Remove(logical); err != nil {
		t.Fatal(err)
	}
	awaitNestedWatchChange(t, changes, physical, "logical symlink removal")
	drainNestedWatchChanges(changes)
	if err := os.Symlink(physical, logical); err != nil {
		t.Fatal(err)
	}
	awaitNestedWatchChange(t, changes, physical, "logical symlink recreation")
	drainNestedWatchChanges(changes)

	if err := os.RemoveAll(nested); err != nil {
		t.Fatal(err)
	}
	awaitNestedWatchChange(t, changes, physical, "physical parent removal")
	drainNestedWatchChanges(changes)
	writeTestFile(t, physical, []byte(islandGSX+"\n// physical parent recreation\n"))
	awaitNestedWatchChange(t, changes, physical, "physical parent recreation")

	stopWatcher()
	drainNestedWatchChanges(changes)
	writeTestFile(t, physical, []byte(islandGSX+"\n// edit after shutdown\n"))
	assertNoNestedWatchChange(t, changes, 75*time.Millisecond, "edit after watcher shutdown")
}

func awaitNestedWatchChange(t *testing.T, changes <-chan []string, want, label string) {
	t.Helper()
	if !awaitOptionalNestedWatchChange(changes, want, 2*time.Second) {
		t.Fatalf("watcher did not report %s at exact source %s", label, want)
	}
}

func awaitOptionalNestedWatchChange(changes <-chan []string, want string, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case paths := <-changes:
			if containsString(paths, want) {
				return true
			}
		case <-deadline.C:
			return false
		}
	}
}

func assertNoNestedWatchChange(t *testing.T, changes <-chan []string, timeout time.Duration, label string) {
	t.Helper()
	select {
	case paths := <-changes:
		t.Fatalf("watcher reported %s: %#v", label, paths)
	case <-time.After(timeout):
	}
}

func drainNestedWatchChanges(changes <-chan []string) {
	for {
		select {
		case <-changes:
		default:
			return
		}
	}
}

func TestPollingQueuesMutationArrivingDuringOnChange(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "main.go")
	writeTestFile(t, path, []byte("package main\n"))
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	s := &Server{
		Dir:          root,
		PollInterval: 10 * time.Millisecond,
		Logf:         func(string, ...any) {},
		OnChange: func() error {
			if calls.Add(1) == 1 {
				started <- struct{}{}
				<-release
			}
			return nil
		},
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.watchWithPolling(stop)
	}()
	defer func() {
		close(stop)
		unblock()
		<-done
	}()

	time.Sleep(40 * time.Millisecond)
	writeTestFile(t, path, []byte("package main\n// first\n"))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("polling watcher did not observe first change")
	}
	writeTestFile(t, path, []byte("package main\n// second mutation while rebuilding\n"))
	unblock()
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("polling watcher absorbed a mutation during OnChange; calls=%d, want at least 2", got)
	}
}

func TestIsWatchedSourceEventAllowsDirectGSXAndOnlySelectedGo(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	unrelated := t.TempDir()
	gsx := filepath.Join(external, "island.gsx")
	goSource := filepath.Join(external, "bridge.go")
	inactiveGo := filepath.Join(external, "inactive.go")
	readme := filepath.Join(external, "README.md")
	nested := filepath.Join(external, "nested", "island.gsx")
	other := filepath.Join(unrelated, "island.gsx")
	for path, data := range map[string][]byte{
		gsx:        []byte("package external"),
		goSource:   []byte("package external"),
		inactiveGo: []byte("package external"),
		readme:     []byte("ignored"),
		nested:     []byte("package nested"),
		other:      []byte("package unrelated"),
	} {
		writeTestFile(t, path, data)
	}
	targets, err := normalizeDependencyWatchTargets(root, []string{external}, []string{goSource}, []string{goSource})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{gsx, goSource} {
		if !isWatchedSourceEventForTargets(root, targets, fsnotify.Event{Name: path, Op: fsnotify.Write}) {
			t.Fatalf("allowlisted dependency source %s was rejected", path)
		}
	}
	if !isWatchedSourceEventForTargets(root, targets, fsnotify.Event{Name: external, Op: fsnotify.Remove}) {
		t.Fatal("removing an allowlisted dependency package directory was rejected")
	}
	for _, event := range []fsnotify.Event{
		{Name: inactiveGo, Op: fsnotify.Write},
		{Name: readme, Op: fsnotify.Write},
		{Name: nested, Op: fsnotify.Write},
		{Name: other, Op: fsnotify.Write},
		{Name: gsx, Op: fsnotify.Chmod},
	} {
		if isWatchedSourceEventForTargets(root, targets, event) {
			t.Fatalf("unexpected external event accepted: %#v", event)
		}
	}
}

func TestWatchedSourceSnapshotReportsExternalChangesDeterministically(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	rootSource := filepath.Join(root, "z.go")
	externalGo := filepath.Join(external, "a.go")
	externalGSX := filepath.Join(external, "z.gsx")
	writeTestFile(t, rootSource, []byte("package main\n"))
	writeTestFile(t, externalGo, []byte("package external\n"))
	writeTestFile(t, externalGSX, []byte("package external\n"))
	targets, err := normalizeDependencyWatchTargets(root, []string{external}, []string{externalGo}, []string{externalGo})
	if err != nil {
		t.Fatal(err)
	}
	before, err := watchedSourceSnapshotForTargets(root, targets)
	if err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, externalGSX, []byte("package external\n// changed\n"))
	if err := os.Remove(externalGo); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(external, "README.md"), []byte("ignored"))
	after, err := watchedSourceSnapshotForTargets(root, targets)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{externalGo, externalGSX}
	if got := changedWatchedPaths(before, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("changed watched paths = %#v, want sorted %#v", got, want)
	}
	if again := changedWatchedPaths(before, after); !reflect.DeepEqual(again, want) {
		t.Fatalf("changed watched path order changed across runs: %#v", again)
	}
}

func TestWatchedSourceSnapshotRejectsDependencySymlinkEscape(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "outside.gsx")
	writeTestFile(t, outside, []byte("package outside"))
	if err := os.Symlink(outside, filepath.Join(external, "escape.gsx")); err != nil {
		t.Fatal(err)
	}
	dirs, err := normalizeDependencyWatchDirs(root, []string{external})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := watchedSourceSnapshot(root, dirs); err == nil || !strings.Contains(err.Error(), "resolves outside allowlisted") {
		t.Fatalf("dependency symlink escape snapshot error = %v", err)
	}
}

func TestShouldWatchProjectFile(t *testing.T) {
	for _, path := range []string{"page.gsx", "main.go", "style.CSS", "app.JS"} {
		if !shouldWatchProjectFile(path) {
			t.Fatalf("%s should be watched", path)
		}
	}
	for _, path := range []string{"README.md", "data.json", "image.png"} {
		if shouldWatchProjectFile(path) {
			t.Fatalf("%s should not be watched", path)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func frozenWriteDev(t *testing.T, filename string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func frozenRetarget(t *testing.T, logical, target string) {
	t.Helper()
	temporary := logical + ".next"
	if err := os.Symlink(target, temporary); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temporary, logical); err != nil {
		t.Fatal(err)
	}
}

func frozenStopWatcher(t *testing.T, stop chan struct{}, done <-chan struct{}) {
	t.Helper()
	close(stop)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher did not stop")
	}
}

func TestFrozenReviewerPureIslandNestedSourceRunsPreflightWithoutRestart(t *testing.T) {
	root := t.TempDir()
	frozenWriteDev(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := t.TempDir()
	physical := filepath.Join(external, "physical", "counter.gsx")
	frozenWriteDev(t, physical, []byte(islandGSX))
	logical := filepath.Join(external, "counter.gsx")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	preflight := make(chan []string, 8)
	var restarts atomic.Int32
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		WatchFiles:   []string{physical},
		PollInterval: 10 * time.Millisecond,
		Logf:         func(string, ...any) {},
		PreflightChange: func(paths []string) error {
			preflight <- append([]string(nil), paths...)
			return nil
		},
		OnChange: func() error { restarts.Add(1); return nil },
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.watchWithFSNotify(stop); err != nil {
			t.Errorf("watchWithFSNotify: %v", err)
		}
	}()
	defer frozenStopWatcher(t, stop, done)

	time.Sleep(75 * time.Millisecond)
	frozenWriteDev(t, physical, []byte(islandGSX+"\n// physical edit\n"))
	select {
	case paths := <-preflight:
		found := false
		for _, changed := range paths {
			if filepath.Clean(changed) == filepath.Clean(physical) {
				found = true
			}
		}
		if !found {
			t.Fatalf("preflight paths=%#v, want exact physical source %q", paths, physical)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pure-island nested edit never reached global preflight")
	}
	time.Sleep(150 * time.Millisecond)
	if got := restarts.Load(); got != 0 {
		t.Fatalf("pure island edit restarted server %d times; hot swap should deliver only preflight/program", got)
	}
}

func TestFrozenReviewerPollingHandlesValidNestedSymlinkRetarget(t *testing.T) {
	root := t.TempDir()
	frozenWriteDev(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := t.TempDir()
	first := filepath.Join(external, "first", "counter.gsx")
	second := filepath.Join(external, "second", "counter.gsx")
	frozenWriteDev(t, first, []byte(islandGSX))
	frozenWriteDev(t, second, []byte(islandGSX+"\n// second target\n"))
	logical := filepath.Join(external, "counter.gsx")
	if err := os.Symlink(first, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	preflight := make(chan []string, 8)
	var logMu sync.Mutex
	var logs []string
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		WatchFiles:   []string{first},
		PollInterval: 10 * time.Millisecond,
		Logf: func(format string, args ...any) {
			logMu.Lock()
			if len(logs) < 5 {
				logs = append(logs, fmt.Sprintf(format, args...))
			}
			logMu.Unlock()
		},
		PreflightChange: func(paths []string) error {
			preflight <- append([]string(nil), paths...)
			return nil
		},
		OnChange: func() error { return nil },
		RefreshWatchTargets: func() ([]string, []string, []string, error) {
			current, err := filepath.EvalSymlinks(logical)
			if err != nil {
				return []string{external}, nil, nil, err
			}
			return []string{external}, []string{current}, nil, nil
		},
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() { defer close(done); s.watchWithPolling(stop) }()
	defer frozenStopWatcher(t, stop, done)

	time.Sleep(50 * time.Millisecond)
	frozenRetarget(t, logical, second)
	select {
	case paths := <-preflight:
		found := false
		for _, changed := range paths {
			if filepath.Clean(changed) == filepath.Clean(second) || filepath.Clean(changed) == filepath.Clean(first) {
				found = true
			}
		}
		if !found {
			t.Fatalf("retarget preflight paths=%#v, want old or new exact target", paths)
		}
	case <-time.After(750 * time.Millisecond):
		logMu.Lock()
		joined := strings.Join(logs, "\n")
		logMu.Unlock()
		t.Fatalf("valid in-package symlink retarget never reached preflight/refresh; polling remained pinned to the old exact allowlist; logs:\n%s", joined)
	}
}

func TestFrozenReviewerFSNotifyQueuesEditToNewTargetDuringRetargetRebuild(t *testing.T) {
	root := t.TempDir()
	frozenWriteDev(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := t.TempDir()
	first := filepath.Join(external, "first", "page.gsx")
	second := filepath.Join(external, "second", "page.gsx")
	serverSource := []byte("package dep\n\nfunc Page() Node {\n\treturn <main>one</main>\n}\n")
	frozenWriteDev(t, first, serverSource)
	frozenWriteDev(t, second, []byte("package dep\n\nfunc Page() Node {\n\treturn <main>two</main>\n}\n"))
	logical := filepath.Join(external, "page.gsx")
	if err := os.Symlink(first, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	var calls atomic.Int32
	preflight := make(chan []string, 8)
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		WatchFiles:   []string{first},
		PollInterval: 10 * time.Millisecond,
		Logf: func(format string, args ...any) {
			t.Logf(format, args...)
		},
		PreflightChange: func(paths []string) error {
			preflight <- append([]string(nil), paths...)
			return nil
		},
		OnChange: func() error {
			if calls.Add(1) == 1 {
				started <- struct{}{}
				<-release
			}
			return nil
		},
		RefreshWatchTargets: func() ([]string, []string, []string, error) {
			current, err := filepath.EvalSymlinks(logical)
			if err != nil {
				return []string{external}, nil, nil, err
			}
			return []string{external}, []string{current}, nil, nil
		},
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.watchWithFSNotify(stop); err != nil {
			t.Errorf("watchWithFSNotify: %v", err)
		}
	}()
	defer func() { unblock(); frozenStopWatcher(t, stop, done) }()

	time.Sleep(75 * time.Millisecond)
	frozenRetarget(t, logical, second)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		select {
		case paths := <-preflight:
			t.Fatalf("symlink retarget reached preflight at %#v but did not start full rebuild", paths)
		default:
			t.Fatal("symlink retarget did not reach preflight or start full rebuild")
		}
	}
	frozenWriteDev(t, second, []byte("package dep\n\nfunc Page() Node {\n\treturn <main>edited during rebuild</main>\n}\n"))
	unblock()
	deadline := time.Now().Add(1 * time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("edit to newly retargeted physical source during blocked rebuild was missed before its parent watch was installed; rebuild calls=%d", got)
	}
}

func TestFrozenReviewerFSNotifyObservesNestedHardlinkAliasMutation(t *testing.T) {
	root := t.TempDir()
	frozenWriteDev(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := t.TempDir()
	logical := filepath.Join(external, "counter.gsx")
	frozenWriteDev(t, logical, []byte(islandGSX))
	alias := filepath.Join(external, "physical", "counter-alias.gsx")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(logical, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}

	preflight := make(chan []string, 8)
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		WatchFiles:   []string{logical},
		PollInterval: 10 * time.Millisecond,
		Logf:         func(string, ...any) {},
		PreflightChange: func(paths []string) error {
			preflight <- append([]string(nil), paths...)
			return nil
		},
		OnChange: func() error { return nil },
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := s.watchWithFSNotify(stop); err != nil {
			t.Errorf("watchWithFSNotify: %v", err)
		}
	}()
	defer frozenStopWatcher(t, stop, done)

	time.Sleep(75 * time.Millisecond)
	frozenWriteDev(t, alias, []byte(islandGSX+"\n// nested hardlink mutation\n"))
	select {
	case <-preflight:
	case <-time.After(750 * time.Millisecond):
		t.Fatal("nested hardlink alias changed the discovery-authoritative inode, but fsnotify watched only the top-level directory entry and missed the mutation")
	}
}

func TestDependencySnapshotDetectsHardlinkAliasSameMetadataMutation(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	logical := filepath.Join(external, "counter.gsx")
	original := []byte(islandGSX)
	frozenWriteDev(t, logical, original)
	alias := filepath.Join(external, "physical", "counter-alias.gsx")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(logical, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	targets, err := normalizeDependencyWatchTargets(root, []string{external}, []string{logical}, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, err := dependencySourceSnapshotForTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(logical)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte(strings.Replace(string(original), "Counter", "Changed", 1))
	if len(replacement) != len(original) {
		t.Fatalf("same-size hardlink fixture changed length: %d != %d", len(replacement), len(original))
	}
	frozenWriteDev(t, alias, replacement)
	if err := os.Chtimes(alias, info.ModTime(), info.ModTime()); err != nil {
		t.Skipf("cannot restore hardlink timestamps: %v", err)
	}
	after, err := dependencySourceSnapshotForTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := changedWatchedPaths(before, after), []string{logical}; !reflect.DeepEqual(got, want) {
		t.Fatalf("same-size/same-mtime hardlink mutation changes = %#v, want authoritative source %#v", got, want)
	}
}

func TestDependencySnapshotDetectsGoHardlinkAliasSameMetadataMutation(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	logical := filepath.Join(external, "bridge.go")
	original := []byte("package dep\n\nvar Bridge = \"old\"\n")
	frozenWriteDev(t, logical, original)
	alias := filepath.Join(external, "physical", "bridge-alias.go")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(logical, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	targets, err := normalizeDependencyWatchTargets(root, []string{external}, []string{logical}, []string{logical})
	if err != nil {
		t.Fatal(err)
	}
	before, err := dependencySourceSnapshotForTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(logical)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte("package dep\n\nvar Bridge = \"new\"\n")
	if len(replacement) != len(original) {
		t.Fatalf("same-size hardlink fixture changed length: %d != %d", len(replacement), len(original))
	}
	frozenWriteDev(t, alias, replacement)
	if err := os.Chtimes(alias, info.ModTime(), info.ModTime()); err != nil {
		t.Skipf("cannot restore hardlink timestamps: %v", err)
	}
	after, err := dependencySourceSnapshotForTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := changedWatchedPaths(before, after), []string{logical}; !reflect.DeepEqual(got, want) {
		t.Fatalf("same-size/same-mtime Go hardlink mutation changes = %#v, want authoritative source %#v", got, want)
	}
}

func TestDependencySnapshotCanonicalCollisionRetainsContentHash(t *testing.T) {
	root := t.TempDir()
	external := t.TempDir()
	physical := filepath.Join(external, "physical", "source")
	content := []byte("package dep\n")
	frozenWriteDev(t, physical, content)
	gsxLogical := filepath.Join(external, "component.gsx")
	goLogical := filepath.Join(external, "bridge.go")
	if err := os.Symlink(physical, gsxLogical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(physical, goLogical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	targets, err := normalizeDependencyWatchTargets(root, []string{external}, []string{physical}, []string{physical})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := dependencySourceSnapshotForTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := snapshot[physical]
	if !ok {
		t.Fatalf("canonical collision snapshot = %#v, want physical source %q", snapshot, physical)
	}
	if !entry.HasContentHash || entry.ContentHash != sha256.Sum256(content) {
		t.Fatalf("canonical collision downgraded content identity: %#v", entry)
	}
}

func TestFSNotifyObservesGoHardlinkAliasSameMetadataMutation(t *testing.T) {
	runGoHardlinkAliasSameMetadataWatcherTest(t, false)
}

func TestPollingObservesGoHardlinkAliasSameMetadataMutation(t *testing.T) {
	runGoHardlinkAliasSameMetadataWatcherTest(t, true)
}

func runGoHardlinkAliasSameMetadataWatcherTest(t *testing.T, polling bool) {
	t.Helper()
	root := t.TempDir()
	frozenWriteDev(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := t.TempDir()
	logical := filepath.Join(external, "bridge.go")
	original := []byte("package dep\n\nvar Bridge = \"old\"\n")
	frozenWriteDev(t, logical, original)
	alias := filepath.Join(external, "physical", "bridge-alias.go")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(logical, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	info, err := os.Stat(logical)
	if err != nil {
		t.Fatal(err)
	}
	preflight := make(chan []string, 1)
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		WatchFiles:   []string{logical},
		WatchGoFiles: []string{logical},
		PollInterval: 10 * time.Millisecond,
		Logf:         func(string, ...any) {},
		PreflightChange: func(paths []string) error {
			preflight <- append([]string(nil), paths...)
			return nil
		},
		OnChange: func() error { return nil },
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if polling {
			s.watchWithPolling(stop)
			return
		}
		if err := s.watchWithFSNotify(stop); err != nil {
			t.Errorf("watchWithFSNotify: %v", err)
		}
	}()
	defer frozenStopWatcher(t, stop, done)

	time.Sleep(75 * time.Millisecond)
	replacement := []byte("package dep\n\nvar Bridge = \"new\"\n")
	if len(replacement) != len(original) {
		t.Fatalf("same-size hardlink fixture changed length: %d != %d", len(replacement), len(original))
	}
	frozenWriteDev(t, alias, replacement)
	if err := os.Chtimes(alias, info.ModTime(), info.ModTime()); err != nil {
		t.Skipf("cannot restore hardlink timestamps: %v", err)
	}
	select {
	case got := <-preflight:
		if want := []string{logical}; !reflect.DeepEqual(got, want) {
			t.Fatalf("same-size/same-mtime Go hardlink mutation paths = %#v, want authoritative source %#v", got, want)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("same-size/same-mtime mutation through nested Go hardlink alias was not delivered")
	}
}

func TestDependencySnapshotAdmitsOnlySelectedActiveGoAndDirectGSX(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fixture pins Linux active/inactive source names")
	}
	root := t.TempDir()
	external := t.TempDir()
	activeGo := filepath.Join(external, "bridge_linux.go")
	inactiveGo := filepath.Join(external, "bridge_windows.go")
	directGSX := filepath.Join(external, "counter.gsx")
	nestedGo := filepath.Join(external, "nested", "neighbor.go")
	nestedGSX := filepath.Join(external, "nested", "neighbor.gsx")
	unrelated := filepath.Join(external, "notes.txt")
	activeBytes := []byte("//go:build linux\n\npackage dep\n")
	gsxBytes := []byte(islandGSX)
	frozenWriteDev(t, activeGo, activeBytes)
	frozenWriteDev(t, inactiveGo, []byte("//go:build windows\n\npackage dep\n"))
	frozenWriteDev(t, directGSX, gsxBytes)
	frozenWriteDev(t, nestedGo, []byte("package dep\n"))
	frozenWriteDev(t, nestedGSX, []byte("package dep\n"))
	frozenWriteDev(t, unrelated, []byte("not source\n"))

	targets, err := normalizeDependencyWatchTargets(
		root,
		[]string{external},
		[]string{directGSX, activeGo},
		[]string{activeGo},
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := dependencySourceSnapshotForTargets(targets)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 2 {
		t.Fatalf("dependency snapshot = %#v, want only active Go and direct GSX", snapshot)
	}
	for path, hash := range map[string][sha256.Size]byte{
		activeGo:  sha256.Sum256(activeBytes),
		directGSX: sha256.Sum256(gsxBytes),
	} {
		entry, ok := snapshot[path]
		if !ok || !entry.HasContentHash || entry.ContentHash != hash {
			t.Fatalf("admitted source %q snapshot = %#v, want content hash %x", path, entry, hash)
		}
	}
	for _, path := range []string{inactiveGo, nestedGo, nestedGSX, unrelated} {
		if _, ok := snapshot[path]; ok {
			t.Fatalf("non-input source %q entered dependency snapshot: %#v", path, snapshot)
		}
	}
}

func TestFSNotifyActiveGoSelectionBoundary(t *testing.T) {
	runActiveGoSelectionWatcherTest(t, false)
}

func TestPollingActiveGoSelectionBoundary(t *testing.T) {
	runActiveGoSelectionWatcherTest(t, true)
}

func runActiveGoSelectionWatcherTest(t *testing.T, polling bool) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("fixture pins Linux active/inactive source names")
	}
	root := t.TempDir()
	frozenWriteDev(t, filepath.Join(root, "main.go"), []byte("package main\n"))
	external := t.TempDir()
	activeGo := filepath.Join(external, "bridge_linux.go")
	inactiveGo := filepath.Join(external, "bridge_windows.go")
	frozenWriteDev(t, activeGo, []byte("//go:build linux\n\npackage dep\n"))
	frozenWriteDev(t, inactiveGo, []byte("//go:build windows\n\npackage dep\n"))

	var selectionMu sync.RWMutex
	selected := []string{activeGo}
	currentSelection := func() []string {
		selectionMu.RLock()
		defer selectionMu.RUnlock()
		return append([]string(nil), selected...)
	}
	preflight := make(chan []string, 16)
	s := &Server{
		Dir:          root,
		WatchDirs:    []string{external},
		WatchFiles:   []string{activeGo},
		WatchGoFiles: []string{activeGo},
		PollInterval: 10 * time.Millisecond,
		Logf:         func(string, ...any) {},
		PreflightChange: func(paths []string) error {
			preflight <- append([]string(nil), paths...)
			return nil
		},
		OnChange: func() error { return nil },
		RefreshWatchTargets: func() ([]string, []string, []string, error) {
			active := currentSelection()
			return []string{external}, active, active, nil
		},
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if polling {
			s.watchWithPolling(stop)
			return
		}
		if err := s.watchWithFSNotify(stop); err != nil {
			t.Errorf("watchWithFSNotify: %v", err)
		}
	}()
	defer frozenStopWatcher(t, stop, done)

	time.Sleep(75 * time.Millisecond)
	frozenWriteDev(t, inactiveGo, []byte("//go:build windows\n\npackage dep\n// inactive edit\n"))
	select {
	case got := <-preflight:
		t.Fatalf("inactive direct Go edit reached preflight: %#v", got)
	case <-time.After(250 * time.Millisecond):
	}

	frozenWriteDev(t, activeGo, []byte("//go:build linux\n\npackage dep\n// active edit\n"))
	assertWatchPreflightContains(t, preflight, activeGo, "active Go edit")
	drainWatchPreflight(preflight)

	selectionMu.Lock()
	selected = []string{activeGo, inactiveGo}
	selectionMu.Unlock()
	frozenWriteDev(t, inactiveGo, []byte("//go:build linux\n\npackage dep\n// activated\n"))
	assertWatchPreflightContains(t, preflight, inactiveGo, "inactive-to-active Go transition")
}

func assertWatchPreflightContains(t *testing.T, changes <-chan []string, want, label string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case paths := <-changes:
			for _, path := range paths {
				if filepath.Clean(path) == filepath.Clean(want) {
					return
				}
			}
		case <-deadline:
			t.Fatalf("%s never reached preflight for %q", label, want)
		}
	}
}

func drainWatchPreflight(changes <-chan []string) {
	for {
		select {
		case <-changes:
		default:
			return
		}
	}
}
