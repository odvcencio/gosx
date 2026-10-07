package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/bundlepolicy"
)

func deploymentFixture(t *testing.T) (string, *buildmanifest.Manifest) {
	t.Helper()
	dir := t.TempDir()
	if err := writeBundlePolicySidecar(dir, bundlepolicy.Config{}); err != nil {
		t.Fatal(err)
	}
	data := []byte("window.fixture = 1;\n")
	asset := buildmanifest.HashedAsset{File: "bootstrap.fixture.js", Size: int64(len(data)), Hash: buildmanifest.ContentHash(data), Integrity: buildmanifest.ContentIntegrity(data)}
	manifest := &buildmanifest.Manifest{Runtime: buildmanifest.RuntimeAssets{Bootstrap: asset}, SourceRoot: "/never/read/the/build/host"}
	mustWriteFile(t, filepath.Join(dir, "assets/runtime", asset.File), string(data))
	for i, role := range []*buildmanifest.HashedAsset{&manifest.Runtime.WASM, &manifest.Runtime.WASMExec, &manifest.Runtime.Patch} {
		*role = asset
		role.File = []string{"runtime.fixture.wasm", "wasm-exec.fixture.js", "patch.fixture.js"}[i]
		mustWriteFile(t, filepath.Join(dir, "assets/runtime", role.File), string(data))
	}
	for _, file := range []string{"server/app", "run.sh"} {
		mustWriteFile(t, filepath.Join(dir, file), "fixture launch file\n")
		if err := os.Chmod(filepath.Join(dir, file), 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeDeploymentFixtureManifest(t, dir, manifest)
	return dir, manifest
}

func writeDeploymentFixtureManifest(t *testing.T, dir string, manifest *buildmanifest.Manifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "build.json"), string(data))
}

func TestDeploymentCheckValidatesCopiedBundle(t *testing.T) {
	dir, manifest := deploymentFixture(t)
	asset := manifest.Runtime.Bootstrap
	manifest.Runtime.WASMVariants = map[string]buildmanifest.RuntimeVariantAsset{"duplicate": {HashedAsset: asset}}
	writeDeploymentFixtureManifest(t, dir, manifest)
	mustWriteFile(t, filepath.Join(dir, "export.json"), `{"pages":["/"],"routes":[{"path":"/","file":"index.html"}]}`)
	mustWriteFile(t, filepath.Join(dir, "static/index.html"), "<p>hello</p>")
	if err := writeCompressedSidecarsIfSmaller(filepath.Join(dir, "assets/runtime", asset.File), []byte("window.fixture = 1;\n")); err != nil {
		t.Fatal(err)
	}
	report, err := checkDeploymentBundle(dir)
	if err != nil || !report.OK || report.Assets != 4 || report.AssetBytes != 4*asset.Size || report.StaticRoutes != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	var out bytes.Buffer
	if err := runDeploy([]string{"check", "--json", dir}, &out); err != nil {
		t.Fatal(err)
	}
	var decoded deploymentCheckReport
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || !decoded.OK {
		t.Fatalf("invalid success report: %s %v", &out, err)
	}
}

func TestDeploymentCheckRejectsIncompleteOrUnsafeBundle(t *testing.T) {
	cases := []struct {
		name string
		edit func(*testing.T, string, *buildmanifest.Manifest)
		want string
	}{
		{"missing asset", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			os.Remove(filepath.Join(d, "assets/runtime", m.Runtime.Bootstrap.File))
		}, "bootstrap.fixture.js"},
		{"modified asset", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			mustWriteFile(t, filepath.Join(d, "assets/runtime", m.Runtime.Bootstrap.File), "window.fixture = 2;\n")
		}, "checksum mismatch"},
		{"truncated asset", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			mustWriteFile(t, filepath.Join(d, "assets/runtime", m.Runtime.Bootstrap.File), "bad")
		}, "size mismatch"},
		{"invalid SRI", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			m.Runtime.Bootstrap.Integrity = "sha256-bad"
			writeDeploymentFixtureManifest(t, d, m)
		}, "integrity mismatch"},
		{"traversal", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			m.Runtime.Bootstrap.File = "../../outside"
			writeDeploymentFixtureManifest(t, d, m)
		}, "invalid asset filename"},
		{"missing server", func(t *testing.T, d string, m *buildmanifest.Manifest) { os.Remove(filepath.Join(d, "server/app")) }, "server/app"},
		{"empty manifest", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			mustWriteFile(t, filepath.Join(d, "build.json"), "{}")
		}, "no runtime assets"},
		{"missing required role", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			m.Runtime.WASM = buildmanifest.HashedAsset{}
			writeDeploymentFixtureManifest(t, d, m)
		}, "required runtime asset wasm"},
		{"runtime secret", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			mustWriteFile(t, filepath.Join(d, ".env"), "SECRET=must-not-be-printed")
		}, "secret or credential"},
		{"missing static page", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			mustWriteFile(t, filepath.Join(d, "export.json"), `{"routes":[{"path":"/","file":"index.html"}]}`)
		}, "static/index.html"},
		{"static traversal", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			mustWriteFile(t, filepath.Join(d, "export.json"), `{"routes":[{"path":"/","file":"../outside"}]}`)
		}, "invalid static route file"},
		{"conflicting aliases", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			m.Runtime.Patch = m.Runtime.Bootstrap
			m.Runtime.Patch.Hash = "bad"
			writeDeploymentFixtureManifest(t, d, m)
		}, "conflicting asset records"},
		{"compressed mismatch", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			w.Write([]byte("wrong"))
			w.Close()
			mustWriteFile(t, filepath.Join(d, "assets/runtime", m.Runtime.Bootstrap.File+".gz"), b.String())
		}, "compressed content differs"},
		{"corrupt brotli", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			mustWriteFile(t, filepath.Join(d, "assets/runtime", m.Runtime.Bootstrap.File+".br"), "corrupt")
		}, ".br"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, manifest := deploymentFixture(t)
			tc.edit(t, dir, manifest)
			report, err := checkDeploymentBundle(dir)
			if err == nil || report.OK || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("report=%+v err=%v; want %q", report, err, tc.want)
			}
			if strings.Contains(err.Error(), "must-not-be-printed") {
				t.Fatal("error exposed secret contents")
			}
		})
	}
}

func TestDeploymentCheckRejectsSymlinks(t *testing.T) {
	dir, manifest := deploymentFixture(t)
	name := filepath.Join(dir, "assets/runtime", manifest.Runtime.Bootstrap.File)
	if err := os.Remove(name); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "run.sh"), name); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := checkDeploymentBundle(dir); err == nil || !strings.Contains(err.Error(), "symlinks") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

func TestDeploymentCheckJSONFailureAndArguments(t *testing.T) {
	var out bytes.Buffer
	err := runDeploy([]string{"check", "--json", t.TempDir()}, &out)
	var report deploymentCheckReport
	if err == nil || json.Unmarshal(out.Bytes(), &report) != nil || report.OK || len(report.Errors) == 0 {
		t.Fatalf("missing JSON failure: %s (%v)", &out, err)
	}
	for _, args := range [][]string{nil, {"push"}, {"check"}, {"check", "--unknown", "dist"}, {"check", "one", "two"}} {
		if err := runDeploy(args, &out); err == nil {
			t.Fatalf("accepted invalid arguments %q", args)
		}
	}
}
