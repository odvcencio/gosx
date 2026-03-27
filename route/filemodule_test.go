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

func TestResolveFileModuleMatchesBuildRootShift(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "src", "app")
	distRoot := filepath.Join(t.TempDir(), "dist", "app")
	source := filepath.Join(sourceRoot, "demo", "verify", "page.gsx")

	registry := NewFileModuleRegistry()
	if err := registry.Register(FileModuleFor(source, FileModuleOptions{})); err != nil {
		t.Fatal(err)
	}

	page := FilePage{
		Source:   "demo/verify/page.gsx",
		FilePath: filepath.Join(distRoot, "demo", "verify", "page.gsx"),
	}
	module, ok := resolveFileModule(registry, distRoot, page)
	if !ok {
		t.Fatalf("expected moved build root lookup to resolve %q", source)
	}
	if module.Source != normalizeFileModuleSource(source) {
		t.Fatalf("expected module source %q, got %q", normalizeFileModuleSource(source), module.Source)
	}
}
