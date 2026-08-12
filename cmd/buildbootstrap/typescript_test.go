package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestLanguageForSourceClassifiesByExtension replaces the former
// TestESBuildLoaderFollowsSourceExtensions. That test proved a whole chunk's
// loader followed its highest-ranked source's extension — the exact
// whole-chunk promotion that reparsed a neighboring .js source as
// TypeScript. Each source now transpiles with its own loader (see
// transpileSource), so the only per-extension question left is
// classification.
func TestLanguageForSourceClassifiesByExtension(t *testing.T) {
	tests := []struct {
		rel  string
		want sourceLanguage
	}{
		{"bootstrap-src/runtime.js", sourceJavaScript},
		{"bootstrap-src/runtime.mjs", sourceJavaScript},
		{"bootstrap-src/runtime.cjs", sourceJavaScript},
		{"bootstrap-src/runtime.ts", sourceTypeScript},
		{"bootstrap-src/runtime.mts", sourceTypeScript},
		{"bootstrap-src/runtime.cts", sourceTypeScript},
	}
	for _, test := range tests {
		t.Run(test.rel, func(t *testing.T) {
			got, err := languageForSource(sourceFile(test.rel))
			if err != nil {
				t.Fatalf("languageForSource: %v", err)
			}
			if got != test.want {
				t.Fatalf("language = %v, want %v", got, test.want)
			}
		})
	}

	// TSX is not a supported extension yet: the bootstrap runtime configures
	// no JSX factory, so an esbuild TSX transform would emit
	// React.createElement calls into a bundle that ships no React. It
	// returns once a JSX factory is configured.
	for _, rel := range []string{"bootstrap-src/view.jsx", "bootstrap-src/view.tsx", "bootstrap-src/notes.txt"} {
		if _, err := languageForSource(sourceFile(rel)); err == nil {
			t.Fatalf("languageForSource(%s) accepted an unsupported extension", rel)
		}
	}
}

// TestJavaScriptSourceBesideTypeScriptSourceKeepsComparisonOperators is the
// regression the reviewer proved against esbuild v0.28.1: parsing a .js
// source with the TypeScript loader silently rewrites the comparison chain
// `a < b > (c)` into the generic-argument call `a<b>(c)`, and type erasure
// then drops it to the call `a(c)`, losing b. Once a typed source shares a
// chunk with a JavaScript source, the JavaScript source must transpile with
// its own loader and never reach the TypeScript parser.
func TestJavaScriptSourceBesideTypeScriptSourceKeepsComparisonOperators(t *testing.T) {
	f := newFixture(t)
	jsRel := f.writeSource("10-comparison.js", "const r = a < b > (c);\n")
	tsRel := f.writeSource("20-typed.ts", `interface RuntimeValue { count: number }
const value: RuntimeValue = { count: 2 };
globalThis.__gosx_typed_count = value.count;
`)

	built, err := buildBundle(f.dir, chunk("mixed.js", jsRel, tsRel), "esbuild", false)
	if err != nil {
		t.Fatalf("buildBundle: %v", err)
	}
	if strings.Contains(built.code, "interface RuntimeValue") {
		t.Error("built code kept TypeScript-only syntax from the neighboring .ts source")
	}
	if strings.Contains(built.code, "a(c)") {
		t.Fatalf("the .js source parsed as TypeScript: `a < b > (c)` collapsed into the call `a(c)`, dropping b: %s", built.code)
	}
	if !strings.Contains(built.code, "a<b>c") {
		t.Fatalf("the .js source lost its comparison operators: %s", built.code)
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
