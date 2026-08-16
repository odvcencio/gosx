package route

import (
	"bytes"
	"log"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnregisteredFileModuleWarningsFiresForUnregisteredServerGo(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "board/page.gsx", `package app
func Page() Node {
	return <main>Board</main>
}
`)
	writeRouteFile(t, root, "board/page.server.go", `package app
func init() {}
`)

	bundle, err := ScanDir(root)
	if err != nil {
		t.Fatal(err)
	}

	warnings := unregisteredFileModuleWarnings(NewFileModuleRegistry(), root, bundle.Pages)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}

	wantDir := filepath.Join(root, "board")
	want := "gosx route: " + wantDir + " has a page.server.go but no registered module; regenerate modules.go (gosx build)"
	if warnings[0] != want {
		t.Fatalf("warning = %q, want %q", warnings[0], want)
	}
}

func TestUnregisteredFileModuleWarningsSkipsRegisteredModule(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "board/page.gsx", `package app
func Page() Node {
	return <main>Board</main>
}
`)
	writeRouteFile(t, root, "board/page.server.go", `package app
func init() {}
`)

	bundle, err := ScanDir(root)
	if err != nil {
		t.Fatal(err)
	}

	modules := NewFileModuleRegistry()
	if err := modules.Register(FileModuleFor(bundle.Pages[0].Source, FileModuleOptions{})); err != nil {
		t.Fatal(err)
	}

	if warnings := unregisteredFileModuleWarnings(modules, root, bundle.Pages); len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none for a registered module", warnings)
	}
}

func TestUnregisteredFileModuleWarningsSkipsPagesWithoutServerGo(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "board/page.gsx", `package app
func Page() Node {
	return <main>Board</main>
}
`)

	bundle, err := ScanDir(root)
	if err != nil {
		t.Fatal(err)
	}

	if warnings := unregisteredFileModuleWarnings(NewFileModuleRegistry(), root, bundle.Pages); len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none when there is no page.server.go to register", warnings)
	}
}

// TestUnregisteredFileModuleWarningsListsEachDirAtThreshold confirms exactly
// unregisteredFileModuleDirThreshold unregistered directories still get one
// line each, rather than collapsing to the summary form.
func TestUnregisteredFileModuleWarningsListsEachDirAtThreshold(t *testing.T) {
	root := t.TempDir()
	var wantDirs []string
	for _, name := range []string{"board", "roster"} {
		writeRouteFile(t, root, name+"/page.gsx", `package app
func Page() Node {
	return <main>Page</main>
}
`)
		writeRouteFile(t, root, name+"/page.server.go", `package app
func init() {}
`)
		wantDirs = append(wantDirs, filepath.Join(root, name))
	}

	bundle, err := ScanDir(root)
	if err != nil {
		t.Fatal(err)
	}

	warnings := unregisteredFileModuleWarnings(NewFileModuleRegistry(), root, bundle.Pages)
	if len(warnings) != len(wantDirs) {
		t.Fatalf("warnings = %v, want one line per directory (%d)", warnings, len(wantDirs))
	}
	for _, dir := range wantDirs {
		want := "gosx route: " + dir + " has a page.server.go but no registered module; regenerate modules.go (gosx build)"
		found := false
		for _, warning := range warnings {
			if warning == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("warnings = %v, want a line for %q", warnings, dir)
		}
	}
}

// TestUnregisteredFileModuleWarningsSummarizesAboveThreshold confirms more
// than unregisteredFileModuleDirThreshold unregistered directories collapse
// to one summary line instead of flooding the log with a line per directory
// — the failure a partially regenerated modules.go produced running a docs
// sub-package test with ten or more routed directories.
func TestUnregisteredFileModuleWarningsSummarizesAboveThreshold(t *testing.T) {
	root := t.TempDir()
	names := []string{"board", "roster", "standings"}
	for _, name := range names {
		writeRouteFile(t, root, name+"/page.gsx", `package app
func Page() Node {
	return <main>Page</main>
}
`)
		writeRouteFile(t, root, name+"/page.server.go", `package app
func init() {}
`)
	}

	bundle, err := ScanDir(root)
	if err != nil {
		t.Fatal(err)
	}

	warnings := unregisteredFileModuleWarnings(NewFileModuleRegistry(), root, bundle.Pages)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one summary line for %d unregistered directories", warnings, len(names))
	}
	if !strings.Contains(warnings[0], "3 routed directories have page.server.go files with no registered module") {
		t.Fatalf("warning = %q, want the summary count and phrasing", warnings[0])
	}
	if !strings.Contains(warnings[0], "regenerate modules.go (gosx build)") {
		t.Fatalf("warning = %q, want the regenerate hint", warnings[0])
	}
	if !strings.Contains(warnings[0], "(first: "+filepath.Join(root, "board")+")") {
		t.Fatalf("warning = %q, want the first unregistered directory named", warnings[0])
	}
}

// TestUnregisteredFileModuleWarningsWarnsRegardlessOfBuildTag documents
// current behavior for a page.server.go guarded by a build tag that excludes
// it from the default build: the warning still fires. This is a plain
// filesystem check (isFile on the *.server.go path) with no build-constraint
// parsing, so it cannot tell a genuinely stale modules.go from a
// build-tag-gated file the developer excluded on purpose. That is acceptable
// here — the check is a cheap diagnostic hint, not a build gate, and warning
// on a legitimately excluded file is a false positive a developer can read
// past, whereas staying silent on a genuinely stale modules.go is the failure
// this warning exists to catch.
func TestUnregisteredFileModuleWarningsWarnsRegardlessOfBuildTag(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "board/page.gsx", `package app
func Page() Node {
	return <main>Board</main>
}
`)
	writeRouteFile(t, root, "board/page.server.go", `//go:build ignore

package app
func init() {}
`)

	bundle, err := ScanDir(root)
	if err != nil {
		t.Fatal(err)
	}

	warnings := unregisteredFileModuleWarnings(NewFileModuleRegistry(), root, bundle.Pages)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one warning even though the build tag excludes page.server.go from compilation", warnings)
	}
}

// TestRouterBuildChecketLogsUnregisteredFileModuleWarning exercises the wiring
// through AddDir and BuildChecked end to end: a routed directory with a
// page.server.go on disk but no registration must warn at build time, not
// render silently with empty data.
func TestRouterBuildCheckedLogsUnregisteredFileModuleWarning(t *testing.T) {
	root := t.TempDir()
	writeRouteFile(t, root, "board/page.gsx", `package app
func Page() Node {
	return <main>Board</main>
}
`)
	writeRouteFile(t, root, "board/page.server.go", `package app
func init() {}
`)

	var logs bytes.Buffer
	prevOutput, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	}()

	router := NewRouter()
	if err := router.AddDir(root, FileRoutesOptions{Modules: NewFileModuleRegistry()}); err != nil {
		t.Fatal(err)
	}
	if _, err := router.BuildChecked(); err != nil {
		t.Fatal(err)
	}

	wantDir := filepath.Join(root, "board")
	want := "gosx route: " + wantDir + " has a page.server.go but no registered module; regenerate modules.go (gosx build)"
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("log output = %q, want it to contain %q", logs.String(), want)
	}
}
