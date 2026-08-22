package vm

import (
	"encoding/json"
	"math"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/scene"
)

// declarativeAnimFixture lowers an orrery-shaped graph — two lights ahead of
// the meshes, one clip channel, one emissive MaterialAnim — into the wire props
// map the shared wasm bundle path consumes. The authored node list interleaves
// lights first, so numeric targetNode values are offset from every flattened
// renderable position: exactly the layout real-browser QA proved inert before
// the substrate repair.
func declarativeAnimFixture(t *testing.T) map[string]any {
	t.Helper()
	const heartY = 2.05
	props := scene.Props{Graph: scene.NewGraph(
		scene.DirectionalLight{ID: "key-light", Intensity: 1},
		scene.PointLight{ID: "heart-light", Position: scene.Vec3(0, heartY, 0)},
		scene.Mesh{
			ID: "orrery-heart", Geometry: scene.SphereGeometry{Radius: 0.5},
			Material: scene.StandardMaterial{Color: "#2a2340", Emissive: 0.12},
			Position: scene.Vec3(0, heartY, 0),
			MaterialAnims: []scene.MaterialUniformAnim{{
				Uniform: "emissive", Arity: 1, Interp: "LINEAR", Loop: true, Duration: 24,
				Times:  []float64{0, 13.2, 24},
				Values: []float64{0.12, 3.4, 0.12},
			}},
		},
		scene.Mesh{
			ID: "orrery-planet", Geometry: scene.SphereGeometry{Radius: 0.2},
			Material: scene.StandardMaterial{Color: "#c98a5a"},
			Position: scene.Vec3(2, heartY, 0),
		},
		scene.AnimationClip{
			Name: "procession", Duration: 24,
			Channels: []scene.AnimationChannel{
				// Authored index 3 == orrery-planet (two lights + heart first).
				// Values are offsets from the authored pose; the first key is
				// the zero offset so the opening frame is stable.
				{TargetNode: 3, Property: "translation", Interpolation: "LINEAR",
					Times: []float64{0, 6}, Values: []float64{0, 0, 0, -4, 0, 0}},
			},
		},
	)}
	wire, err := json.Marshal(props.SceneIR())
	if err != nil {
		t.Fatalf("marshal SceneIR: %v", err)
	}
	var ir map[string]any
	if err := json.Unmarshal(wire, &ir); err != nil {
		t.Fatalf("unmarshal SceneIR: %v", err)
	}
	return map[string]any{"scene": ir}
}

func declarativeAnimNodes() []resolvedNode {
	return []resolvedNode{
		{Kind: "light", Props: map[string]any{"kind": "directional", "id": "key-light"}},
		{Kind: "light", Props: map[string]any{"kind": "point", "id": "heart-light"}},
		{Kind: "mesh", Props: map[string]any{"id": "orrery-heart", "geometry": "sphere", "radius": 0.5, "y": 2.05}},
		{Kind: "mesh", Props: map[string]any{"id": "orrery-planet", "geometry": "sphere", "radius": 0.2, "x": 2, "y": 2.05}},
	}
}

// bundleObjectByID locates a rendered object record by ID.
func bundleObjectByID(t *testing.T, objects []rootengine.RenderObject, id string) rootengine.RenderObject {
	t.Helper()
	for _, object := range objects {
		if object.ID == id {
			return object
		}
	}
	t.Fatalf("bundle has no object %q", id)
	return rootengine.RenderObject{}
}

// objectWorldSample reads one world-space vertex of a rendered object.
func objectWorldSample(bundle *rootengine.RenderBundle, object rootengine.RenderObject) [3]float64 {
	lo := object.VertexOffset * 3
	return [3]float64{bundle.WorldPositions[lo], bundle.WorldPositions[lo+1], bundle.WorldPositions[lo+2]}
}

func meshObjectWorldSample(bundle *rootengine.RenderBundle, object rootengine.RenderObject) [3]float64 {
	lo := object.VertexOffset * 3
	return [3]float64{bundle.WorldMeshPositions[lo], bundle.WorldMeshPositions[lo+1], bundle.WorldMeshPositions[lo+2]}
}

func TestSharedBundleSolidMeshStreams(t *testing.T) {
	node := resolvedNode{Kind: "mesh", Props: map[string]any{
		"id": "solid", "geometry": "sphere", "radius": 0.5,
		"segments": 12, "wireframe": false,
	}}
	bundle := buildRenderBundleCached(map[string]any{}, []resolvedNode{node}, 320, 240, 0, newSpinScratch(), nil)
	if got := len(bundle.MeshObjects); got != 1 {
		t.Fatalf("expected exactly one mesh object for solid sphere, got %d", got)
	}
	object := bundle.MeshObjects[0]
	if object.VertexCount <= 0 || object.VertexCount%3 != 0 {
		t.Fatalf("invalid solid sphere vertex count %d", object.VertexCount)
	}
	if object.VertexOffset < 0 || object.VertexOffset+object.VertexCount > len(bundle.WorldMeshPositions)/3 {
		t.Fatalf("mesh range out of bounds: offset=%d count=%d vertices=%d", object.VertexOffset, object.VertexCount, len(bundle.WorldMeshPositions)/3)
	}
	vertices := len(bundle.WorldMeshPositions) / 3
	if bundle.WorldMeshVertexCount != vertices {
		t.Fatalf("WorldMeshVertexCount = %d, want %d", bundle.WorldMeshVertexCount, vertices)
	}
	if len(bundle.WorldMeshNormals) != vertices*3 {
		t.Fatalf("normals length %d, want %d", len(bundle.WorldMeshNormals), vertices*3)
	}
	if len(bundle.WorldMeshUVs) != vertices*2 {
		t.Fatalf("UV length %d, want %d", len(bundle.WorldMeshUVs), vertices*2)
	}
	if len(bundle.WorldMeshColors) != vertices*4 {
		t.Fatalf("color length %d, want %d", len(bundle.WorldMeshColors), vertices*4)
	}

	box := resolvedNode{Kind: "mesh", Props: map[string]any{
		"id": "solid-box", "geometry": "box", "width": 1.0, "height": 1.0,
		"depth": 1.0, "wireframe": false,
	}}
	boxBundle := buildRenderBundleCached(map[string]any{}, []resolvedNode{box}, 320, 240, 0, newSpinScratch(), nil)
	if len(boxBundle.WorldMeshNormals) == 0 {
		t.Fatal("expected solid box normals")
	}
	for i := 0; i+2 < len(boxBundle.WorldMeshNormals); i += 3 {
		components := [3]float64{
			math.Abs(boxBundle.WorldMeshNormals[i]),
			math.Abs(boxBundle.WorldMeshNormals[i+1]),
			math.Abs(boxBundle.WorldMeshNormals[i+2]),
		}
		axisCount := 0
		zeroCount := 0
		for _, component := range components {
			if component >= 0.999 {
				axisCount++
			}
			if component <= 0.001 {
				zeroCount++
			}
		}
		if axisCount != 1 || zeroCount != 2 {
			t.Fatalf("box normal %d is not face-aligned: %v", i/3, components)
		}
	}
}

func TestSharedBundleWireframeStaysLineOnly(t *testing.T) {
	node := resolvedNode{Kind: "mesh", Props: map[string]any{
		"id": "wire", "geometry": "sphere", "radius": 0.5,
		"segments": 12, "wireframe": true,
	}}
	bundle := buildRenderBundleCached(map[string]any{}, []resolvedNode{node}, 320, 240, 0, newSpinScratch(), nil)
	if got := len(bundle.MeshObjects); got != 0 {
		t.Fatalf("expected zero mesh objects for explicit wireframe, got %d", got)
	}
	if len(bundle.Objects) == 0 || len(bundle.WorldPositions) == 0 {
		t.Fatal("expected explicit wireframe to retain line objects and world positions")
	}
}

func TestSharedBundleSolidMeshAnimationAdvances(t *testing.T) {
	props := declarativeAnimFixture(t)
	nodes := declarativeAnimNodes()
	nodes[2].Props["wireframe"] = false
	nodes[3].Props["wireframe"] = false
	sc := newSpinScratch()
	b0 := buildRenderBundleCached(props, nodes, 480, 360, 0, sc, nil)
	b6 := buildRenderBundleCached(props, nodes, 480, 360, 6, sc, nil)
	planet0 := bundleObjectByID(t, b0.MeshObjects, "orrery-planet")
	planet6 := bundleObjectByID(t, b6.MeshObjects, "orrery-planet")
	pos0 := meshObjectWorldSample(&b0, planet0)
	pos6 := meshObjectWorldSample(&b6, planet6)
	if pos0 == pos6 {
		t.Fatalf("expected animated mesh world position to differ, got %v", pos0)
	}
}

// TestDeclarativeGraphClipAnimatesOnSharedBundlePath is the runtime regression
// for the browser QA finding: a declarative AnimationClip transform target must
// actually move on the shared render-bundle path even when its authored index
// counts lights that never become renderable objects.
func TestDeclarativeGraphClipAnimatesOnSharedBundlePath(t *testing.T) {
	props := declarativeAnimFixture(t)
	nodes := declarativeAnimNodes()
	sc := newSpinScratch()

	b0 := buildRenderBundleCached(props, nodes, 320, 240, 0, sc, nil)
	b6 := buildRenderBundleCached(props, nodes, 320, 240, 6, sc, nil)

	planet0 := bundleObjectByID(t, b0.Objects, "orrery-planet")
	planet6 := bundleObjectByID(t, b6.Objects, "orrery-planet")
	if planet0.VertexCount == 0 || planet6.VertexCount == 0 {
		t.Fatalf("planet produced no vertices: t=0 count %d, t=6 count %d", planet0.VertexCount, planet6.VertexCount)
	}
	pos0 := objectWorldSample(&b0, planet0)
	pos6 := objectWorldSample(&b6, planet6)
	if pos0 == pos6 {
		t.Fatalf("planet world position identical at t=0 and t=6 (%v): declarative clip did not advance", pos0)
	}
}

// TestDeclarativeMaterialAnimsAnimateOnSharedBundlePath proves declared
// MaterialAnims choreography advances through the shared bundle path: the
// heart's packed material emissive follows its keyframes instead of freezing at
// the authored value.
func TestDeclarativeMaterialAnimsAnimateOnSharedBundlePath(t *testing.T) {
	props := declarativeAnimFixture(t)
	nodes := declarativeAnimNodes()
	sc := newSpinScratch()

	emissiveAt := func(seconds float64) float64 {
		bundle := buildRenderBundleCached(props, nodes, 320, 240, seconds, sc, nil)
		object := bundleObjectByID(t, bundle.Objects, "orrery-heart")
		if object.MaterialIndex < 0 || object.MaterialIndex >= len(bundle.Materials) {
			t.Fatalf("heart material index %d out of range", object.MaterialIndex)
		}
		return bundle.Materials[object.MaterialIndex].Emissive
	}

	atOpen := emissiveAt(0)
	atFlare := emissiveAt(13.5)
	if math.Abs(atOpen-0.12) > 1e-9 {
		t.Fatalf("heart emissive at t=0 = %v, want authored 0.12", atOpen)
	}
	if math.Abs(atFlare-atOpen) < 1e-6 {
		t.Fatalf("heart emissive static across transit flare: %v", atFlare)
	}
}

// TestDeclarativeGraphClipAuthoredIndexTargeting covers foreign payloads that
// carry only a numeric targetNode: the number addresses the AUTHORED node list,
// so a light-offset index still reaches the right object.
func TestDeclarativeGraphClipAuthoredIndexTargeting(t *testing.T) {
	props := map[string]any{"scene": map[string]any{
		"animations": []map[string]any{{
			"name": "foreign", "duration": 10,
			"channels": []map[string]any{{
				"targetNode": 3, "property": "translation", "interpolation": "LINEAR",
				"times": []float64{0, 5}, "values": []float64{0, 0, 0, 0, 3, 0},
			}},
		}},
	}}
	nodes := declarativeAnimNodes()
	sc := newSpinScratch()
	b0 := buildRenderBundleCached(props, nodes, 320, 240, 0, sc, nil)
	b5 := buildRenderBundleCached(props, nodes, 320, 240, 5, sc, nil)
	planet0 := bundleObjectByID(t, b0.Objects, "orrery-planet")
	planet5 := bundleObjectByID(t, b5.Objects, "orrery-planet")
	y0 := objectWorldSample(&b0, planet0)[1]
	y5 := objectWorldSample(&b5, planet5)[1]
	if math.Abs(y5-y0) < 1e-9 {
		t.Fatalf("authored-index clip did not move planet: y0=%v y5=%v", y0, y5)
	}
}
