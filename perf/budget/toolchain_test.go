package budget

import (
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestToolchainContract(t *testing.T) {
	// The archive digest is a synthetic fixture, not an installed browser pin.
	tc, err := LoadToolchain("testdata/toolchain.v1.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if tc.Brotli != "v1.2.1" || tc.BuilderBrotli != "v1.2.2" || tc.ZopfliIterations != 5 || tc.GzipLevel != 9 {
		t.Fatalf("producer pins were conflated: %+v", tc)
	}
	for _, key := range []string{"brotli", "builderBrotli", "binaryen", "chromeProduct", "chromeSnapshot", "cdproto", "chromedp", "node", "gzipLevel", "brotliQuality", "brotliWindow"} {
		t.Run(key, func(t *testing.T) {
			path := changedInput(t, "toolchain", func(v map[string]any) { v[key] = "wrong-pin" })
			if _, err := LoadToolchain(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
				t.Fatal("incorrect pin accepted")
			}
		})
	}
	for name, edit := range map[string]func(map[string]any){
		"private-field":    func(v map[string]any) { v["hostname"] = "unexpected" },
		"unknown-platform": func(v map[string]any) { v["platformArchives"].(map[string]any)["other"] = strings.Repeat("a", 64) },
		"bad-hash":         func(v map[string]any) { v["binaryenSHA256"] = "not-a-sha" },
		"no-archive":       func(v map[string]any) { v["platformArchives"] = map[string]any{} },
	} {
		t.Run(name, func(t *testing.T) {
			path := changedInput(t, "toolchain", edit)
			if _, err := LoadToolchain(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
				t.Fatal("invalid toolchain accepted")
			}
		})
	}
	root := projectRoot(t)
	if _, err := readReference(root, Ref{File: tc.Fonts[0].File, SHA256: strings.Repeat("a", 64)}, 16<<20); err == nil {
		t.Fatal("stale font accepted")
	}
	deps := []*debug.Module{{Path: "github.com/andybalholm/brotli", Version: "v1.2.1"}, {Path: "github.com/chromedp/chromedp", Version: "v0.15.1"}}
	if err := tc.validateModules(deps); err != nil {
		t.Fatal(err)
	}
	deps[0].Version = "v1.2.2"
	if err := tc.validateModules(deps); err == nil {
		t.Fatal("MVS-selected builder Brotli accepted as canonical")
	}
	deps[0].Version = "v1.2.1"
	deps[0].Replace = &debug.Module{Path: "example.invalid/replacement"}
	if err := tc.validateModules(deps); err == nil {
		t.Fatal("unpinned module replacement accepted")
	}
}
