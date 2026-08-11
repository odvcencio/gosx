package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedRuntimeContractMatchesGoSchema(t *testing.T) {
	contractPath := filepath.Clean(filepath.Join(
		shippedClientJS(t),
		"..",
		"runtime",
		"generated",
		"runtime-abi.ts",
	))
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
	f := newFixture(t)
	rel := f.writeSource("runtime-abi.ts", runtimeContractTypeScript())
	built, err := buildBundle(f.dir, chunk("runtime-abi.js", rel), "esbuild", false)
	if err != nil {
		t.Fatalf("build generated runtime contract: %v", err)
	}
	for _, erased := range []string{"interface GoSXCoreExports", "interface GoSXRuntimeManifestV1", "GoSXFeatureMask"} {
		if strings.Contains(built.code, erased) {
			t.Errorf("built contract kept TypeScript-only syntax %q", erased)
		}
	}
	if !strings.Contains(built.code, "GOSX_RUNTIME_ABI_VERSION") {
		t.Fatal("built contract lost its runtime ABI version value")
	}

	free, err := chunkFreeIdentifiers(f.dir, chunk("runtime-abi.js", rel))
	if err != nil {
		t.Fatalf("analyze generated runtime contract closure: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("generated runtime contract reports free identifiers %v", free)
	}
}
