package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveWASMCompilerUsesExplicitTinyGo(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom toolchain", "compiler")
	t.Setenv("TINYGO", want)
	compiler, got, err := resolveWASMCompiler(BuildOptions{}, func(name string) (string, error) {
		if name != want {
			t.Fatalf("compiler lookup = %q, want explicit %q", name, want)
		}
		return want, nil
	})
	if err != nil || compiler != wasmCompilerTinyGo || got != want {
		t.Fatalf("compiler = %q, path = %q, error = %v", compiler, got, err)
	}
}

func TestResolveWASMCompilerMakesExplicitRelativeTinyGoAbsolute(t *testing.T) {
	t.Chdir(t.TempDir())
	name := "custom-tinygo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	relative := filepath.Join("tools", name)
	if err := os.MkdirAll("tools", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(relative, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TINYGO", relative)
	want, err := filepath.Abs(relative)
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := resolveWASMCompiler(BuildOptions{}, exec.LookPath)
	if err != nil || got != want {
		t.Fatalf("path = %q, want %q, error = %v", got, want, err)
	}
}

func TestResolveWASMCompilerDevIgnoresExplicitTinyGo(t *testing.T) {
	t.Setenv("TINYGO", "missing-custom-tinygo")
	compiler, path, err := resolveWASMCompiler(BuildOptions{Dev: true}, func(string) (string, error) {
		t.Fatal("development build looked up TinyGo")
		return "", nil
	})
	if err != nil || compiler != wasmCompilerGo || path != "" {
		t.Fatalf("compiler = %q, path = %q, error = %v", compiler, path, err)
	}
}

func TestResolveWASMCompilerForProjectRejectsMissingExplicitTinyGo(t *testing.T) {
	t.Setenv(runtimeModeEnv, "auto")
	t.Setenv("TINYGO", "missing-custom-tinygo")
	original := resolveProjectGoSXVersionFunc
	resolveProjectGoSXVersionFunc = func(string) (string, bool, bool) {
		t.Fatal("explicit compiler failure attempted a prebuilt fallback")
		return "", false, false
	}
	t.Cleanup(func() { resolveProjectGoSXVersionFunc = original })
	_, _, _, err := resolveWASMCompilerForProject(BuildOptions{}, t.TempDir(), func(name string) (string, error) {
		return "", errors.New("compiler not found: " + name)
	})
	if err == nil || !strings.Contains(err.Error(), "TINYGO") || !strings.Contains(err.Error(), "missing-custom-tinygo") {
		t.Fatalf("missing explicit compiler error = %v", err)
	}
}

func TestResolveWASMCompilerForProjectExplicitPrebuiltKeepsPrecedence(t *testing.T) {
	t.Setenv(runtimeModeEnv, "prebuilt")
	t.Setenv("TINYGO", "unused-custom-tinygo")
	stubResolveProjectGoSXVersion(t, "v1.2.3", false, true)
	cache := t.TempDir()
	t.Setenv(runtimeCacheEnv, cache)
	if _, err := resolvePrebuiltRuntime(context.Background(), cache, buildFakeRuntimeRelease("v1.2.3"), "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	compiler, _, bundle, err := resolveWASMCompilerForProject(BuildOptions{Offline: true}, t.TempDir(), func(string) (string, error) {
		t.Fatal("explicit prebuilt mode looked up TinyGo")
		return "", nil
	})
	if err != nil || compiler != wasmCompilerPrebuilt || bundle == nil {
		t.Fatalf("compiler = %q, bundle = %v, error = %v", compiler, bundle, err)
	}
}
