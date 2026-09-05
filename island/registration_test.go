package island

import (
	"errors"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestRegistrationTransaction(t *testing.T) {
	r := NewRenderer("entry")
	r.RenderIsland("Existing", nil, gosx.Text("before"))
	manifest := r.Manifest()
	_, err := r.WithRegistrations(func(stage *Renderer) (gosx.Node, error) {
		stage.SetProgramAsset("Next", "/next.json", "json", "hash")
		stage.RenderIsland("Next", nil, gosx.Text("discard"))
		return gosx.Node{}, errors.New("authored error")
	})
	if err == nil || len(manifest.Islands) != 1 || len(r.programAssets) != 0 {
		t.Fatalf("rollback: %v %#v", err, manifest.Islands)
	}
	_, err = r.WithRegistrations(func(stage *Renderer) (gosx.Node, error) {
		stage.RenderIsland("Discard", nil, gosx.Text("discard"))
		r.RenderIsland("External", nil, gosx.Text("keep"))
		return gosx.Text("stale"), nil
	})
	if err == nil || !strings.Contains(err.Error(), "changed during") || len(manifest.Islands) != 2 {
		t.Fatalf("stale: %v %#v", err, manifest.Islands)
	}
	_, err = r.WithRegistrations(func(stage *Renderer) (gosx.Node, error) {
		stage.SetProgramAsset("Next", "/next.json", "json", "hash")
		return stage.RenderIsland("Next", nil, gosx.Text("committed")), nil
	})
	if err != nil || r.Manifest() != manifest || len(manifest.Islands) != 3 || manifest.Islands[2].ProgramRef != "/next.json" {
		t.Fatalf("commit: %v %#v", err, manifest.Islands)
	}
}

func TestRegistrationConfigurationIsolation(t *testing.T) {
	r := NewRenderer("entry")
	r.SetIslandRuntime("/before.wasm", "before", 1)
	_, err := r.WithRegistrations(func(stage *Renderer) (gosx.Node, error) {
		stage.SetIslandRuntime("/discard.wasm", "discard", 2)
		return gosx.Node{}, errors.New("abort")
	})
	if err == nil || r.runtimeVariants["islands"].Hash != "before" {
		t.Fatal("staged configuration leaked", r.runtimeVariants)
	}
	_, err = r.WithRegistrations(func(stage *Renderer) (gosx.Node, error) {
		stage.SetIslandRuntime("/after.wasm", "after", 3)
		return gosx.Text("commit"), nil
	})
	if err != nil || r.runtimeVariants["islands"].Hash != "after" || r.islandRuntime.Hash != "after" {
		t.Fatal("successful configuration not committed", err)
	}
	_, err = r.WithRegistrations(func(stage *Renderer) (gosx.Node, error) {
		stage.SetIslandRuntime("/staged.wasm", "staged", 4)
		r.SetIslandRuntime("/external.wasm", "external", 5)
		return gosx.Text("stale"), nil
	})
	if err == nil || !strings.Contains(err.Error(), "changed during") || r.islandRuntime.Hash != "external" || r.runtimeVariants["islands"].Hash != "external" {
		t.Fatal("external configuration overwritten", err)
	}
}
