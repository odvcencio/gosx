package main

import "testing"

func TestTinyGoBuildArgsUseSlimRuntimeByDefault(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "")

	args := tinyGoBuildArgs("runtime.wasm")
	if !stringSliceContains(args, "-tags=gosx_tiny_runtime") {
		t.Fatalf("expected slim TinyGo runtime tag in args: %v", args)
	}
	if !stringSliceContains(args, "-no-debug") {
		t.Fatalf("expected -no-debug in args: %v", args)
	}
	if !stringSliceContains(args, "-panic=trap") {
		t.Fatalf("expected -panic=trap in args: %v", args)
	}
}

func TestTinyGoBuildArgsCanKeepFullRuntime(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "1")

	args := tinyGoBuildArgs("runtime.wasm")
	if stringSliceContains(args, "-tags=gosx_tiny_runtime") {
		t.Fatalf("did not expect slim TinyGo runtime tag in full runtime args: %v", args)
	}
	if !stringSliceContains(args, "-no-debug") {
		t.Fatalf("expected -no-debug in args: %v", args)
	}
	if !stringSliceContains(args, "-panic=trap") {
		t.Fatalf("expected -panic=trap in args: %v", args)
	}
}

func TestTinyGoBuildArgsAppendVariantTags(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "")

	args := tinyGoBuildArgs("runtime.wasm", "gosx_tiny_islands_only")
	if !stringSliceContains(args, "-tags=gosx_tiny_runtime gosx_tiny_islands_only") {
		t.Fatalf("expected combined TinyGo runtime tags in args: %v", args)
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
