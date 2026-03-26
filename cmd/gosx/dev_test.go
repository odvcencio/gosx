package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	cmd := exec.Command("sh", "-c", "sleep 30 & wait")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	childPID, err := waitForChildProcess(cmd.Process.Pid, 2*time.Second)
	if err != nil {
		_ = killProcessTree(cmd.Process.Pid)
		<-done
		t.Fatal(err)
	}

	if err := killProcessTree(cmd.Process.Pid); err != nil {
		_ = killProcessTree(cmd.Process.Pid)
		<-done
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = killProcessTree(cmd.Process.Pid)
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
		out, err := exec.Command("ps", "-o", "pid=", "--ppid", parent).Output()
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
