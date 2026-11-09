package scene

import (
	"testing"

	"m31labs.dev/gosx/assetpipe/manifest"
)

func TestModelLODGroupFromManifest(t *testing.T) {
	entry := manifest.Model{
		Name: "hero",
		LODs: []manifest.LOD{
			{Level: 0, URI: "hero.glb"},
			{Level: 1, URI: "hero.lod1.glb", Distance: 39.2},
			{Level: 2, URI: "hero.lod2.glb", Distance: 62},
		},
	}
	base := Model{ID: "player", Position: Vector3{X: 1}, CastShadow: true}
	group := ModelLODGroup(entry, "/assets/", base)
	if group.ID != "player" || len(group.Levels) != 3 {
		t.Fatalf("group = %+v", group)
	}
	wantSrc := []string{"/assets/hero.glb", "/assets/hero.lod1.glb", "/assets/hero.lod2.glb"}
	wantDist := []float64{0, 39.2, 62}
	for i, level := range group.Levels {
		model, ok := level.Node.(Model)
		if !ok {
			t.Fatalf("level %d node is %T", i, level.Node)
		}
		if model.Src != wantSrc[i] || level.Distance != wantDist[i] || !model.CastShadow || model.Position.X != 1 {
			t.Fatalf("level %d = %+v distance %v", i, model, level.Distance)
		}
		if want := "player-lod" + string(rune('0'+i)); model.ID != want {
			t.Fatalf("level %d id = %q, want %q", i, model.ID, want)
		}
	}
}

func TestModelLODGroupLowersToModelsWithDistanceBands(t *testing.T) {
	entry := manifest.Model{Name: "hero", LODs: []manifest.LOD{
		{Level: 0, URI: "hero.glb"}, {Level: 1, URI: "hero.lod1.glb", Distance: 40},
	}}
	group := ModelLODGroup(entry, "", Model{})
	l := &graphLowerer{anchors: make(map[string]worldTransform)}
	l.lowerLODGroup(group, identityTransform())
	if len(l.models) != 2 {
		t.Fatalf("models = %d, want 2", len(l.models))
	}
	first, second := l.models[0], l.models[1]
	if first.LODGroup != "hero" || first.LODLevel != 0 || first.LODMinDistance != 0 || first.LODMaxDistance != 40 {
		t.Fatalf("first = %+v", first)
	}
	if second.LODLevel != 1 || second.LODMinDistance != 40 || second.LODMaxDistance != 0 {
		t.Fatalf("second = %+v", second)
	}
}
