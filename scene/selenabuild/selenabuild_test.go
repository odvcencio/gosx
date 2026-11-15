package selenabuild

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"m31labs.dev/selena/artifact"
	"m31labs.dev/selena/result"
)

const litSource = `material Lit {
    context {
        sunDir   : vec3
        sunColor : vec3
    }
    param baseColor : color = rgb(0.7, 0.6, 0.5)
    surface(geo) -> color {
        return baseColor * sunColor * max(dot(normalize(geo.worldNormal), sunDir), 0.0)
    }
}
`

const librarySource = `fn halve(x: float) -> float {
    return x * 0.5
}
`

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSourcesSkipsIgnoredDirectoriesAndNestedModules(t *testing.T) {
	root := writeTree(t, map[string]string{
		"materials/rock.sel":         litSource,
		"app/scene/water.sel":        litSource,
		"lib.sel":                    librarySource,
		".cache/hidden.sel":          litSource,
		"_scratch/old.sel":           litSource,
		"testdata/fixture.sel":       litSource,
		"node_modules/pkg/x.sel":     litSource,
		"vendor/m/y.sel":             litSource,
		"dist/assets/z.sel":          litSource,
		"tools/gen/go.mod":           "module gen\n",
		"tools/gen/own.sel":          litSource,
		"materials/rock.sel.json":    "{}",
		"materials/notes.sel.backup": "",
	})
	got, err := Sources(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"app/scene/water.sel", "lib.sel", "materials/rock.sel"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Sources = %v, want %v", got, want)
	}
}

func TestBuildWritesArtifactsNextToSourcesAndCheckPasses(t *testing.T) {
	root := writeTree(t, map[string]string{"materials/rock.sel": litSource, "lib.sel": librarySource})
	report, err := Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Written, []string{"materials/rock.sel.json"}) || !reflect.DeepEqual(report.Libraries, []string{"lib.sel"}) {
		t.Fatalf("report = %+v", report)
	}
	data, err := os.ReadFile(filepath.Join(root, "materials", "rock.sel.json"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := artifact.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if file.Source.Path != "materials/rock.sel" || !reflect.DeepEqual(file.Options.Targets, DefaultTargets) {
		t.Fatalf("path %q targets %v", file.Source.Path, file.Options.Targets)
	}
	if err := Check(root, Options{}); err != nil {
		t.Fatalf("fresh build failed Check: %v", err)
	}

	// A second build rewrites nothing, so mtimes and Go's build cache survive.
	stamp := time.Unix(1_700_000_000, 0)
	path := filepath.Join(root, "materials", "rock.sel.json")
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	report, err = Build(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Written) != 0 || !info.ModTime().Equal(stamp) {
		t.Fatalf("unchanged source rewrote its artifact: %+v", report)
	}
}

func TestCheckReportsEveryKindOfDrift(t *testing.T) {
	root := writeTree(t, map[string]string{"a.sel": litSource, "b.sel": litSource, "c.sel": litSource})
	if _, err := Build(root, Options{}); err != nil {
		t.Fatal(err)
	}
	// a: source edited after the build.
	if err := os.WriteFile(filepath.Join(root, "a.sel"), []byte(strings.Replace(litSource, "0.7", "0.9", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	// b: artifact deleted.
	if err := os.Remove(filepath.Join(root, "b.sel.json")); err != nil {
		t.Fatal(err)
	}
	// c: artifact corrupted.
	if err := os.WriteFile(filepath.Join(root, "c.sel.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Check(root, Options{})
	var drift *DriftError
	if !errors.As(err, &drift) {
		t.Fatalf("Check = %v, want a DriftError", err)
	}
	reasons := map[string]string{}
	for _, p := range drift.Problems {
		reasons[p.Source] = p.Reason
	}
	if !strings.Contains(reasons["a.sel"], "sha256") || !strings.Contains(reasons["b.sel"], "no artifact") || reasons["c.sel"] == "" {
		t.Fatalf("problems = %+v", drift.Problems)
	}
	if !strings.Contains(err.Error(), "gosx selena build") {
		t.Fatalf("error %q does not say how to fix it", err)
	}
}

func TestCheckFailsWhenConfigChangesOptions(t *testing.T) {
	root := writeTree(t, map[string]string{"a.sel": litSource})
	if _, err := Build(root, Options{}); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []Options{
		{Targets: []string{"wgsl", "gles", "metal"}},
		{SpecularMRT: true},
		{Modules: []string{"brdf"}},
	} {
		err := Check(root, opts)
		if err == nil || !strings.Contains(err.Error(), "config wants") {
			t.Fatalf("%+v: Check = %v, want an options mismatch", opts, err)
		}
	}
}

func TestOptionsValidateTargets(t *testing.T) {
	got, err := Options{Targets: []string{"gles", "wgsl", "metal"}}.targets()
	if err != nil || !reflect.DeepEqual(got, []result.Target{result.TargetGLES, result.TargetWGSL, result.TargetMetal}) {
		t.Fatalf("targets = %v, %v", got, err)
	}
	for _, bad := range [][]string{{"hlsl"}, {"wgsl", "wgsl"}} {
		if _, err := (Options{Targets: bad}).targets(); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	root := writeTree(t, map[string]string{"a.sel": litSource})
	if _, err := Build(root, Options{Modules: []string{"missing"}}); err == nil {
		t.Fatal("Build accepted an unknown module")
	}
	if _, err := Build(root, Options{Targets: []string{"hlsl"}}); err == nil {
		t.Fatal("Build accepted an unknown target")
	}
}

func TestBuildReportsCompileErrorsWithThePath(t *testing.T) {
	root := writeTree(t, map[string]string{"bad.sel": "material Bad { surface(geo) -> color { return missing } }"})
	_, err := Build(root, Options{})
	if err == nil || !strings.Contains(err.Error(), "bad.sel") {
		t.Fatalf("Build = %v, want an error naming bad.sel", err)
	}
}
