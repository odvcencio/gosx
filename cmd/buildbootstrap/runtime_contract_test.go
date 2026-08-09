package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

func TestGeneratedRuntimeContractMatchesGoSchema(t *testing.T) {
	contractPath := runtimeContractPath(shippedClientJS(t))
	got, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read generated runtime contract: %v", err)
	}
	want := []byte(runtimeContractTypeScript())
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is stale; regenerate it from runtimeContractTypeScript", contractPath)
	}
}

func TestGeneratedRuntimeContractCompilesAndErasesTypes(t *testing.T) {
	result := esbuild.Transform(runtimeContractTypeScript(), esbuild.TransformOptions{
		Charset:       esbuild.CharsetUTF8,
		Format:        esbuild.FormatESModule,
		LegalComments: esbuild.LegalCommentsNone,
		Loader:        esbuild.LoaderTS,
		Sourcefile:    "runtime-abi.ts",
		Target:        esbuild.ES2020,
	})
	if len(result.Errors) > 0 {
		t.Fatalf("build generated runtime contract: %s", result.Errors[0].Text)
	}
	code := string(result.Code)
	for _, erased := range []string{"interface GoSXCoreExports", "interface GoSXRuntimeManifestV1", "GoSXFeatureMask"} {
		if strings.Contains(code, erased) {
			t.Errorf("built contract kept TypeScript-only syntax %q", erased)
		}
	}
	if !strings.Contains(code, "GOSX_RUNTIME_ABI_VERSION") {
		t.Fatal("built contract lost its runtime ABI version value")
	}
}

func TestRunWritesRuntimeContractAndCheckCatchesDrift(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("10-runtime.js", "globalThis.__gosx_runtime_contract_fixture = 1;\n")
	useOutputs(t, chunk("runtime.js", rel))

	if err := runTool(t, "-dir", f.dir); err != nil {
		t.Fatalf("build fixture: %v", err)
	}
	contractPath := runtimeContractPath(f.dir)
	got, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read generated runtime contract: %v", err)
	}
	if !bytes.Equal(got, []byte(runtimeContractTypeScript())) {
		t.Fatalf("generated runtime contract differs from schema")
	}
	if err := runTool(t, "-dir", f.dir, "--check"); err != nil {
		t.Fatalf("--check reports stale after generation: %v", err)
	}

	if err := os.WriteFile(contractPath, []byte("// stale\n"), 0o644); err != nil {
		t.Fatalf("damage runtime contract: %v", err)
	}
	err = runTool(t, "-dir", f.dir, "--check")
	if err == nil {
		t.Fatal("--check passed with a stale runtime contract")
	}
	if !strings.Contains(err.Error(), "../runtime/generated/runtime-abi.ts") {
		t.Fatalf("--check does not name the stale runtime contract: %v", err)
	}
	got, err = os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read stale runtime contract: %v", err)
	}
	if string(got) != "// stale\n" {
		t.Fatalf("--check rewrote the runtime contract")
	}

	if err := os.Remove(contractPath); err != nil {
		t.Fatalf("remove runtime contract: %v", err)
	}
	err = runTool(t, "-dir", f.dir, "--check")
	if err == nil {
		t.Fatal("--check passed with a missing runtime contract")
	}
	if !strings.Contains(err.Error(), "../runtime/generated/runtime-abi.ts") {
		t.Fatalf("--check does not name the missing runtime contract: %v", err)
	}
}
