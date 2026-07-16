package main

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveWASMCompilerRequiresTinyGoForProduction(t *testing.T) {
	compiler, path, err := resolveWASMCompiler(BuildOptions{Dev: false}, func(string) (string, error) {
		return "", errors.New("not found")
	})
	if err == nil {
		t.Fatal("expected production build to require TinyGo")
	}
	if compiler != "" || path != "" {
		t.Fatalf("expected no compiler/path on failure, got %q %q", compiler, path)
	}
	if !strings.Contains(err.Error(), "production GoSX builds require TinyGo") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveWASMCompilerKeepsGoForDevBuilds(t *testing.T) {
	compiler, path, err := resolveWASMCompiler(BuildOptions{Dev: true}, func(string) (string, error) {
		t.Fatal("dev builds should not look for TinyGo")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if compiler != wasmCompilerGo || path != "" {
		t.Fatalf("expected dev Go compiler, got %q path=%q", compiler, path)
	}
}

func TestResolveWASMCompilerUsesTinyGoForProduction(t *testing.T) {
	compiler, path, err := resolveWASMCompiler(BuildOptions{Dev: false}, func(name string) (string, error) {
		if name != "tinygo" {
			t.Fatalf("unexpected lookup %q", name)
		}
		return "/usr/local/bin/tinygo", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if compiler != wasmCompilerTinyGo || path != "/usr/local/bin/tinygo" {
		t.Fatalf("expected TinyGo compiler, got %q path=%q", compiler, path)
	}
}

func TestTinyGoBuildArgsUseSlimRuntimeByDefault(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "")

	args := tinyGoBuildArgs("runtime.wasm")
	if !stringSliceContains(args, "-tags=tinygo gosx_tiny_runtime") {
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
	if !stringSliceContains(args, "-tags=tinygo") {
		t.Fatalf("expected explicit TinyGo build tag in full runtime args: %v", args)
	}
	if stringSliceContains(args, "-tags=tinygo gosx_tiny_runtime") {
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
	if !stringSliceContains(args, "-tags=tinygo gosx_tiny_runtime gosx_tiny_islands_only") {
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
	if !stringSliceContains(args, "m31labs.dev/gosx/client/wasm") {
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

func TestTinyGoWASMDependencyClosurePrunesHostShaderCompiler(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "")

	packages, modules, err := tinyGoWASMDependencyClosure(".", tinyGoWASMTags()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range packages {
		if pkg.ImportPath == "m31labs.dev/gosx/scene" {
			t.Fatalf("TinyGo WASM closure unexpectedly includes host scene package: %+v", pkg)
		}
	}
	for _, module := range modules {
		if module.Path == "m31labs.dev/selena" {
			t.Fatalf("TinyGo WASM closure unexpectedly includes Selena: %+v", module)
		}
	}
}

// TestTinyGoWASMDependencyClosureExcludesGoTreeSitterAndGob is a regression
// pin for the R2 "unreachable" trap that fired on every /admin/editor load
// in production (sassafras / handoff-26-wasm-trap investigation).
//
// Root cause: engine/surface/lowering.go and ir/lower.go had no build
// constraint excluding them from the WASM client, even though both are
// host/build-time-only (their only callers — engine/surface/discover.go,
// compile.go, lsp/symbols.go — are all already `!js`/`!tinygo`). Importing
// either pulled github.com/odvcencio/gotreesitter (and its encoding/gob
// dependency, used by its embedded-grammar loader) into every TinyGo build
// of client/wasm. TinyGo's internal/reflectlite has a known gap —
// AssignableTo/Implements against a non-empty interface panics
// ("reflect: unimplemented: AssignableTo with interface") — that gob's
// type-info machinery (mustGetTypeInfo -> userType -> implementsInterface)
// trips during WASM boot, before any hydrate call. Combined with this
// build's `-panic=trap` TinyGo flag, that unrecoverable panic silently
// compiled to a bare `unreachable` WASM trap on every production page load.
//
// This test would have failed red before the fix (gotreesitter present as
// an external module dependency of the TinyGo client/wasm closure) and is
// green after (see engine/surface/lowering.go and ir/lower.go's
// `!js`/`!tinygo` tags). tinyGoWASMDependencyClosure separates in-module
// gosx packages from external modules — gotreesitter is a third-party
// module (github.com/odvcencio/gotreesitter), so it shows up in the
// `modules` return value, not `packages` (which only reflects packages
// within m31labs.dev/gosx itself — see TestTinyGoWASMDependencyClosure
// PrunesHostShaderCompiler's m31labs.dev/selena check for the same
// pattern). encoding/gob is Go standard library, which
// tinyGoWASMDependencyClosure filters out of both return values entirely
// (`pkg.Standard` short-circuit) — gotreesitter's absence is what actually
// matters here, since gob only entered the graph transitively through it.
func TestTinyGoWASMDependencyClosureExcludesGoTreeSitterAndGob(t *testing.T) {
	t.Setenv("GOSX_TINYGO_FULL_RUNTIME", "")

	_, modules, err := tinyGoWASMDependencyClosure(".", tinyGoWASMTags()...)
	if err != nil {
		t.Fatal(err)
	}
	for _, module := range modules {
		if module.Path == "github.com/odvcencio/gotreesitter" {
			t.Fatalf("TinyGo WASM closure unexpectedly includes gotreesitter (R2 unreachable-trap regression — it drags in encoding/gob, which trips TinyGo's internal/reflectlite AssignableTo-with-interface gap): %+v", module)
		}
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
