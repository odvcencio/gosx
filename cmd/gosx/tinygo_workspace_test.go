package main

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func tinyGoWorkspaceSupported() bool {
	switch runtime.GOOS {
	case "darwin", "dragonfly", "freebsd", "linux", "netbsd", "openbsd", "windows":
		return true
	}
	return false
}

func TestTinyGoWorkspaceScopeAndCleanup(t *testing.T) {
	t.Setenv(runtimeCacheEnv, t.TempDir())
	source := t.TempDir()
	first, release, err := tinyGoScratchDirectory(source, "core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	if _, err := os.Stat(first); err != nil {
		t.Fatal(err)
	}
	other, releaseOther, err := tinyGoScratchDirectory(source, "engine")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseOther)
	if other == first {
		t.Fatal("variants share a workspace")
	}
	release()
	second, releaseSecond, err := tinyGoScratchDirectory(source, "core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseSecond)
	if tinyGoWorkspaceSupported() && first != second {
		t.Fatal("identical inputs lost their stable compiler source path")
	}
	marker := filepath.Join(second, "current")
	if err := os.WriteFile(marker, []byte("current"), 0600); err != nil {
		t.Fatal(err)
	}
	release()
	if data, err := os.ReadFile(marker); err != nil || string(data) != "current" {
		t.Fatal("repeated cleanup interfered with the next owner", err)
	}
	third, releaseThird, err := tinyGoScratchDirectory(t.TempDir(), "core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseThird)
	if third == second {
		t.Fatal("different source roots share a workspace")
	}
}

func TestTinyGoWorkspaceUnavailableCache(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(cache, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(runtimeCacheEnv, cache)
	dir, release, err := tinyGoScratchDirectory(t.TempDir(), "core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	if !strings.HasPrefix(filepath.Base(dir), "gosx-tinygo-wasm-") {
		t.Fatal("unavailable cache must retain isolated temporary builds", dir)
	}
	if data, err := os.ReadFile(cache); err != nil || string(data) != "retain" {
		t.Fatal("cache failure changed an existing file", err)
	}
}

func TestTinyGoWorkspaceProcessLock(t *testing.T) {
	if os.Getenv("GOSX_TEST_TINYGO_WORKSPACE_CHILD") == "1" {
		dir, release, err := tinyGoScratchDirectory(os.Getenv("GOSX_TEST_TINYGO_SOURCE"), "core")
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		if err := os.WriteFile(filepath.Join(dir, "stale"), []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(dir); err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	if !tinyGoWorkspaceSupported() {
		return // The other-platform implementation deliberately uses temporary builds.
	}
	t.Setenv(runtimeCacheEnv, t.TempDir())
	source := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestTinyGoWorkspaceProcessLock$")
	cmd.Env = append(os.Environ(), "GOSX_TEST_TINYGO_WORKSPACE_CHILD=1", "GOSX_TEST_TINYGO_SOURCE="+source)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	var owned string
	if err := json.NewDecoder(stdout).Decode(&owned); err != nil {
		t.Fatal(err)
	}
	busy, releaseBusy, err := tinyGoScratchDirectory(source, "core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseBusy)
	if busy == owned || !strings.HasPrefix(filepath.Base(busy), "gosx-tinygo-wasm-") {
		t.Fatal("a live compiler's workspace was reused", busy)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	recovered, releaseRecovered, err := tinyGoScratchDirectory(source, "core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseRecovered)
	if recovered != owned {
		t.Fatal("process exit left a stale lock", recovered, owned)
	}
	if _, err := os.Stat(filepath.Join(recovered, "stale")); !os.IsNotExist(err) {
		t.Fatal("a crashed build's source survived workspace preparation", err)
	}
}

func TestTinyGoScratchModuleRefreshesSources(t *testing.T) {
	t.Setenv(runtimeCacheEnv, t.TempDir())
	source := t.TempDir()
	mustWriteFile(t, filepath.Join(source, "go.mod"), "module m31labs.dev/gosx\n\ngo 1.25\n")
	mustWriteFile(t, filepath.Join(source, "client/wasm/main.go"), "package main\nimport \"m31labs.dev/gosx/core\"\nfunc main(){core.Use()}\n")
	mustWriteFile(t, filepath.Join(source, "core/use.go"), "package core\nfunc Use(){}\n")
	removed := filepath.Join(source, "core/removed.go")
	mustWriteFile(t, removed, "package core\nvar Removed=1\n")
	first, release, err := prepareTinyGoWASMModule(source, "tinygo", "gosx_runtime_core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	release()
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	want := "package core\nfunc Use(){println(2)}\n"
	mustWriteFile(t, filepath.Join(source, "core/use.go"), want)
	mustWriteFile(t, filepath.Join(source, "core/added.go"), "package core\nvar Added=2\n")
	second, releaseSecond, err := prepareTinyGoWASMModule(source, "tinygo", "gosx_runtime_core")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releaseSecond)
	if tinyGoWorkspaceSupported() && first != second {
		t.Fatal("source edits changed the compiler's workspace path")
	}
	if data, err := os.ReadFile(filepath.Join(second, "core/use.go")); err != nil || string(data) != want {
		t.Fatal("edited source was not restaged", err)
	}
	if _, err := os.Stat(filepath.Join(second, "core/removed.go")); !os.IsNotExist(err) {
		t.Fatal("deleted source was retained", err)
	}
	if _, err := os.Stat(filepath.Join(second, "core/added.go")); err != nil {
		t.Fatal("added source was not staged", err)
	}
}

func TestTinyGoWorkspaceCompilerRefreshesSources(t *testing.T) {
	tinygo, err := exec.LookPath("tinygo")
	if err != nil {
		t.Skip("TinyGo is required for the compiler integration check")
	}
	t.Setenv(runtimeCacheEnv, t.TempDir())
	source := t.TempDir()
	mustWriteFile(t, filepath.Join(source, "go.mod"), "module m31labs.dev/gosx\n\ngo 1.25\n")
	entry := filepath.Join(source, "client/wasm/main.go")
	compile := func(value string) [32]byte {
		t.Helper()
		mustWriteFile(t, entry, "package main\nfunc main(){println("+value+")}\n")
		output := filepath.Join(t.TempDir(), "runtime.wasm")
		if err := buildTinyGoWASM(source, source, output, tinygo, "gosx_runtime_core"); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(data)
	}
	first := compile("1")
	if changed := compile("2"); changed == first {
		t.Fatal("an edited program reused stale compiler output")
	}
	if restored := compile("1"); restored != first {
		t.Fatal("restored sources no longer produce the original module")
	}
}
