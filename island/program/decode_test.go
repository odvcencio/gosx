package program

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestFixturesRoundTrip is the wire-format contract test required by ADR 0001.
// Each fixture is decoded, re-encoded, and re-decoded — the second encoding
// must match the first byte-for-byte (modulo whitespace). A failure here is
// the signal that a v2 envelope is required (see ADR 0001 §"Test contract").
func TestFixturesRoundTrip(t *testing.T) {
	entries, err := os.ReadDir("testdata/fixtures")
	if err != nil {
		t.Fatalf("readdir fixtures: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no fixtures found; Task 4.1 must seed at least three")
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata/fixtures", name))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			var p Program
			if err := json.Unmarshal(data, &p); err != nil {
				t.Fatalf("decode: %v", err)
			}
			out, err := json.Marshal(&p)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			// Re-decode the re-encoded form; structural equality only.
			var p2 Program
			if err := json.Unmarshal(out, &p2); err != nil {
				t.Fatalf("re-decode: %v", err)
			}
			// Compare canonical encodings (normalizes key order, whitespace).
			canonA, _ := json.Marshal(&p)
			canonB, _ := json.Marshal(&p2)
			if string(canonA) != string(canonB) {
				t.Errorf("round-trip mismatch:\n got: %s\nwant: %s", canonB, canonA)
			}
		})
	}
}

func TestDecodeProgramJSONInjectsSurfaceDOM(t *testing.T) {
	data := []byte(`{"nodes":[],"exprs":[]}`)
	p, err := DecodeProgramJSON(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Surface != SurfaceDOM {
		t.Errorf("Surface = %v, want SurfaceDOM", p.Surface)
	}
}
