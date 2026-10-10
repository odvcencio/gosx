package buildmanifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoWASMManifestResolutionAndValidation(t *testing.T) {
	if err := (*Manifest)(nil).ValidateGoWASMAssets(); err == nil {
		t.Fatal("nil manifest accepted")
	}
	valid := HashedAsset{File: "controls.0123456789abcdef.wasm", Hash: "0123456789abcdef", Size: 42}
	m := &Manifest{GoWASM: map[string]HashedAsset{"controls": valid}}
	if err := m.ValidateGoWASMAssets(); err != nil {
		t.Fatal(err)
	}
	if got := m.GoWASMURL("/.proxy/gosx/assets", "controls"); got != "/.proxy/gosx/assets/go-wasm/"+valid.File {
		t.Fatal(got)
	}
	if m.GoWASMURL("/gosx/assets", "missing") != "" || (*Manifest)(nil).GoWASMURL("", "controls") != "" {
		t.Fatal("missing module resolved")
	}
	for _, bad := range []HashedAsset{
		{File: "../controls.0123456789abcdef.wasm", Hash: valid.Hash, Size: 42},
		{File: "controls.hash.wasm", Hash: "hash", Size: 42},
		{File: valid.File, Hash: valid.Hash, Size: 0},
	} {
		m.GoWASM["controls"] = bad
		data, _ := json.Marshal(m)
		p := filepath.Join(t.TempDir(), "build.json")
		if err := os.WriteFile(p, data, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(p); err == nil || m.GoWASMURL("/gosx/assets", "controls") != "" {
			t.Fatalf("malformed module accepted: %+v", bad)
		}
	}
	data, err := json.Marshal(&Manifest{})
	if err != nil || strings.Contains(string(data), "goWASM") {
		t.Fatal("unconfigured manifest changed")
	}
}
