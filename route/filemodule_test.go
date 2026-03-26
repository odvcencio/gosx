package route

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileModuleSourceFromFilePrefersSiblingPageFile(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "account", "page.gsx")
	if err := os.MkdirAll(filepath.Dir(page), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(page, []byte("package docs\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := fileModuleSourceFromFile(filepath.Join(root, "account", "page.server.go"))
	if got != page {
		t.Fatalf("expected %q, got %q", page, got)
	}
}

func TestMustRegisterFileModuleHereUsesCallerSource(t *testing.T) {
	source := helperFileModuleHereSource()

	previous := defaultFileModuleRegistry
	defaultFileModuleRegistry = NewFileModuleRegistry()
	defer func() {
		defaultFileModuleRegistry = previous
	}()

	helperMustRegisterFileModuleHere()

	if _, ok := defaultFileModuleRegistry.Lookup(source); !ok {
		t.Fatalf("expected module registration for %q", source)
	}
}

func TestMustRegisterFileModuleCallerSkipsWrapperFrames(t *testing.T) {
	source := helperFileModuleHereSource()

	previous := defaultFileModuleRegistry
	defaultFileModuleRegistry = NewFileModuleRegistry()
	defer func() {
		defaultFileModuleRegistry = previous
	}()

	helperMustRegisterFileModuleViaWrapper()

	if _, ok := defaultFileModuleRegistry.Lookup(source); !ok {
		t.Fatalf("expected wrapped module registration for %q", source)
	}
}
