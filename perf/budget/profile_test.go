package budget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	root, err := inputRoot("testdata/profile.v1.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func changedInput(t *testing.T, name string, edit func(map[string]any)) string {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(fixture(t, name), &value); err != nil {
		t.Fatal(err)
	}
	edit(value)
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "input.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProfileContract(t *testing.T) {
	p, err := LoadProfile("testdata/profile.v1.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Networks.Slow4G.DownBytesPerSec != 204800 || p.Networks.P75.DownBytesPerSec != 1125000 || p.Networks.Slow4G.RTTMicros != 150000 || p.Reference != "desktop-cpu-proxy" || p.BenchmarkIndexTarget != nil {
		t.Fatalf("wrong profile: %+v", p)
	}
	for name, edit := range map[string]func(map[string]any){
		"byte-unit":            func(v map[string]any) { v["quantumBytes"] = 1000 },
		"unknown-unit":         func(v map[string]any) { v["rttMillis"] = 150 },
		"nested-unknown":       func(v map[string]any) { v["networks"].(map[string]any)["slow4g"].(map[string]any)["downMbps"] = 1.6 },
		"missing-required":     func(v map[string]any) { delete(v, "setupRtts") },
		"null-required":        func(v map[string]any) { v["setupRtts"] = nil },
		"zero-bandwidth":       func(v map[string]any) { v["networks"].(map[string]any)["p75"].(map[string]any)["downBytesPerSec"] = 0 },
		"fractional-cpu":       func(v map[string]any) { v["cpuMultiplierMilli"] = 4000.5 },
		"phone-without-target": func(v map[string]any) { v["reference"] = "phone-4gb" },
	} {
		t.Run(name, func(t *testing.T) {
			path := changedInput(t, "profile", edit)
			if _, err := LoadProfile(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
				t.Fatal("invalid profile accepted")
			}
		})
	}
	path := changedInput(t, "profile", func(v map[string]any) {
		v["reference"] = "phone-4gb"
		v["benchmarkIndexTarget"] = 100
		v["cpuMultiplierMilli"] = 4250
	})
	if p, err := LoadProfile(path, LoadOptions{RootDir: filepath.Dir(path)}); err != nil || p.CPUMultiplierMilli != 4250 {
		t.Fatalf("fractional multiplier rejected: %v", err)
	}
}

func TestInputReferenceConfinement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "profile.json")
	data := fixture(t, "profile")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	ref := Ref{File: "profile.json", SHA256: hex.EncodeToString(digest[:])}
	if _, err := readReference(root, ref, maxInputBytes); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/profile.json", "../profile.json", "nested/../profile.json", "./profile.json", "C:/profile.json", `nested\profile.json`} {
		bad := ref
		bad.File = path
		if _, err := readReference(root, bad, maxInputBytes); err == nil {
			t.Fatalf("escape accepted: %q", path)
		}
	}
	bad := ref
	bad.SHA256 = strings.Repeat("f", 64)
	if _, err := readReference(root, bad, maxInputBytes); err == nil {
		t.Fatal("wrong hash accepted")
	}
	if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReference(root, ref, maxInputBytes); err == nil {
		t.Fatal("changed reference accepted")
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "profile.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skip(err)
	}
	ref.File = "linked/profile.json"
	if _, err := readReference(root, ref, maxInputBytes); err == nil {
		t.Fatal("symlink escape accepted")
	}
	if _, err := LoadProfile(filepath.Join(root, ref.File), LoadOptions{RootDir: root}); err == nil {
		t.Fatal("root input symlink escape accepted")
	}
}

func TestInputRootAndReadLimit(t *testing.T) {
	data := fixture(t, "profile")
	root := t.TempDir()
	path := filepath.Join(root, "profile.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(path, LoadOptions{}); err == nil {
		t.Fatal("missing root accepted")
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.invalid/fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(path, LoadOptions{}); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.invalid/nested\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := inputRoot(filepath.Join(nested, "input.json"), LoadOptions{}); err != nil || got != nested {
		t.Fatalf("wrong nearest root: %v %v", got, err)
	}
	t.Chdir(t.TempDir())
	if _, err := LoadProfile(path, LoadOptions{RootDir: root}); err != nil {
		t.Fatalf("load depends on cwd: %v", err)
	}
	if _, err := readWithin(root, path, int64(len(data)-1)); err == nil {
		t.Fatal("oversized input accepted")
	}
	if err := os.WriteFile(path, append(data, []byte(" {}")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(path, LoadOptions{RootDir: root}); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
