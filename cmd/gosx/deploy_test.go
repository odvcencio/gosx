package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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
		{"missing policy", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			os.Remove(filepath.Join(d, "bundle-policy.json"))
		}, "bundle-policy.json is missing; run gosx build --prod"},
		{"missing scene report", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			m.SceneAssets = &buildmanifest.SceneAssetManifest{File: "scene-assets.json"}
			writeDeploymentFixtureManifest(t, d, m)
		}, "scene-assets.json is missing"},
		{"scene report traversal", func(t *testing.T, d string, m *buildmanifest.Manifest) {
			m.SceneAssets = &buildmanifest.SceneAssetManifest{File: "../outside"}
			writeDeploymentFixtureManifest(t, d, m)
		}, "invalid bundle path"},
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
		{"missing server", func(t *testing.T, d string, m *buildmanifest.Manifest) { os.Remove(filepath.Join(d, "server/app")) }, "server/app and server/app.exe are missing; run gosx build --prod"},
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
		for _, jsonOut := range []bool{false, true} {
			command := append([]string(nil), args...)
			if jsonOut {
				command = append(command, "--json")
			}
			out.Reset()
			err := runDeploy(command, &out)
			if err == nil || deploymentExitCode(err) != 2 {
				t.Fatalf("usage error %q: %v", command, err)
			}
			if jsonOut && (json.Unmarshal(out.Bytes(), &report) != nil || report.OK || report.Version != 1 || len(report.Errors) != 1) {
				t.Fatalf("missing JSON argument error %q: %s", command, &out)
			}
		}
	}
	if deploymentExitCode(nil) != 0 || deploymentExitCode(err) != 1 {
		t.Fatal("incorrect success/check exit codes")
	}

}

func TestDeploymentRuntimeWalkIncludesSliceAssets(t *testing.T) {
	asset := buildmanifest.HashedAsset{File: "slice.js", Size: 7}
	var got []buildmanifest.HashedAsset
	runtime := struct {
		Groups []struct {
			Assets []buildmanifest.RuntimeVariantAsset
		}
		Extra [1]any
		Empty []buildmanifest.HashedAsset
	}{}
	runtime.Groups = append(runtime.Groups, struct {
		Assets []buildmanifest.RuntimeVariantAsset
	}{[]buildmanifest.RuntimeVariantAsset{{HashedAsset: asset}}})
	runtime.Extra[0] = &asset
	walkDeploymentRuntimeAssets(reflect.ValueOf(runtime), func(a buildmanifest.HashedAsset) { got = append(got, a) })
	if !reflect.DeepEqual(got, []buildmanifest.HashedAsset{asset, asset}) {
		t.Fatalf("slice/array assets = %#v", got)
	}
}

func TestDeploymentRuntimeSliceCannotSkipValidation(t *testing.T) {
	for _, kind := range []string{"missing", "checksum"} {
		t.Run(kind, func(t *testing.T) {
			dir, manifest := deploymentFixture(t)
			asset := manifest.Runtime.Bootstrap
			file := filepath.Join(dir, "assets", "runtime", asset.File)
			if kind == "missing" {
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			} else {
				data := bytes.Repeat([]byte("x"), int(asset.Size))
				if err := os.WriteFile(file, data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			failures := 0
			walkDeploymentRuntimeAssets(reflect.ValueOf(struct{ Chunks []buildmanifest.HashedAsset }{[]buildmanifest.HashedAsset{asset}}), func(a buildmanifest.HashedAsset) {
				if err := verifyDeploymentAsset(root, deploymentAsset{path: "assets/runtime/" + a.File, HashedAsset: a}); err != nil {
					failures++
				}
			})
			if failures != 1 {
				t.Fatalf("slice asset validation failures=%d, want 1", failures)
			}
		})
	}
}

func TestDeploymentCheckAcceptsWindowsServer(t *testing.T) {
	dir, _ := deploymentFixture(t)
	exe := filepath.Join(dir, "server", "app.exe")
	if err := os.Rename(filepath.Join(dir, "server", "app"), exe); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(exe, 0644); err != nil {
		t.Fatal(err)
	}
	if report, err := checkDeploymentBundle(dir); err != nil || !report.OK {
		t.Fatalf("Windows bundle: %+v %v", report, err)
	}
	// A broken primary binary must not be hidden by the Windows fallback.
	mustWriteFile(t, filepath.Join(dir, "server", "app"), "")
	if _, err := checkDeploymentBundle(dir); err == nil {
		t.Fatal("accepted empty primary server")
	}
}

func TestDeploymentAssetsCoverManifest(t *testing.T) {
	var manifest buildmanifest.Manifest
	expected := make(map[string]string)
	hashed := reflect.TypeFor[buildmanifest.HashedAsset]()
	var fill func(reflect.Value, string)
	fill = func(v reflect.Value, field string) {
		if v.Type() == hashed {
			asset := buildmanifest.HashedAsset{File: fmt.Sprintf("asset-%d.js", len(expected)), Size: 1}
			expected[asset.File] = field
			v.Set(reflect.ValueOf(asset))
			return
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				fill(v.Field(i), field+"."+v.Type().Field(i).Name)
			}
		case reflect.Pointer:
			v.Set(reflect.New(v.Type().Elem()))
			fill(v.Elem(), field)
		case reflect.Slice:
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
			fill(v.Index(0), field+"[]")
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				fill(v.Index(i), field+"[]")
			}
		case reflect.Map:
			value := reflect.New(v.Type().Elem()).Elem()
			fill(value, field+"{}")
			v.Set(reflect.MakeMap(v.Type()))
			v.SetMapIndex(reflect.Zero(v.Type().Key()), value)
		}
	}
	fill(reflect.ValueOf(&manifest).Elem(), "Manifest")
	for _, asset := range deploymentAssets(&manifest) {
		if _, ok := expected[asset.File]; !ok {
			t.Errorf("unexpected or duplicate asset %s", asset.File)
		}
		delete(expected, asset.File)
	}
	for _, field := range expected {
		t.Errorf("uncovered HashedAsset field: %s", field)
	}
	// SceneAssets.File points to an unhashed report, which must also be present.
	dir, fixture := deploymentFixture(t)
	fixture.SceneAssets = &buildmanifest.SceneAssetManifest{File: "scene-assets.json"}
	mustWriteFile(t, filepath.Join(dir, "scene-assets.json"), "{}")
	writeDeploymentFixtureManifest(t, dir, fixture)
	if _, err := checkDeploymentBundle(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "scene-assets.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := checkDeploymentBundle(dir); err == nil || !strings.Contains(err.Error(), "scene-assets.json is missing") {
		t.Fatalf("uncovered SceneAssets.File: %v", err)
	}
}
