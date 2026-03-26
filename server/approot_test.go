package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAppRootUsesEnvOverride(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app"), 0755); err != nil {
		t.Fatal(err)
	}

	prev := os.Getenv("GOSX_APP_ROOT")
	t.Cleanup(func() {
		if prev == "" {
			_ = os.Unsetenv("GOSX_APP_ROOT")
			return
		}
		_ = os.Setenv("GOSX_APP_ROOT", prev)
	})
	if err := os.Setenv("GOSX_APP_ROOT", root); err != nil {
		t.Fatal(err)
	}

	if got := ResolveAppRoot("/tmp/ignored/main.go"); got != root {
		t.Fatalf("expected env root %q, got %q", root, got)
	}
}

func TestResolveAppRootFallsBackToCallerDir(t *testing.T) {
	prev := os.Getenv("GOSX_APP_ROOT")
	t.Cleanup(func() {
		if prev == "" {
			_ = os.Unsetenv("GOSX_APP_ROOT")
			return
		}
		_ = os.Setenv("GOSX_APP_ROOT", prev)
	})
	_ = os.Unsetenv("GOSX_APP_ROOT")

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app"), 0755); err != nil {
		t.Fatal(err)
	}
	caller := filepath.Join(root, "main.go")

	if got := ResolveAppRoot(caller); got != root {
		t.Fatalf("expected caller root %q, got %q", root, got)
	}
}
