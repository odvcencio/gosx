package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

// TestCollectProjectIslandProgramsSourceHashDetectsSourceChange exercises the
// same discovery function RunBuildWithOptions calls to populate
// IslandAsset.SourceFile/SourceHash (issue #166). It builds a manifest from
// the real compiler pipeline, confirms the staleness check reports nothing
// for unchanged source, mutates the island's .gsx file, and confirms the
// staleness check now reports it — plus a back-compat manifest entry that
// predates the field, which must never be reported.
func TestCollectProjectIslandProgramsSourceHashDetectsSourceChange(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	programs, _, err := collectProjectIslandPrograms(dir)
	if err != nil {
		t.Fatalf("collectProjectIslandPrograms: %v", err)
	}
	if len(programs) != 1 || programs[0].Name != "Counter" {
		t.Fatalf("collectProjectIslandPrograms = %v, want one Counter island", programs)
	}
	prog := programs[0]
	if prog.SourceFile != filepath.Join(dir, "counter.gsx") {
		t.Fatalf("SourceFile = %q, want %q", prog.SourceFile, filepath.Join(dir, "counter.gsx"))
	}
	if prog.SourceHash == "" {
		t.Fatal("SourceHash is empty")
	}

	rel, err := filepath.Rel(dir, prog.SourceFile)
	if err != nil {
		t.Fatalf("relativize source path: %v", err)
	}
	manifest := &buildmanifest.Manifest{Islands: []buildmanifest.IslandAsset{
		{
			Name:        prog.Name,
			Format:      "bin",
			HashedAsset: buildmanifest.HashedAsset{File: "Counter.aaaaaaaa.gxi", Hash: "aaaaaaaa", Size: 1},
			SourceFile:  filepath.ToSlash(rel),
			SourceHash:  prog.SourceHash,
		},
		// A pre-#166 manifest entry: no SourceFile/SourceHash. Must never be
		// reported, regardless of what its dist program hash claims.
		{
			Name:        "LegacyIsland",
			Format:      "bin",
			HashedAsset: buildmanifest.HashedAsset{File: "LegacyIsland.bbbbbbbb.gxi", Hash: "bbbbbbbb", Size: 1},
		},
	}}

	if stale := manifest.StaleIslands(dir); len(stale) != 0 {
		t.Fatalf("unchanged island reported stale: %v", stale)
	}

	// Re-run the pipeline after the source changes, mirroring what happens
	// between `gosx build` and a later `go run`: only the .gsx source
	// mutates, the manifest on disk (and thus SourceHash) does not.
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 2) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	stale := manifest.StaleIslands(dir)
	if len(stale) != 1 || stale[0].Name != "Counter" {
		t.Fatalf("changed island stale report = %+v, want [Counter]", stale)
	}
}

func TestCollectProjectIslandProgramsFollowsTransitiveImportsDeterministically(t *testing.T) {
	appDir := newTransitiveIslandProject(t)

	programs, files, err := collectProjectIslandPrograms(appDir)
	if err != nil {
		t.Fatalf("collectProjectIslandPrograms: %v", err)
	}
	wantPrograms := []string{
		"example.com/leaf#LeafIsland",
		"example.com/middle#MiddleIsland",
	}
	if got := islandProgramIdentities(programs); !reflect.DeepEqual(got, wantPrograms) {
		t.Fatalf("program identities = %#v, want %#v", got, wantPrograms)
	}
	if len(files) != 3 {
		t.Fatalf("discovered files = %#v, want app, middle, and transitive leaf files", files)
	}
	for _, suffix := range []string{"app/app/page.gsx", "middle/island.gsx", "leaf/island.gsx"} {
		if !hasPathSuffix(files, suffix) {
			t.Fatalf("discovered files = %#v, missing %q", files, suffix)
		}
	}

	againPrograms, againFiles, err := collectProjectIslandPrograms(appDir)
	if err != nil {
		t.Fatalf("second collectProjectIslandPrograms: %v", err)
	}
	if got, want := islandProgramIdentities(againPrograms), islandProgramIdentities(programs); !reflect.DeepEqual(got, want) {
		t.Fatalf("second program order = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(againFiles, files) {
		t.Fatalf("second file order = %#v, want %#v", againFiles, files)
	}
}

func newDiamondCycleIslandRoot(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	leafDir := writeIslandModule(t, root, "leaf", "example.com/leaf", "leaf", `package leaf

//gosx:island
component LeafIsland() {
	value := signal.New(0)
	return <span>{value.Get()}</span>
}
`)
	return root, leafDir
}

func newNonDottedIslandProject(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	leafDir := writeIslandModule(t, root, "leaf", "corp/leaf", "leaf", testIslandSource("leaf", "LeafIsland"))
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module corp/app

go 1.22

require corp/leaf v0.0.0
replace corp/leaf => ../leaf
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import leaf "corp/leaf"

func Page() Node {
	return <leaf.LeafIsland></leaf.LeafIsland>
}
`)
	return appDir, leafDir
}

func TestIslandPackageResolverDistinguishesStandardLibraryWithGoMetadata(t *testing.T) {
	appDir := t.TempDir()
	writeTempFile(t, appDir, "go.mod", "module corp/app\n\ngo 1.22\n")
	resolver := newIslandPackageResolver(appDir)
	info, err := resolver.goList("fmt")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Standard || info.ImportPath != "fmt" || info.Name != "fmt" {
		t.Fatalf("fmt metadata = %#v, want Standard Go package", info)
	}
	packages, err := resolver.resolve([]string{"fmt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 0 {
		t.Fatalf("standard library resolved as island package: %#v", packages)
	}
}

func TestCollectProjectIslandProgramsHonorsActivePlatformGoFiles(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		t.Skip("platform fixture exercises the Linux/amd64 active-file set")
	}
	root := t.TempDir()
	widgetDir := writeIslandModule(t, root, "widget", "corp/widget", "widget", testIslandSource("widget", "Widget"))
	writeTempFile(t, widgetDir, "stub.go", "package widget\n\nconst Marker = true\n")
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module corp/app

go 1.22

require corp/widget v0.0.0
replace corp/widget => ../widget
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

func Page() Node {
	return <widget.Widget></widget.Widget>
}
`)
	writeTempFile(t, appDir, "app/bridge_active.go", `//go:build linux && amd64

package app

import widget "corp/widget"

var _ = widget.Marker
`)
	writeTempFile(t, appDir, "app/bridge_inactive.go", `//go:build windows || arm64

package inactiveplatform

import widget "corp/windows-widget"

var _ = widget.Marker
`)

	files, packageName, imports, err := localGoPackageMetadata(filepath.Join(appDir, "app"))
	if err != nil {
		t.Fatalf("resolve active platform files: %v", err)
	}
	if packageName != "app" || !reflect.DeepEqual(imports, []string{"corp/widget"}) {
		t.Fatalf("active metadata: package=%q imports=%v, want app and corp/widget", packageName, imports)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "bridge_active.go") {
		t.Fatalf("active Go files = %v, want only bridge_active.go", files)
	}

	discovery, err := collectProjectIslandDiscovery(appDir)
	if err != nil {
		t.Fatalf("inactive platform package/import influenced discovery: %v", err)
	}
	if got, want := islandProgramIdentities(discovery.Programs), []string{"corp/widget#Widget"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("program identities = %#v, want %#v", got, want)
	}
	activePath := filepath.Join(appDir, "app", "bridge_active.go")
	inactivePath := filepath.Join(appDir, "app", "bridge_inactive.go")
	if !containsPath(discovery.WatchGoFiles, activePath) || !containsPath(discovery.WatchFiles, activePath) {
		t.Fatalf("active platform Go source missing from watch identities: go=%#v all=%#v", discovery.WatchGoFiles, discovery.WatchFiles)
	}
	if containsPath(discovery.WatchGoFiles, inactivePath) || containsPath(discovery.WatchFiles, inactivePath) {
		t.Fatalf("inactive platform Go source entered watch identities: go=%#v all=%#v", discovery.WatchGoFiles, discovery.WatchFiles)
	}
}

func TestCollectProjectIslandProgramsHonorsCgoBuildConstraint(t *testing.T) {
	t.Setenv("CGO_ENABLED", "0")
	root := t.TempDir()
	widgetDir := writeIslandModule(t, root, "widget", "corp/widget", "widget", testIslandSource("widget", "Widget"))
	writeTempFile(t, widgetDir, "stub.go", "package widget\n\nconst Marker = true\n")
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module corp/app

go 1.22

require corp/widget v0.0.0
replace corp/widget => ../widget
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

func Page() Node {
	return <widget.Widget></widget.Widget>
}
`)
	writeTempFile(t, appDir, "app/bridge_nocgo.go", `//go:build !cgo

package app

import widget "corp/widget"

var _ = widget.Marker
`)
	writeTempFile(t, appDir, "app/bridge_cgo.go", `//go:build cgo

package inactivecgo

import widget "corp/cgo-only-widget"

var _ = widget.Marker
`)

	discovery, err := collectProjectIslandDiscovery(appDir)
	if err != nil {
		t.Fatalf("cgo-excluded package/import influenced discovery: %v", err)
	}
	if got, want := islandProgramIdentities(discovery.Programs), []string{"corp/widget#Widget"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("program identities = %#v, want %#v", got, want)
	}
	activePath := filepath.Join(appDir, "app", "bridge_nocgo.go")
	inactivePath := filepath.Join(appDir, "app", "bridge_cgo.go")
	if !containsPath(discovery.WatchGoFiles, activePath) || containsPath(discovery.WatchGoFiles, inactivePath) {
		t.Fatalf("cgo active selection not preserved in watch identities: go=%#v", discovery.WatchGoFiles)
	}
}

func TestCollectProjectIslandDiscoveryCarriesApplicableCgoFile(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("active cgo watcher fixture is exercised on Linux")
	}
	t.Setenv("CGO_ENABLED", "1")
	root := t.TempDir()
	widgetDir := writeIslandModule(t, root, "widget", "corp/widget", "widget", testIslandSource("widget", "Widget"))
	writeTempFile(t, widgetDir, "stub.go", "package widget\n\nconst Marker = true\n")
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module corp/app

go 1.22

require corp/widget v0.0.0
replace corp/widget => ../widget
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

func Page() Node {
	return <widget.Widget></widget.Widget>
}
`)
	writeTempFile(t, appDir, "app/bridge_cgo.go", `package app

/*
*/
import "C"
import widget "corp/widget"

var _ = widget.Marker
`)

	discovery, err := collectProjectIslandDiscovery(appDir)
	if err != nil {
		t.Fatalf("discover active cgo package: %v", err)
	}
	cgoPath := filepath.Join(appDir, "app", "bridge_cgo.go")
	if !containsPath(discovery.WatchGoFiles, cgoPath) || !containsPath(discovery.WatchFiles, cgoPath) {
		t.Fatalf("applicable CgoFiles input missing from watch identities: go=%#v all=%#v", discovery.WatchGoFiles, discovery.WatchFiles)
	}
}

func TestCollectProjectIslandProgramsUsesResolvedGoPackageNameForDefaultImport(t *testing.T) {
	root := t.TempDir()
	widgetsDir := writeIslandModule(t, root, "components", "corp/components", "widgets", testIslandSource("widgets", "SharedIsland"))
	writeTempFile(t, widgetsDir, "stub.go", "package widgets\n\nconst Marker = true\n")
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module corp/app

go 1.22

require corp/components v0.0.0
replace corp/components => ../components
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

func Page() Node {
	return <widgets.SharedIsland></widgets.SharedIsland>
}
`)
	writeTempFile(t, appDir, "app/page.server.go", `package app

import "corp/components"

var _ = widgets.Marker
`)

	programs, _, err := collectProjectIslandPrograms(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := islandProgramIdentities(programs), []string{"corp/components#SharedIsland"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("program identities = %#v, want package-name-aware import resolution %#v", got, want)
	}
}

func TestCollectProjectIslandProgramsFollowsNonDottedModulePath(t *testing.T) {
	appDir, leafDir := newNonDottedIslandProject(t)

	discovery, err := collectProjectIslandDiscovery(appDir)
	if err != nil {
		t.Fatalf("collect non-dotted module closure: %v", err)
	}
	if got, want := islandProgramIdentities(discovery.Programs), []string{"corp/leaf#LeafIsland"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("program identities = %#v, want %#v", got, want)
	}
	canonicalLeaf, err := canonicalExistingDir(leafDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(discovery.WatchDirs, canonicalLeaf) {
		t.Fatalf("watch dirs = %#v, missing non-dotted dependency %q", discovery.WatchDirs, canonicalLeaf)
	}
}

func TestCollectProjectIslandProgramsFailsClosedWhenDefaultGoImportNameCannotResolve(t *testing.T) {
	appDir := t.TempDir()
	writeTempFile(t, appDir, "go.mod", "module corp/app\n\ngo 1.22\n")
	writeTempFile(t, appDir, "app/page.gsx", `package app

func Page() Node {
	return <mystery.SharedIsland></mystery.SharedIsland>
}
`)
	writeTempFile(t, appDir, "app/page.server.go", `package app

import "corp/app/missing"
`)

	_, _, err := collectProjectIslandPrograms(appDir)
	if err == nil || !strings.Contains(err.Error(), "resolve default Go import name corp/app/missing") {
		t.Fatalf("unresolved default import error = %v", err)
	}
}

func TestCollectProjectIslandProgramsDedupesSymlinkedSourceIdentity(t *testing.T) {
	appDir := t.TempDir()
	writeTempFile(t, appDir, "go.mod", "module example.com/app\n\ngo 1.22\n")
	writeTempFile(t, appDir, "pkg/island.gsx", testIslandSource("pkg", "Counter"))
	target := filepath.Join(appDir, "pkg", "island.gsx")
	if err := os.Symlink(target, filepath.Join(appDir, "pkg", "island_alias.gsx")); err != nil {
		t.Fatal(err)
	}

	programs, files, err := collectProjectIslandPrograms(appDir)
	if err != nil {
		t.Fatalf("collect same physical source through symlink: %v", err)
	}
	if len(programs) != 1 || programs[0].Name != "Counter" || len(files) != 1 {
		t.Fatalf("programs=%#v files=%#v, want one physical source", islandProgramIdentities(programs), files)
	}
}

func TestCollectProjectIslandDiscoveryCarriesCanonicalNestedSymlinkWatchTarget(t *testing.T) {
	root := t.TempDir()
	depDir := filepath.Join(root, "dep")
	writeTempFile(t, depDir, "go.mod", "module corp/dep\n\ngo 1.22\n")
	writeTempFile(t, depDir, "stub.go", "package dep\n")
	physical := filepath.Join(depDir, "physical", "counter.gsx")
	writeTempFile(t, depDir, "physical/counter.gsx", testIslandSource("dep", "Counter"))
	if err := os.Symlink(physical, filepath.Join(depDir, "counter.gsx")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module corp/app

go 1.22

require corp/dep v0.0.0
replace corp/dep => ../dep
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import dep "corp/dep"

func Page() Node {
	return <dep.Counter></dep.Counter>
}
`)

	discovery, err := collectProjectIslandDiscovery(appDir)
	if err != nil {
		t.Fatalf("discovery rejected in-package symlink target: %v", err)
	}
	if len(discovery.Programs) != 1 || discovery.Programs[0].SourceFile != physical {
		t.Fatalf("program source = %#v, want canonical nested target %q", discovery.Programs, physical)
	}
	canonicalDep, err := canonicalExistingDir(depDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsPath(discovery.WatchDirs, canonicalDep) {
		t.Fatalf("watch dirs %#v do not include dependency root %q", discovery.WatchDirs, canonicalDep)
	}
	if !containsPath(discovery.WatchFiles, physical) {
		t.Fatalf("watch files %#v do not include canonical nested target %q", discovery.WatchFiles, physical)
	}

	dirs, files, goFiles, err := collectProjectIslandWatchTargets(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dirs, discovery.WatchDirs) || !reflect.DeepEqual(files, discovery.WatchFiles) || !reflect.DeepEqual(goFiles, discovery.WatchGoFiles) {
		t.Fatalf("refreshed watch targets dirs=%#v files=%#v go=%#v, want discovery dirs=%#v files=%#v go=%#v", dirs, files, goFiles, discovery.WatchDirs, discovery.WatchFiles, discovery.WatchGoFiles)
	}
}

func TestCollectProjectIslandProgramsCanonicalizesSymlinkedProjectRoot(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	writeTempFile(t, realDir, "go.mod", "module example.com/app\n\ngo 1.22\n")
	writeTempFile(t, realDir, "island.gsx", testIslandSource("app", "Counter"))
	linkedDir := filepath.Join(root, "linked")
	if err := os.Symlink(realDir, linkedDir); err != nil {
		t.Fatal(err)
	}

	programs, files, err := collectProjectIslandPrograms(linkedDir)
	if err != nil {
		t.Fatalf("collect through symlinked project root: %v", err)
	}
	if len(programs) != 1 || programs[0].Name != "Counter" || len(files) != 1 {
		t.Fatalf("programs=%#v files=%#v, want Counter through symlinked root", islandProgramIdentities(programs), files)
	}
	if strings.HasPrefix(files[0], linkedDir+string(filepath.Separator)) {
		t.Fatalf("source identity remained logical symlink path instead of canonical physical path: %q", files[0])
	}
}

func TestCollectProjectIslandProgramsDedupesSameModuleThroughSymlinkAliases(t *testing.T) {
	root := t.TempDir()
	sharedDir := writeIslandModule(t, root, "shared", "example.com/shared", "shared", testIslandSource("shared", "SharedIsland"))
	leftDir := filepath.Join(root, "left")
	rightDir := filepath.Join(root, "right")
	if err := os.Symlink(sharedDir, leftDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sharedDir, rightDir); err != nil {
		t.Fatal(err)
	}
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module example.com/app

go 1.22

require (
	example.com/left v0.0.0
	example.com/right v0.0.0
)
replace example.com/left => ../left
replace example.com/right => ../right
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import (
	left "example.com/left"
	right "example.com/right"
)

func Page() Node {
	return <main><left.SharedIsland></left.SharedIsland><right.SharedIsland></right.SharedIsland></main>
}
`)

	programs, files, err := collectProjectIslandPrograms(appDir)
	if err != nil {
		t.Fatalf("collect same physical module through symlink aliases: %v", err)
	}
	if len(programs) != 1 || programs[0].Name != "SharedIsland" || len(files) != 2 {
		t.Fatalf("programs=%#v files=%#v, want one physical imported island plus project source", islandProgramIdentities(programs), files)
	}
}

func TestCollectProjectIslandProgramsTraversesGoOnlyBridgePackage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture keeps bridge_windows.go inactive")
	}
	root := t.TempDir()
	leafDir := writeIslandModule(t, root, "leaf", "example.com/leaf", "leaf", testIslandSource("leaf", "LeafIsland"))
	writeTempFile(t, leafDir, "stub.go", "package leaf\n\nconst Marker = true\n")
	bridgeDir := filepath.Join(root, "bridge")
	writeTempFile(t, bridgeDir, "go.mod", `module example.com/bridge

go 1.22

require example.com/leaf v0.0.0
replace example.com/leaf => ../leaf
`)
	writeTempFile(t, bridgeDir, "bridge.go", `package bridge

import leaf "example.com/leaf"

var _ = leaf.Marker
`)
	writeTempFile(t, bridgeDir, "bridge_windows.go", `//go:build windows

package inactivebridge

import leaf "example.com/inactive-leaf"

var _ = leaf.Marker
`)
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module example.com/app

go 1.22

require (
	example.com/bridge v0.0.0
	example.com/leaf v0.0.0
)
replace example.com/bridge => ../bridge
replace example.com/leaf => ../leaf
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import bridge "example.com/bridge"

func Page() Node {
	return <bridge.Wrapper></bridge.Wrapper>
}
`)

	discovery, err := collectProjectIslandDiscovery(appDir)
	if err != nil {
		t.Fatalf("collect through Go-only bridge: %v", err)
	}
	if got, want := islandProgramIdentities(discovery.Programs), []string{"example.com/leaf#LeafIsland"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("program identities = %#v, want %#v", got, want)
	}
	for _, dir := range []string{bridgeDir, leafDir} {
		canonical, err := canonicalExistingDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !containsPath(discovery.WatchDirs, canonical) {
			t.Fatalf("watch dirs = %#v, missing closure directory %q", discovery.WatchDirs, canonical)
		}
	}
	activeBridge := filepath.Join(bridgeDir, "bridge.go")
	inactiveBridge := filepath.Join(bridgeDir, "bridge_windows.go")
	if !containsPath(discovery.WatchGoFiles, activeBridge) || !containsPath(discovery.WatchFiles, activeBridge) {
		t.Fatalf("active Go-only bridge source missing from watch identities: go=%#v all=%#v", discovery.WatchGoFiles, discovery.WatchFiles)
	}
	if containsPath(discovery.WatchGoFiles, inactiveBridge) || containsPath(discovery.WatchFiles, inactiveBridge) {
		t.Fatalf("inactive platform bridge source entered watch identities: go=%#v all=%#v", discovery.WatchGoFiles, discovery.WatchFiles)
	}
}

func TestCollectProjectIslandProgramsRejectsMixedGSXPackageDeclarationsDeterministically(t *testing.T) {
	appDir := t.TempDir()
	writeTempFile(t, appDir, "go.mod", "module example.com/app\n\ngo 1.22\n")
	writeTempFile(t, appDir, "pkg/alpha.gsx", testIslandSource("alpha", "AlphaIsland"))
	writeTempFile(t, appDir, "pkg/beta.gsx", testIslandSource("beta", "BetaIsland"))

	_, _, err := collectProjectIslandPrograms(appDir)
	if err == nil || !strings.Contains(err.Error(), "mixed island package declarations") {
		t.Fatalf("mixed package declarations error = %v", err)
	}
	_, _, againErr := collectProjectIslandPrograms(appDir)
	if againErr == nil || againErr.Error() != err.Error() {
		t.Fatalf("mixed-package diagnostic changed across runs:\nfirst: %v\nsecond: %v", err, againErr)
	}
}

func TestCollectProjectIslandProgramsRejectsResolvedGoPackageMismatch(t *testing.T) {
	root := t.TempDir()
	leafDir := writeIslandModule(t, root, "leaf", "example.com/leaf", "leaf", testIslandSource("wrong", "LeafIsland"))
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module example.com/app

go 1.22

require example.com/leaf v0.0.0
replace example.com/leaf => ../leaf
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import leaf "example.com/leaf"

func Page() Node {
	return <leaf.LeafIsland></leaf.LeafIsland>
}
`)

	_, _, err := collectProjectIslandPrograms(appDir)
	if err == nil {
		t.Fatal("resolved Go package accepted mismatched .gsx package declaration")
	}
	for _, want := range []string{"island package identity mismatch", "example.com/leaf", `declares package "wrong"`, `resolved Go package is "leaf"`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("package mismatch error = %q, missing %q", err, want)
		}
	}
	if !strings.Contains(err.Error(), filepath.ToSlash(filepath.Join(leafDir, "island.gsx"))) {
		t.Fatalf("package mismatch error lacks source origin: %v", err)
	}
}

func TestCollectProjectIslandProgramsRejectsGSXSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", "module example.com/app\n\ngo 1.22\n")
	writeTempFile(t, root, "outside.gsx", testIslandSource("pkg", "Outside"))
	external := filepath.Join(root, "outside.gsx")
	if err := os.MkdirAll(filepath.Join(appDir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	logical := filepath.Join(appDir, "pkg", "outside.gsx")
	if err := os.Symlink(external, logical); err != nil {
		t.Fatal(err)
	}

	_, _, err := collectProjectIslandPrograms(appDir)
	if err == nil || !strings.Contains(err.Error(), "resolves outside package root") {
		t.Fatalf("GSX symlink escape error = %v", err)
	}
}

func TestCollectProjectIslandProgramsHandlesDiamondCycleOnce(t *testing.T) {
	root, leafDir := newDiamondCycleIslandRoot(t)
	leftDir := writeIslandModule(t, root, "left", "example.com/left", "left", `package left

import (
	leaf "example.com/leaf"
	right "example.com/right"
)

func LeftWrapper() Node {
	return <main><leaf.LeafIsland></leaf.LeafIsland><right.RightWrapper></right.RightWrapper></main>
}

//gosx:island
component LeftIsland() {
	value := signal.New(0)
	return <span>{value.Get()}</span>
}
`)
	rightDir := writeIslandModule(t, root, "right", "example.com/right", "right", `package right

import (
	leaf "example.com/leaf"
	left "example.com/left"
)

func RightWrapper() Node {
	return <main><leaf.LeafIsland></leaf.LeafIsland><left.LeftWrapper></left.LeftWrapper></main>
}

//gosx:island
component RightIsland() {
	value := signal.New(0)
	return <span>{value.Get()}</span>
}
`)
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module example.com/app

go 1.22

require (
	example.com/leaf v0.0.0
	example.com/left v0.0.0
	example.com/right v0.0.0
)

replace example.com/leaf => ../leaf
replace example.com/left => ../left
replace example.com/right => ../right
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import left "example.com/left"

func Page() Node {
	return <left.LeftWrapper></left.LeftWrapper>
}
`)

	programs, files, err := collectProjectIslandPrograms(appDir)
	if err != nil {
		t.Fatalf("collectProjectIslandPrograms: %v", err)
	}
	wantPrograms := []string{
		"example.com/leaf#LeafIsland",
		"example.com/left#LeftIsland",
		"example.com/right#RightIsland",
	}
	if got := islandProgramIdentities(programs); !reflect.DeepEqual(got, wantPrograms) {
		t.Fatalf("cycle program identities = %#v, want %#v", got, wantPrograms)
	}
	if len(files) != 4 {
		t.Fatalf("cycle discovered files = %#v, want each package exactly once", files)
	}
	for _, dir := range []string{leafDir, leftDir, rightDir} {
		count := 0
		for _, file := range files {
			if filepath.Clean(filepath.Dir(file)) == filepath.Clean(dir) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("package %s appeared %d times in %#v, want once", dir, count, files)
		}
	}
}

func TestCollectProjectIslandProgramsRejectsAmbiguousNamesWithBothOrigins(t *testing.T) {
	appDir, firstSource, secondSource := newAmbiguousIslandProject(t, false)

	_, _, err := collectProjectIslandPrograms(appDir)
	if err == nil {
		t.Fatal("collectProjectIslandPrograms accepted duplicate Counter islands")
	}
	for _, want := range []string{
		"ambiguous island program \"Counter\"",
		"example.com/alpha",
		"example.com/beta",
		filepath.ToSlash(firstSource),
		filepath.ToSlash(secondSource),
		"names must be unique across the project dependency closure",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("collectProjectIslandPrograms error = %q, want substring %q", err, want)
		}
	}

	_, _, againErr := collectProjectIslandPrograms(appDir)
	if againErr == nil || againErr.Error() != err.Error() {
		t.Fatalf("duplicate diagnostic changed across runs:\nfirst:  %v\nsecond: %v", err, againErr)
	}

	partial, partialErr := collectProjectIslandDiscovery(appDir)
	if partialErr == nil || partial == nil {
		t.Fatalf("ambiguous discovery partial=%#v error=%v, want safe watcher closure plus validation error", partial, partialErr)
	}
	for _, source := range []string{firstSource, secondSource} {
		if !containsPath(partial.WatchDirs, filepath.Dir(source)) {
			t.Fatalf("partial watcher closure = %#v, missing invalid dependency directory %q", partial.WatchDirs, filepath.Dir(source))
		}
	}
	watchDirs, watchErr := collectProjectIslandWatchDirs(appDir)
	if watchErr == nil || watchErr.Error() != partialErr.Error() {
		t.Fatalf("recovery watch dirs error = %v, want original validation error %v", watchErr, partialErr)
	}
	if !reflect.DeepEqual(watchDirs, partial.WatchDirs) {
		t.Fatalf("recovery watch dirs = %#v, want %#v", watchDirs, partial.WatchDirs)
	}
}

func TestIslandManifestStageWritesTransitiveProgramsDeterministically(t *testing.T) {
	appDir := newTransitiveIslandProject(t)

	manifest, manifestPath, err := islandManifestStage(appDir, false)
	if err != nil {
		t.Fatalf("islandManifestStage: %v", err)
	}
	if got, want := islandAssetNames(manifest.Islands), []string{"LeafIsland", "MiddleIsland"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest islands = %#v, want %#v", got, want)
	}
	firstManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	firstAssets, err := os.ReadDir(filepath.Join(appDir, "dist", "assets", "islands"))
	if err != nil {
		t.Fatal(err)
	}
	firstNames := directoryEntryNames(firstAssets)

	if _, _, err := islandManifestStage(appDir, false); err != nil {
		t.Fatalf("second islandManifestStage: %v", err)
	}
	secondManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	secondAssets, err := os.ReadDir(filepath.Join(appDir, "dist", "assets", "islands"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(secondManifest, firstManifest) {
		t.Fatalf("manifest bytes changed across identical runs:\nfirst:\n%s\nsecond:\n%s", firstManifest, secondManifest)
	}
	if got := directoryEntryNames(secondAssets); !reflect.DeepEqual(got, firstNames) {
		t.Fatalf("island artifact names changed across runs: first=%#v second=%#v", firstNames, got)
	}
}

func TestIslandManifestStageRejectsAmbiguousNamesBeforeDistWrites(t *testing.T) {
	appDir, _, _ := newAmbiguousIslandProject(t, false)

	_, _, err := islandManifestStage(appDir, false)
	if err == nil || !strings.Contains(err.Error(), "ambiguous island program \"Counter\"") {
		t.Fatalf("islandManifestStage error = %v, want ambiguous island rejection", err)
	}
	if _, statErr := os.Stat(filepath.Join(appDir, "dist")); !os.IsNotExist(statErr) {
		t.Fatalf("ambiguous island stage wrote dist before failing: %v", statErr)
	}
}

func newTransitiveIslandProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	leafDir := writeIslandModule(t, root, "leaf", "example.com/leaf", "leaf", `package leaf

//gosx:island
component LeafIsland() {
	value := signal.New(0)
	return <span>{value.Get()}</span>
}
`)
	writeTempFile(t, leafDir, "stub.go", "package leaf\n\nconst Marker = true\n")
	middleDir := writeIslandModule(t, root, "middle", "example.com/middle", "middle", `package middle

func Wrapper() Node {
	return <leaf.LeafIsland></leaf.LeafIsland>
}

//gosx:island
component MiddleIsland() {
	value := signal.New(0)
	return <span>{value.Get()}</span>
}
`)
	writeTempFile(t, middleDir, "go.mod", `module example.com/middle

go 1.22

require example.com/leaf v0.0.0
`)
	writeTempFile(t, middleDir, "bridge.go", `package middle

import leaf "example.com/leaf"

var _ = leaf.Marker
`)
	appDir := filepath.Join(root, "app")
	writeTempFile(t, appDir, "go.mod", `module example.com/app

go 1.22

require (
	example.com/leaf v0.0.0
	example.com/middle v0.0.0
)

replace example.com/leaf => ../leaf
replace example.com/middle => ../middle
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import middle "example.com/middle"

func Page() Node {
	return <middle.Wrapper></middle.Wrapper>
}
`)
	return appDir
}

func newAmbiguousIslandProject(t *testing.T, includeGoSX bool) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	alphaDir := writeIslandModule(t, root, "alpha", "example.com/alpha", "alpha", `package alpha

//gosx:island
component Counter() {
	value := signal.New(1)
	return <span>{value.Get()}</span>
}
`)
	betaDir := writeIslandModule(t, root, "beta", "example.com/beta", "beta", `package beta

//gosx:island
component Counter() {
	value := signal.New(2)
	return <span>{value.Get()}</span>
}
`)
	appDir := filepath.Join(root, "app")
	gosxRequire := ""
	gosxReplace := ""
	if includeGoSX {
		gosxRequire = "\tm31labs.dev/gosx v0.53.9\n"
		gosxReplace = "replace m31labs.dev/gosx => " + filepath.ToSlash(testRepoRoot(t)) + "\n"
		writeTempFile(t, appDir, "main.go", "package main\n\nfunc main() {}\n")
	}
	writeTempFile(t, appDir, "go.mod", `module example.com/app

go 1.22

require (
	example.com/alpha v0.0.0
	example.com/beta v0.0.0
`+gosxRequire+`)

replace example.com/alpha => ../alpha
replace example.com/beta => ../beta
`+gosxReplace)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import (
	alpha "example.com/alpha"
	beta "example.com/beta"
)

func Page() Node {
	return <main><alpha.Counter></alpha.Counter><beta.Counter></beta.Counter></main>
}
`)
	return appDir, filepath.Join(alphaDir, "island.gsx"), filepath.Join(betaDir, "island.gsx")
}

func writeIslandModule(t *testing.T, root, dirName, modulePath, packageName, source string) string {
	t.Helper()
	dir := filepath.Join(root, dirName)
	writeTempFile(t, dir, "go.mod", "module "+modulePath+"\n\ngo 1.22\n")
	writeTempFile(t, dir, "stub.go", "package "+packageName+"\n")
	writeTempFile(t, dir, "island.gsx", source)
	return dir
}

func islandProgramIdentities(programs []*IslandProgramSource) []string {
	identities := make([]string, 0, len(programs))
	for _, program := range programs {
		identities = append(identities, program.PackagePath+"#"+program.Name)
	}
	return identities
}

func islandAssetNames(assets []IslandAsset) []string {
	names := make([]string, 0, len(assets))
	for _, asset := range assets {
		names = append(names, asset.Name)
	}
	return names
}

func directoryEntryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func hasPathSuffix(files []string, suffix string) bool {
	suffix = filepath.ToSlash(suffix)
	for _, file := range files {
		if strings.HasSuffix(filepath.ToSlash(file), suffix) {
			return true
		}
	}
	return false
}

func containsPath(paths []string, want string) bool {
	want = filepath.Clean(want)
	for _, path := range paths {
		if filepath.Clean(path) == want {
			return true
		}
	}
	return false
}

func testIslandSource(packageName, componentName string) string {
	return `package ` + packageName + `

//gosx:island
component ` + componentName + `() {
	value := signal.New(0)
	return <span>{value.Get()}</span>
}
`
}

func frozenWriteCmd(t *testing.T, filename, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func frozenIsland(pkg, name string) string {
	return "package " + pkg + "\n\n//gosx:island\ncomponent " + name + "() {\n\tvalue := signal.New(0)\n\treturn <span>{value.Get()}</span>\n}\n"
}

func frozenImportedApp(t *testing.T, root, depDir string) string {
	t.Helper()
	appDir := filepath.Join(root, "app")
	frozenWriteCmd(t, filepath.Join(appDir, "go.mod"), `module corp/app

go 1.22

require corp/dep v0.0.0
replace corp/dep => ../dep
`)
	frozenWriteCmd(t, filepath.Join(appDir, "app", "page.gsx"), `package app

import dep "corp/dep"

func Page() Node {
	return <dep.Counter></dep.Counter>
}
`)
	return appDir
}

func TestFrozenReviewerPartialInvalidSourceIdentityRetainsNewPackageRoot(t *testing.T) {
	root := t.TempDir()
	depDir := filepath.Join(root, "dep")
	frozenWriteCmd(t, filepath.Join(depDir, "go.mod"), "module corp/dep\n\ngo 1.22\n")
	frozenWriteCmd(t, filepath.Join(depDir, "stub.go"), "package dep\n")
	outside := filepath.Join(root, "outside.gsx")
	frozenWriteCmd(t, outside, frozenIsland("dep", "Counter"))
	logical := filepath.Join(depDir, "counter.gsx")
	if err := os.Symlink(outside, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	safePhysical := filepath.Join(depDir, "physical", "recovery.gsx")
	frozenWriteCmd(t, safePhysical, frozenIsland("dep", "Recovery"))
	if err := os.Symlink(safePhysical, filepath.Join(depDir, "z_recovery.gsx")); err != nil {
		t.Fatal(err)
	}
	appDir := frozenImportedApp(t, root, depDir)

	partial, err := collectProjectIslandDiscovery(appDir)
	if err == nil || !strings.Contains(err.Error(), "resolves outside package root") {
		t.Fatalf("escaping imported source error = %v", err)
	}
	if partial == nil {
		t.Fatal("invalid imported source returned no partial watcher closure")
	}
	canonicalDep, canonicalErr := canonicalExistingDir(depDir)
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	if !containsPath(partial.WatchDirs, canonicalDep) {
		t.Fatalf("partial invalid source closure lost newly resolved package root %q: dirs=%#v; retargeting the top-level link cannot recover dev without another project edit", canonicalDep, partial.WatchDirs)
	}
	if !containsPath(partial.WatchFiles, safePhysical) {
		t.Fatalf("partial invalid source closure lost independently safe exact source %q: files=%#v", safePhysical, partial.WatchFiles)
	}
}

func TestFrozenReviewerDiscoveryAcceptsUntrackedNestedHardlinkAlias(t *testing.T) {
	root := t.TempDir()
	depDir := filepath.Join(root, "dep")
	frozenWriteCmd(t, filepath.Join(depDir, "go.mod"), "module corp/dep\n\ngo 1.22\n")
	frozenWriteCmd(t, filepath.Join(depDir, "stub.go"), "package dep\n")
	logical := filepath.Join(depDir, "counter.gsx")
	frozenWriteCmd(t, logical, frozenIsland("dep", "Counter"))
	alias := filepath.Join(depDir, "physical", "counter-alias.gsx")
	if err := os.MkdirAll(filepath.Dir(alias), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(logical, alias); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	appDir := frozenImportedApp(t, root, depDir)

	discovery, err := collectProjectIslandDiscovery(appDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(discovery.Programs) != 1 || discovery.Programs[0].SourceFile != logical {
		t.Fatalf("discovery programs=%#v, want canonical top-level hardlink %q", discovery.Programs, logical)
	}
	if containsPath(discovery.WatchFiles, alias) {
		t.Fatalf("nested hardlink alias unexpectedly appeared in direct source discovery: %#v", discovery.WatchFiles)
	}
	info, err := os.Stat(logical)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(info, aliasInfo) {
		t.Fatal("fixture paths are not the same physical file")
	}
}
