package route

import "testing"

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
