package main

import (
	"encoding/json"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"
	"github.com/odvcencio/gotreesitter/grammars"
)

func requireTypedGrammars(t *testing.T) {
	t.Helper()
	if grammars.TypescriptLanguage() == nil || grammars.TsxLanguage() == nil {
		t.Skip("gotreesitter TypeScript/TSX grammars require grammar_subset_typescript and grammar_subset_tsx tags")
	}
}

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
	if _, err := esbuildLoaderForOutput(chunk(
		"runtime.js",
		"bootstrap-src/assertion.ts",
		"bootstrap-src/view.tsx",
	)); err == nil {
		t.Fatal("esbuildLoaderForOutput accepted mixed TypeScript and TSX sources")
	}
}

func TestBuildBundleAcceptsTypeScriptAndKeepsOriginalMapIdentity(t *testing.T) {
	requireTypedGrammars(t)
	f := newFixture(t)
	rel := f.writeSource("10-runtime-contract.ts", `interface ExternalContract {
  name?: string;
}
interface RuntimeContract extends ExternalContract {
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
	for _, erased := range []string{"interface RuntimeContract", "interface ExternalContract"} {
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
	requireTypedGrammars(t)
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

func TestTypedBuildRejectsModuleSyntaxBeforeTranspile(t *testing.T) {
	requireTypedGrammars(t)
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "value import",
			body: "import { value } from './dep';\nglobalThis.__gosx_value = value;\n",
			want: `module syntax "import_statement"`,
		},
		{
			name: "type import",
			body: "import type { Contract } from './dep';\nconst value: number = 1;\n",
			want: `module syntax "import_statement"`,
		},
		{
			name: "value export",
			body: "export function contractVersion(): number { return 1; }\n",
			want: `module syntax "export_statement"`,
		},
		{
			name: "type export",
			body: "export interface RuntimeContract { version: number }\n",
			want: `module syntax "export_statement"`,
		},
		{
			name: "type alias export",
			body: "export type RuntimeContract = { version: number };\n",
			want: `module syntax "export_statement"`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateTypedSource(sourceFile("bootstrap-src/module.ts"), []byte(test.body))
			if err == nil {
				t.Fatal("validateTypedSource accepted module syntax")
			}
			for _, want := range []string{"bootstrap-src/module.ts:1:1", test.want, "classic-script"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("diagnostic does not contain %q: %v", want, err)
				}
			}
		})
	}
}

func TestChunkClosureRejectsTypeOnlyImportsAndValueExports(t *testing.T) {
	requireTypedGrammars(t)
	f := newFixture(t)
	rel := f.writeSource("10-module.ts", `import type { ExternalContract } from "./external-contract";
export interface RuntimeContract extends ExternalContract { version: number }
export function contractVersion(contract: RuntimeContract): number {
  return contract.version;
}
`)

	_, err := chunkFreeIdentifiers(f.dir, chunk("module.js", rel))
	if err == nil {
		t.Fatal("module-shaped TypeScript chunk passed closure analysis")
	}
	if !strings.Contains(err.Error(), "bootstrap-src/10-module.ts:1:1") ||
		!strings.Contains(err.Error(), "classic-script") {
		t.Fatalf("module-shaped TypeScript diagnostic is not precise: %v", err)
	}
}

func TestGotreesitterTypeScriptDiagnosticCarriesSourceRange(t *testing.T) {
	requireTypedGrammars(t)
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
	requireTypedGrammars(t)
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

func TestMixedTypeScriptAndTSXChunksFailClosed(t *testing.T) {
	requireTypedGrammars(t)
	f := newFixture(t)
	ts := f.writeSource("10-assertion.ts", `interface RuntimeValue { count: number }
const raw: unknown = { count: 2 };
const value = <RuntimeValue>raw;
globalThis.__gosx_typed_count = value.count;
`)
	tsx := f.writeSource("11-view.tsx", `type Props = { value: number };
const view = <span data-value={1} />;
globalThis.__gosx_view = view;
`)

	_, err := buildBundle(f.dir, chunk("mixed.js", ts, tsx), "esbuild", false)
	if err == nil {
		t.Fatal("buildBundle accepted a mixed TypeScript and TSX chunk")
	}
	for _, want := range []string{"mixed.js", "mixes TypeScript and TSX", "bootstrap-src/10-assertion.ts", "bootstrap-src/11-view.tsx"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("diagnostic does not contain %q: %v", want, err)
		}
	}
}

func TestClassicScriptTopLevelThisSurvivesTypeErasure(t *testing.T) {
	requireTypedGrammars(t)
	f := newFixture(t)
	rel := f.writeSource("10-top-level-this.ts", `interface RuntimeGlobal { marker?: string }
const runtimeThis = this as RuntimeGlobal;
globalThis.__gosx_runtime_this = runtimeThis;
`)

	built, err := buildBundle(f.dir, chunk("top-level-this.js", rel), "esbuild", false)
	if err != nil {
		t.Fatalf("buildBundle: %v", err)
	}
	if strings.Contains(built.code, "export") || strings.Contains(built.code, "import") {
		t.Fatalf("built classic script contains module syntax: %s", built.code)
	}
	if !strings.Contains(built.code, "this") {
		t.Fatalf("built classic script lost top-level this: %s", built.code)
	}
}
