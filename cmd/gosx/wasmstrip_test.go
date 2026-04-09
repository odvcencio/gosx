package main

import (
	"os"
	"testing"
)

func TestWasmExternalizeData(t *testing.T) {
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
