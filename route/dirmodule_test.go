package route

import (
	"path/filepath"
	"testing"
)

func TestMustRegisterDirModuleCallerSkipsWrapperFrames(t *testing.T) {
	source := helperDirModuleHereSource()

	previous := defaultDirModuleRegistry
	defaultDirModuleRegistry = NewDirModuleRegistry()
	defer func() {
		defaultDirModuleRegistry = previous
	}()

	helperMustRegisterDirModuleViaWrapper()

	if _, ok := defaultDirModuleRegistry.Lookup(source); !ok {
		t.Fatalf("expected dir module registration for %q", source)
	}
}

func TestLookupDirModuleMatchesBuildRootShift(t *testing.T) {
	sourceRoot := filepath.Join(t.TempDir(), "src", "app")
	distRoot := filepath.Join(t.TempDir(), "dist", "app")

	registry := NewDirModuleRegistry()
	for _, source := range []string{
		sourceRoot,
		filepath.Join(sourceRoot, "docs"),
	} {
		if err := registry.Register(DirModuleFor(source, DirModuleOptions{})); err != nil {
			t.Fatal(err)
		}
	}

	if _, ok := lookupDirModule(registry, distRoot, ""); !ok {
		t.Fatalf("expected root dir module to resolve from moved build root")
	}
	if _, ok := lookupDirModule(registry, distRoot, "docs"); !ok {
		t.Fatalf("expected nested dir module to resolve from moved build root")
	}
}
