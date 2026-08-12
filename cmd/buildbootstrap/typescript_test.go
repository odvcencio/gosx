package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

func TestESBuildLoaderFollowsSourceExtensions(t *testing.T) {
	tests := []struct {
		name  string
		entry output
		want  esbuild.Loader
	}{
		{name: "javascript", entry: chunk("runtime.js", "bootstrap-src/runtime.js"), want: esbuild.LoaderJS},
		{name: "typescript", entry: chunk("runtime.js", "bootstrap-src/runtime.ts"), want: esbuild.LoaderTS},
		{name: "mixed", entry: chunk("runtime.js", "bootstrap-src/head.js", "bootstrap-src/runtime.ts"), want: esbuild.LoaderTS},
		{name: "tsx", entry: chunk("runtime.js", "bootstrap-src/view.tsx"), want: esbuild.LoaderTSX},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := esbuildLoaderForOutput(test.entry)
			if err != nil {
				t.Fatalf("esbuildLoaderForOutput: %v", err)
			}
			if got != test.want {
				t.Fatalf("loader = %v, want %v", got, test.want)
			}
		})
	}

	if _, err := esbuildLoaderForOutput(chunk("runtime.js", "bootstrap-src/runtime.jsx")); err == nil {
		t.Fatal("esbuildLoaderForOutput accepted an unsupported extension")
	}
}

func TestBuildBundleAcceptsTypeScriptAndKeepsOriginalMapIdentity(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-runtime-contract.ts", `import type { ExternalContract } from "./external-contract";
export interface RuntimeContract extends ExternalContract {
  version: number;
}
function contractVersion(contract: RuntimeContract): number {
  return contract.version;
}
const contract = { version: 1 } as RuntimeContract;
globalThis.__gosx_contract_version = contractVersion(contract);
`)

	built, err := buildBundle(f.dir, chunk("typed-runtime.js", rel), "esbuild", false)
	if err != nil {
		t.Fatalf("buildBundle: %v", err)
	}
	for _, erased := range []string{"interface RuntimeContract", "ExternalContract", "import type"} {
		if strings.Contains(built.code, erased) {
			t.Errorf("built JavaScript kept TypeScript-only syntax %q", erased)
		}
	}
	if !strings.Contains(built.code, "__gosx_contract_version") {
		t.Fatal("built JavaScript lost the runtime contract assignment")
	}

	var parsed struct {
		Sources        []string `json:"sources"`
		SourcesContent []string `json:"sourcesContent"`
	}
	if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
		t.Fatalf("decode source map: %v", err)
	}
	if len(parsed.Sources) != 1 || parsed.Sources[0] != rel {
		t.Fatalf("source map sources = %v, want [%s]", parsed.Sources, rel)
	}
	if len(parsed.SourcesContent) != 1 || !strings.Contains(parsed.SourcesContent[0], "interface RuntimeContract") {
		t.Fatalf("source map lost the original TypeScript source: %v", parsed.SourcesContent)
	}
}

func TestChunkClosureErasesTypesButStillFindsMissingRuntimeValues(t *testing.T) {
	f := newFixture(t)
	closed := f.writeSource("10-closed.ts", `interface RuntimeInput { value: number }
function normalize(input: RuntimeInput): number { return input.value + 1; }
const input: RuntimeInput = { value: 41 };
globalThis.__gosx_typed_result = normalize(input);
`)
	free, err := chunkFreeIdentifiers(f.dir, chunk("closed.js", closed))
	if err != nil {
		t.Fatalf("closed TypeScript chunk: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("closed TypeScript chunk reports free identifiers %v", free)
	}

	broken := f.writeSource("20-broken.ts", `const result: number = missingRuntimeValue();
globalThis.__gosx_typed_result = result;
`)
	free, err = chunkFreeIdentifiers(f.dir, chunk("broken.js", broken))
	if err != nil {
		t.Fatalf("broken TypeScript chunk: %v", err)
	}
	if len(free) != 1 || free[0] != "missingRuntimeValue" {
		t.Fatalf("broken TypeScript chunk reports %v, want [missingRuntimeValue]", free)
	}
}

func TestChunkClosureUnderstandsTypeOnlyImportsAndValueExports(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-module.ts", `import type { ExternalContract } from "./external-contract";
export interface RuntimeContract extends ExternalContract { version: number }
export function contractVersion(contract: RuntimeContract): number {
  return contract.version;
}
`)

	free, err := chunkFreeIdentifiers(f.dir, chunk("module.js", rel))
	if err != nil {
		t.Fatalf("module-shaped TypeScript chunk: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("module-shaped TypeScript chunk reports free identifiers %v", free)
	}
}

func TestGotreesitterTypeScriptDiagnosticCarriesSourceRange(t *testing.T) {
	err := validateTypedSource(
		sourceFile("bootstrap-src/broken.ts"),
		[]byte("const value: number = @;\n"),
	)
	if err == nil {
		t.Fatal("validateTypedSource accepted invalid TypeScript")
	}
	for _, want := range []string{
		"bootstrap-src/broken.ts:1:",
		"-1:",
		"TypeScript syntax error",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("diagnostic does not contain %q: %v", want, err)
		}
	}
}

func TestTdewolffPathErasesTypeScriptBeforeMinifying(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-typed.ts", `interface RuntimeValue { count: number }
const value: RuntimeValue = { count: 2 };
globalThis.__gosx_typed_count = value.count;
`)

	built, err := buildBundle(f.dir, chunk("typed.js", rel), "tdewolff", false)
	if err != nil {
		t.Fatalf("buildBundle with tdewolff: %v", err)
	}
	if strings.Contains(built.code, "interface RuntimeValue") || strings.Contains(built.code, ": RuntimeValue") {
		t.Fatalf("tdewolff bundle kept TypeScript syntax: %s", built.code)
	}
}

func TestShippedRuntimeWASMLeavesAreTypedAndBuildInTheirOwners(t *testing.T) {
	dir := shippedClientJS(t)
	leaves := []struct {
		rel    string
		marker string
		global string
	}{
		{rel: "../runtime/wasm/abi.ts", marker: "function selectVariant", global: "__gosx_runtime_abi_support"},
		{rel: "../runtime/wasm/mailbox.ts", marker: "function decodePatchMailbox", global: "__gosx_runtime_mailbox"},
		{rel: "../runtime/wasm/loader.ts", marker: "async function load", global: "__gosx_runtime_wasm_loader"},
	}
	for _, leaf := range leaves {
		leaf := leaf
		t.Run(filepath.Base(leaf.rel), func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(leaf.rel)))
			if err != nil {
				t.Fatalf("read shipped runtime WASM leaf %s: %v", leaf.rel, err)
			}
			if !strings.Contains(string(body), leaf.marker) || !strings.Contains(string(body), leaf.global) {
				t.Fatalf("%s is missing its typed runtime marker/global", leaf.rel)
			}
			for _, name := range []string{"bootstrap.js", "bootstrap-runtime.js"} {
				var entry output
				for _, candidate := range outputs {
					if candidate.name == name {
						entry = candidate
						break
					}
				}
				if entry.name == "" {
					t.Fatalf("chunk table has no %s", name)
				}
				built, err := buildBundle(dir, entry, "esbuild", false)
				if err != nil {
					t.Fatalf("build %s with typed runtime WASM leaf %s: %v", name, leaf.rel, err)
				}
				if strings.Contains(built.code, "@typedef {object} GoSXRuntime") {
					t.Errorf("%s retained source-only runtime ABI JSDoc", name)
				}
				var parsed struct {
					Sources []string `json:"sources"`
				}
				if err := json.Unmarshal([]byte(built.m), &parsed); err != nil {
					t.Fatalf("decode %s source map: %v", name, err)
				}
				found := false
				for _, source := range parsed.Sources {
					if source == leaf.rel {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("%s source map does not retain %s", name, leaf.rel)
				}
			}
		})
	}
}
