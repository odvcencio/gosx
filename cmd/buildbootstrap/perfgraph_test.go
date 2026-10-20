package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
)

func perfGraphFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "js")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path string, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
		if err := writeCompressedSidecars(path, body); err != nil {
			t.Fatal(err)
		}
	}
	for _, output := range outputs {
		write(filepath.Join(dir, output.name), "/* synthetic "+output.name+" */")
	}
	for _, output := range inlineAssets {
		write(filepath.Join(dir, output.name), "/* synthetic navigation */")
	}
	return dir
}

func TestPerfGraphWholeOutputInventory(t *testing.T) {
	dir := perfGraphFixture(t)
	graph, err := perfGraph(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Assets) != len(outputs)+len(inlineAssets) {
		t.Fatal("output table and graph diverged")
	}
	seen := map[string]buildmanifest.PerfAssetUse{}
	for _, a := range graph.Assets {
		if a.Phase != "dormant" || a.Owner != "framework" || a.Kind != "js" || len(a.SHA256) != 64 {
			t.Fatalf("inventory: %+v", a)
		}
		seen[a.ID] = a
	}
	if seen["framework/runtime/bootstrap.js"].SHA256 == seen["framework/runtime/bootstrap-runtime.js"].SHA256 {
		t.Fatal("whole-output identities collapsed")
	}
	for _, name := range []string{"webgpu", "webgl"} {
		a := seen["framework/runtime/bootstrap-feature-scene3d-"+name+".js"]
		if a.Condition != name || !reflect.DeepEqual(a.Dependencies, []string{"framework/runtime/bootstrap-feature-scene3d.js"}) {
			t.Fatalf("backend closure: %+v", a)
		}
	}
	// Recovery ships inside the WebGPU renderers, not as its own output.
	if a, ok := seen["framework/runtime/bootstrap-feature-scene3d-pipeline-recovery.js"]; ok {
		t.Fatalf("removed recovery chunk is still in the inventory: %+v", a)
	}
	if a := seen["framework/runtime/navigation.js"]; !strings.Contains(a.URL, a.SHA256) {
		t.Fatal("navigation URL not bound to output")
	}
	out := filepath.Join(t.TempDir(), "graph.json")
	if err := writePerfGraph(dir, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var decoded buildmanifest.PerfAssetUses
	if err := json.Unmarshal(data, &decoded); err != nil || !reflect.DeepEqual(&decoded, graph) {
		t.Fatalf("closed graph round trip: %v", err)
	}
	if strings.Contains(string(data), "sources") || strings.Contains(string(data), dir) {
		t.Fatal("output includes source byte attribution or a local root")
	}
}

func TestPerfGraphEquivalentDirectorySpellings(t *testing.T) {
	dir := perfGraphFixture(t)
	plain, err := perfGraph(dir)
	if err != nil {
		t.Fatal(err)
	}
	withSlash, err := perfGraph(dir + string(filepath.Separator))
	if err != nil {
		t.Fatal("directory with trailing slash:", err)
	}
	if !reflect.DeepEqual(plain, withSlash) {
		t.Fatal("equivalent directory spellings changed the graph")
	}
	t.Chdir(dir)
	if err := os.Mkdir("js", 0755); err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{".", "./", "./js/..", "./js/../"} {
		got, err := perfGraph(spelling)
		if err != nil || !reflect.DeepEqual(plain, got) {
			t.Errorf("directory %q changed the graph: %v", spelling, err)
		}
	}
}

func TestPerfGraphCommittedOutputs(t *testing.T) {
	dir, err := filepath.Abs("../../client/js")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := perfGraph(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range graph.Assets {
		if asset.ID == "framework/runtime/navigation.js" {
			if asset.URL != runtimehost.NavigationRuntimePath {
				t.Fatal("committed graph disagrees with the served navigation identity")
			}
			return
		}
	}
	t.Fatal("committed navigation body missing")
}

func TestPerfGraphMissingBodyAndCorruptSidecars(t *testing.T) {
	for _, suffix := range []string{"", ".gz", ".br"} {
		t.Run(suffix, func(t *testing.T) {
			dir := perfGraphFixture(t)
			path := filepath.Join(dir, outputs[0].name+suffix)
			if suffix == "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.WriteFile(path, []byte("corrupt"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			graph, err := perfGraph(dir)
			var typed *buildmanifest.PerfAssetError
			if graph != nil || !errors.As(err, &typed) || strings.Contains(err.Error(), dir) {
				t.Fatalf("unverified graph: %v", err)
			}
		})
	}
	if err := writePerfGraph("unused", ""); err != nil {
		t.Fatal("optional output changed ordinary builder behavior")
	}
}
