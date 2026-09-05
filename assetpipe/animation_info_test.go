package assetpipe

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlanMorphOnlyAssetAndAnimationInventory(t *testing.T) {
	dir := t.TempDir()
	fixture := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []any{map[string]any{"primitives": []any{
			map[string]any{"attributes": map[string]int{"POSITION": 0}, "targets": []any{map[string]int{"POSITION": 1}, map[string]int{"NORMAL": 2}}},
			map[string]any{"attributes": map[string]int{"POSITION": 0}},
		}}},
		"accessors": []any{
			map[string]any{"type": "VEC3", "count": 3, "componentType": 5126},
			map[string]any{"type": "SCALAR", "count": 2, "componentType": 5126, "min": []float64{0.5}, "max": []float64{2.5}},
			map[string]any{"type": "SCALAR", "count": 2, "componentType": 5126, "min": []float64{0}, "max": []float64{1}},
		},
		"animations": []any{
			map[string]any{"name": "lunging", "samplers": []any{map[string]any{"input": 1, "output": 0}, map[string]any{"input": 2, "output": 0, "interpolation": "CUBICSPLINE"}}, "channels": []any{
				map[string]any{"sampler": 0, "target": map[string]any{"node": 0, "path": "weights"}},
				map[string]any{"sampler": 1, "target": map[string]any{"node": 0, "path": "translation"}},
			}},
			map[string]any{"name": "lunging"},
			map[string]any{},
		},
	}
	mustWriteBytes(t, filepath.Join(dir, "morph.glb"), buildTestGLB(t, fixture))
	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	info := findAsset(t, report, "morph.glb").GLTF
	if info == nil || info.MorphTargets != 2 || info.MorphPrimitives != 1 || len(info.AnimationClips) != 3 {
		t.Fatalf("deformation inventory: %+v", info)
	}
	if got := report.SkinManifest["morph.glb"]; got.Skinned || !got.MorphTargets {
		t.Fatalf("morph-only asset must be included without claiming a skin: %+v", got)
	}
	clip := info.AnimationClips[0]
	if clip.Index != 0 || clip.Name != "lunging" || clip.Channels != 2 || clip.Duration == nil || *clip.Duration != 2.5 || *clip.StartSeconds != 0 || *clip.EndSeconds != 2.5 || clip.TimingSource != "accessor-bounds" {
		t.Fatalf("clip metadata: %+v", clip)
	}
	if !reflect.DeepEqual(clip.TargetPaths, []string{"translation", "weights"}) || !reflect.DeepEqual(clip.Interpolations, []string{"CUBICSPLINE", "LINEAR"}) {
		t.Fatalf("clip channel inventory: %+v", clip)
	}
	if info.AnimationClips[1].Index != 1 || info.AnimationClips[1].Name != "lunging" || info.AnimationClips[1].Duration != nil || info.AnimationClips[2].Name != "" {
		t.Fatalf("duplicate and unnamed clips must keep their authored identities: %+v", info.AnimationClips)
	}
}

func TestAnimationInventoryDoesNotInventDuration(t *testing.T) {
	for name, source := range map[string]string{
		"missing input":      `{"samplers":[{}],"channels":[{"sampler":0,"target":{"path":"rotation"}}]}`,
		"missing sampler":    `{"samplers":[{"input":0}],"channels":[{"target":{"path":"rotation"}}]}`,
		"negative sampler":   `{"samplers":[{"input":0}],"channels":[{"sampler":-1}]}`,
		"out of range input": `{"samplers":[{"input":2}],"channels":[{"sampler":0}]}`,
		"partial timing":     `{"samplers":[{"input":0},{"input":1}],"channels":[{"sampler":0},{"sampler":1}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var animation gltfAnimationProbe
			if err := json.Unmarshal([]byte(source), &animation); err != nil {
				t.Fatal(err)
			}
			accessors := []gltfAnimationAccessorProbe{{Type: "SCALAR", Count: 2, ComponentType: 5126, Min: []float64{0}, Max: []float64{3}}, {Type: "SCALAR", Count: 2, ComponentType: 5126}}
			clip := inspectAnimationClips([]gltfAnimationProbe{animation}, accessors)[0]
			if clip.Duration != nil || clip.StartSeconds != nil || clip.EndSeconds != nil || clip.TimingSource != "" {
				t.Fatalf("incomplete timing must stay unknown: %+v", clip)
			}
		})
	}
}

func TestAnimationInventoryRejectsInvalidTimeBounds(t *testing.T) {
	var animation gltfAnimationProbe
	if err := json.Unmarshal([]byte(`{"samplers":[{"input":0}],"channels":[{"sampler":0}]}`), &animation); err != nil {
		t.Fatal(err)
	}
	for _, accessor := range []gltfAnimationAccessorProbe{
		{Type: "VEC3", ComponentType: 5126, Count: 2, Min: []float64{0}, Max: []float64{3}},
		{Type: "SCALAR", ComponentType: 5123, Count: 2, Min: []float64{0}, Max: []float64{3}},
		{Type: "SCALAR", ComponentType: 5126, Count: 0, Min: []float64{0}, Max: []float64{3}},
		{Type: "SCALAR", ComponentType: 5126, Count: 2, Min: []float64{-1}, Max: []float64{3}},
		{Type: "SCALAR", ComponentType: 5126, Count: 2, Min: []float64{4}, Max: []float64{3}},
	} {
		if clip := inspectAnimationClips([]gltfAnimationProbe{animation}, []gltfAnimationAccessorProbe{accessor})[0]; clip.Duration != nil {
			t.Fatalf("invalid timing reported: %+v", clip)
		}
	}
}
