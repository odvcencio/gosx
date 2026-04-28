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

func TestGoWASMBuildArgsAppendVariantTags(t *testing.T) {
	args := goWASMBuildArgs("runtime-islands.wasm", islandOnlyWASMTags(wasmCompilerGo)...)
	if !stringSliceContains(args, "-trimpath") {
		t.Fatalf("expected trimpath in args: %v", args)
	}
	if !stringSliceContains(args, "-ldflags=-s -w") {
		t.Fatalf("expected stripped linker flags in args: %v", args)
	}
	if !stringSliceContains(args, "-tags=gosx_tiny_runtime gosx_tiny_islands_only") {
		t.Fatalf("expected combined Go runtime tags in args: %v", args)
	}
	if !stringSliceContains(args, "github.com/odvcencio/gosx/client/wasm") {
		t.Fatalf("expected wasm package in args: %v", args)
	}
}

func TestTinyGoIslandOnlyTagsRelyOnTinyRuntimeDefault(t *testing.T) {
	args := islandOnlyWASMTags(wasmCompilerTinyGo)
	if stringSliceContains(args, "gosx_tiny_runtime") {
		t.Fatalf("did not expect duplicate TinyGo slim runtime tag: %v", args)
	}
	if !stringSliceContains(args, "gosx_tiny_islands_only") {
		t.Fatalf("expected islands-only tag: %v", args)
	}
}

func TestStandardGoWASMOptIsOptIn(t *testing.T) {
	t.Setenv("GOSX_GO_WASM_OPT", "")
	if standardGoWASMOptEnabled() {
		t.Fatal("did not expect standard Go wasm-opt by default")
	}
	t.Setenv("GOSX_GO_WASM_OPT", "1")
	if !standardGoWASMOptEnabled() {
		t.Fatal("expected standard Go wasm-opt when enabled")
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
