package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
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
		t.Fatalf("%s is stale; regenerate it from runtimeContractTypeScript\n--- got ---\n%s\n--- want ---\n%s", contractPath, got, want)
	}
}

func TestGeneratedRuntimeContractCompilesAndErasesTypes(t *testing.T) {
	f := newFixture(t)
	rel := f.writeSource("runtime-abi.ts", runtimeContractTypeScript())
	built, err := buildBundle(f.dir, chunk("runtime-abi.js", rel), "esbuild", false)
	if err != nil {
		t.Fatalf("build generated runtime contract: %v", err)
	}
	for _, erased := range []string{"GoSXCoreExports", "alloc(size", "MailboxOpcodeAction", "INPUT_BATCH", "ENGINE_FRAME"} {
		if strings.Contains(built.code, erased) {
			t.Errorf("built contract kept TypeScript-only syntax %q", erased)
		}
	}
	if !strings.Contains(built.code, "__gosx_runtime_contract") {
		t.Fatal("built contract lost its runtime ABI version value")
	}
	if !strings.Contains(built.code, runtimewasm.ManifestIdentity()) {
		t.Fatal("built contract lost its manifest identity")
	}
	if !strings.Contains(built.code, "mailboxHeaderBytes") {
		t.Fatal("built contract lost its mailbox header size")
	}
	if !strings.Contains(built.code, "compatibilityVariants") {
		t.Fatal("built contract lost its non-advertised compatibility variants")
	}

	free, err := chunkFreeIdentifiers(f.dir, chunk("runtime-abi.js", rel))
	if err != nil {
		t.Fatalf("analyze generated runtime contract closure: %v", err)
	}
	if len(free) != 0 {
		t.Fatalf("generated runtime contract reports free identifiers %v", free)
	}
}

func TestRuntimeBundlesConsumeGeneratedContractBeforeABI(t *testing.T) {
	for _, outputName := range []string{"bootstrap.js", "bootstrap-runtime.js"} {
		var got []source
		for _, candidate := range outputs {
			if candidate.name == outputName {
				got = candidate.sources
				break
			}
		}
		contractIndex, abiIndex := -1, -1
		for i, src := range got {
			switch src.rel {
			case runtimeContractFile:
				contractIndex = i
			case runtimeABISupportFile:
				abiIndex = i
			}
		}
		if contractIndex < 0 || abiIndex < 0 || contractIndex >= abiIndex {
			t.Fatalf("%s contract/ABI source order = %d/%d", outputName, contractIndex, abiIndex)
		}
	}
}
