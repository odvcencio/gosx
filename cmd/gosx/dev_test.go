package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBuildServerBinaryIfPresentBuildsMainPackage(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "go.mod", "module example.com/app\ngo 1.22\n")
	writeTempFile(t, dir, "main.go", `package main

func main() {}
`)

	out := filepath.Join(dir, "dist", "server", "app")
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		t.Fatal(err)
	}

	built, err := buildServerBinaryIfPresent(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	if !built {
		t.Fatal("expected main package to build")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected binary at %s: %v", out, err)
	}
}

func TestRunDevStrictGateRunsBeforeAssetWritesOrRunnerStart(t *testing.T) {
	dir := newInvalidStrictStarter(t, "dev-strict-gate")
	err := RunDev(dir)
	if err == nil || !strings.Contains(err.Error(), "cannot use 42") {
		t.Fatalf("RunDev error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Fatalf("strict gate wrote dev assets before failing: %v", statErr)
	}
}

func TestRunDevRejectsAmbiguousIslandClosureBeforeGeneratedWritesOrRunnerStart(t *testing.T) {
	dir, _, _ := newAmbiguousIslandProject(t, true)
	err := RunDev(dir)
	if err == nil || !strings.Contains(err.Error(), "ambiguous island program \"Counter\"") {
		t.Fatalf("RunDev error = %v, want ambiguous island rejection", err)
	}
	for _, output := range []string{"build", "modules/modules.go"} {
		if _, statErr := os.Stat(filepath.Join(dir, filepath.FromSlash(output))); !os.IsNotExist(statErr) {
			t.Fatalf("ambiguous island dev preflight created %s before failing: %v", output, statErr)
		}
	}
}

func TestDevChangeStrictGateRunsBeforeAssetWritesOrRestart(t *testing.T) {
	dir := newInvalidStrictStarter(t, "dev-change-strict-gate")
	err := rebuildChangedDevApp(context.Background(), dir, nil, "0", "http://127.0.0.1:0")
	if err == nil || !strings.Contains(err.Error(), "cannot use 42") {
		t.Fatalf("rebuildChangedDevApp error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Fatalf("strict change gate wrote dev assets before failing: %v", statErr)
	}
}

type recordingDevStopper struct {
	stopped bool
}

func (s *recordingDevStopper) stop() error {
	s.stopped = true
	return nil
}

func TestDevHotSwapPreflightStopsUnsafeUpstream(t *testing.T) {
	dir := newInvalidStrictStarter(t, "dev-hotswap-strict-gate")
	runner := &recordingDevStopper{}
	err := preflightChangedDevApp(context.Background(), dir, runner)
	if err == nil || !strings.Contains(err.Error(), "cannot use 42") {
		t.Fatalf("preflightChangedDevApp error = %v", err)
	}
	if !runner.stopped {
		t.Fatal("invalid hot swap left the old upstream process running")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "build")); !os.IsNotExist(statErr) {
		t.Fatalf("hot-swap preflight wrote assets before failing: %v", statErr)
	}
}

func TestDevHotSwapPreflightStopsAmbiguousIslandUpstream(t *testing.T) {
	dir, _, _ := newAmbiguousIslandProject(t, false)
	runner := &recordingDevStopper{}

	err := preflightChangedDevApp(context.Background(), dir, runner)
	if err == nil || !strings.Contains(err.Error(), "ambiguous island program \"Counter\"") {
		t.Fatalf("preflightChangedDevApp error = %v, want ambiguous island rejection", err)
	}
	if !runner.stopped {
		t.Fatal("ambiguous island hot swap left the old upstream process running")
	}
}

func TestBuildServerBinaryIfPresentSkipsLibraryPackage(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "go.mod", "module example.com/lib\ngo 1.22\n")
	writeTempFile(t, dir, "lib.go", `package lib

func Value() int { return 1 }
`)

	out := filepath.Join(dir, "dist", "server", "app")
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		t.Fatal(err)
	}

	built, err := buildServerBinaryIfPresent(dir, out)
	if err != nil {
		t.Fatal(err)
	}
	if built {
		t.Fatal("expected library package to be skipped")
	}
	if _, err := os.Stat(out); err == nil {
		t.Fatal("unexpected server binary for library package")
	}
}

func TestCompileDevIslandsWritesJSONPrograms(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build", "islands")
	writeTempFile(t, dir, "counter.gsx", `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div><span>{count.Get()}</span><button onClick={increment}>+</button></div>
}
`)

	if err := compileDevIslands(dir, out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(out, "Counter.json"))
	if err != nil {
		t.Fatalf("expected Counter.json: %v", err)
	}
	if !strings.Contains(string(data), `"name": "Counter"`) {
		t.Fatalf("unexpected island JSON: %s", data)
	}
}

func TestCompileDevIslandsIncludesImportedPackageIslands(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	sharedDir := filepath.Join(root, "shared")
	out := filepath.Join(appDir, "build", "islands")

	writeTempFile(t, sharedDir, "go.mod", "module example.com/shared\n\ngo 1.22\n")
	writeTempFile(t, sharedDir, "shared.go", "package shared\n")
	writeTempFile(t, sharedDir, "island.gsx", `package shared

//gosx:island
func SharedIsland() Node {
	count := signal.New(0)
	return <div>{count.Get()}</div>
}
`)

	writeTempFile(t, appDir, "go.mod", `module example.com/app

go 1.22

require example.com/shared v0.0.0

replace example.com/shared => ../shared
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

import shared "example.com/shared"

func Page() Node {
	return <main><shared.SharedIsland></shared.SharedIsland></main>
}
`)

	if err := compileDevIslands(appDir, out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(out, "SharedIsland.json"))
	if err != nil {
		t.Fatalf("expected imported SharedIsland.json: %v", err)
	}
	if !strings.Contains(string(data), `"name": "SharedIsland"`) {
		t.Fatalf("unexpected imported island JSON: %s", data)
	}
}

func TestCompileDevIslandsIncludesRootPackageIslandFromQualifiedTagGoImport(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "app")
	studioDir := filepath.Join(root, "gosx-studio")
	out := filepath.Join(appDir, "build", "islands")

	writeTempFile(t, studioDir, "go.mod", "module m31labs.dev/gosx-studio\n\ngo 1.22\n")
	writeTempFile(t, studioDir, "studio.go", `package studio

func ServerOnlyValue() string { return "studio" }
`)
	writeTempFile(t, studioDir, "home_layer_selection_island.gsx", `package studio

//gosx:island
func HomeLayerSelectionIsland(props any) Node {
	selected := signal.New("home")
	return <div data-selected={selected.Get()}>{selected.Get()}</div>
}
`)

	writeTempFile(t, appDir, "go.mod", `module example.com/app

go 1.22

require m31labs.dev/gosx-studio v0.0.0

replace m31labs.dev/gosx-studio => ../gosx-studio
`)
	writeTempFile(t, appDir, "app/page.gsx", `package app

func Page() Node {
	return <main><studio.HomeLayerSelectionIsland></studio.HomeLayerSelectionIsland></main>
}
`)
	writeTempFile(t, appDir, "app/page.server.go", `package app

import studio "m31labs.dev/gosx-studio"

var _ = studio.ServerOnlyValue
`)

	if err := compileDevIslands(appDir, out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(out, "HomeLayerSelectionIsland.json"))
	if err != nil {
		t.Fatalf("expected imported root package HomeLayerSelectionIsland.json: %v", err)
	}
	if !strings.Contains(string(data), `"name": "HomeLayerSelectionIsland"`) {
		t.Fatalf("unexpected imported island JSON: %s", data)
	}
}

func TestCompileDevIslandsRejectsAmbiguousNamesBeforeReplacingAssets(t *testing.T) {
	appDir, _, _ := newAmbiguousIslandProject(t, false)
	out := filepath.Join(appDir, "build", "islands")
	writeTempFile(t, out, "Counter.json", "previous-valid-program")

	err := compileDevIslands(appDir, out)
	if err == nil || !strings.Contains(err.Error(), "ambiguous island program \"Counter\"") {
		t.Fatalf("compileDevIslands error = %v, want ambiguous island rejection", err)
	}
	data, readErr := os.ReadFile(filepath.Join(out, "Counter.json"))
	if readErr != nil {
		t.Fatalf("read previous island asset: %v", readErr)
	}
	if got := string(data); got != "previous-valid-program" {
		t.Fatalf("ambiguous discovery replaced previous island asset with %q", got)
	}
}

func TestStageSidecarCSSCopiesFiles(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build", "css")
	writeTempFile(t, dir, "counter.css", ".counter { color: red; }\n")

	if err := stageSidecarCSS(dir, out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(out, "counter.css"))
	if err != nil {
		t.Fatalf("expected copied css: %v", err)
	}
	if strings.TrimSpace(string(data)) != ".counter { color: red; }" {
		t.Fatalf("unexpected css contents: %q", string(data))
	}
}

func TestStageSidecarCSSPreservesRelativePaths(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "build", "css")
	writeTempFile(t, dir, "app/page.css", ".page { color: red; }\n")
	writeTempFile(t, dir, "app/docs/page.css", ".docs-page { color: blue; }\n")

	if err := stageSidecarCSS(dir, out); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"app/page.css", "app/docs/page.css"} {
		if _, err := os.Stat(filepath.Join(out, rel)); err != nil {
			t.Fatalf("expected preserved css path %s: %v", rel, err)
		}
	}
}

func TestKillProcessTreeStopsChildProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix shell process-tree assertions")
	}
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = childProcessAttributes()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	childPID, err := waitForChildProcess(cmd.Process.Pid, 2*time.Second)
	if err != nil {
		_ = terminateProcessTree(cmd.Process.Pid)
		<-done
		t.Fatal(err)
	}

	if err := terminateProcessTree(cmd.Process.Pid); err != nil {
		_ = terminateProcessTree(cmd.Process.Pid)
		<-done
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = terminateProcessTree(cmd.Process.Pid)
		t.Fatal("timed out waiting for process tree to stop")
	}

	if err := waitForProcessExit(childPID, 2*time.Second); err != nil {
		t.Fatal(err)
	}
}

func waitForChildProcess(parentPID int, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	parent := strconv.Itoa(parentPID)
	for time.Now().Before(deadline) {
		out, err := exec.Command("pgrep", "-P", parent).Output()
		if err == nil {
			fields := strings.Fields(string(out))
			for _, field := range fields {
				pid, err := strconv.Atoi(field)
				if err == nil && pid > 0 {
					return pid, nil
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, os.ErrDeadlineExceeded
}

func waitForProcessExit(pid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExists(pid) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return os.ErrDeadlineExceeded
}

func processExists(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false
	}
	state := strings.TrimSpace(string(out))
	return state != "" && !strings.HasPrefix(state, "Z")
}

func writeTempFile(t *testing.T, dir, name, contents string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
