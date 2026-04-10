package main

import (
	"os"
	"testing"
)

// TestWasmExternalizeData is a developer scratch test that measures
// the size split of externalizing WASM data sections. It relies on
// /tmp/stripped.wasm being provided manually and is skipped in CI.
func TestWasmExternalizeData(t *testing.T) {
	if _, err := os.Stat("/tmp/stripped.wasm"); os.IsNotExist(err) {
		t.Skip("skipping: /tmp/stripped.wasm not present (manual dev test)")
	}
	err := wasmExternalizeData("/tmp/stripped.wasm", "/tmp/extern.wasm", "/tmp/extern.data")
	if err != nil {
		t.Fatal(err)
	}
	origInfo, _ := os.Stat("/tmp/stripped.wasm")
	wasmInfo, _ := os.Stat("/tmp/extern.wasm")
	dataInfo, _ := os.Stat("/tmp/extern.data")
	t.Logf("Original: %d KB", origInfo.Size()/1024)
	t.Logf("Stripped WASM: %d KB", wasmInfo.Size()/1024)
	t.Logf("External data: %d KB", dataInfo.Size()/1024)
	t.Logf("Combined: %d KB", (wasmInfo.Size()+dataInfo.Size())/1024)
}
