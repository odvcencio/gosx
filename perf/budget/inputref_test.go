package budget

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInputDuplicateObjectsCannotBypassTypedValidation(t *testing.T) {
	root := t.TempDir()
	var toolchain Toolchain
	if err := json.Unmarshal(fixture(t, "toolchain"), &toolchain); err != nil {
		t.Fatal(err)
	}
	font := []byte("font fixture")
	digest := sha256.Sum256(font)
	toolchain.Fonts = []Ref{{File: "font.woff2", SHA256: hex.EncodeToString(digest[:])}}
	if err := os.WriteFile(filepath.Join(root, "font.woff2"), font, 0600); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(toolchain)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "toolchain.json")
	write := func(raw []byte) {
		t.Helper()
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(data)
	if _, err := LoadToolchain(path, LoadOptions{RootDir: root}); err != nil {
		t.Fatal("valid control rejected", err)
	}
	duplicate := bytes.Replace(data, []byte(`"platformArchives":`), []byte(`"platformArchives":{"windows-x64":"not-a-sha"},"platformArchives":`), 1)
	if bytes.Equal(data, duplicate) {
		t.Fatal("duplicate-object test did not alter its input")
	}
	write(duplicate)
	loaded, err := LoadToolchain(path, LoadOptions{RootDir: root})
	// Either rejecting duplicate keys or decoding only the validated last object
	// is safe. A typed merge must never retain the unvalidated first object's field.
	if err == nil && (loaded.PlatformArchives.WindowsX64 != toolchain.PlatformArchives.WindowsX64 || loaded.PlatformArchives.LinuxX64 != toolchain.PlatformArchives.LinuxX64) {
		t.Fatal("typed output retained an unvalidated archive digest")
	}
}
