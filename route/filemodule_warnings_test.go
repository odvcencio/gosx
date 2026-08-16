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
