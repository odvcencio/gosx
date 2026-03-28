package enginevm

import (
	"math"
	"testing"

	"github.com/odvcencio/gosx/client/vm"
	rootengine "github.com/odvcencio/gosx/engine"
	islandprogram "github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/signal"
)

func TestRuntimeInitialReconcileCreatesObjects(t *testing.T) {
	prog := &rootengine.Program{
		Name: "GeometryZoo",
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"x": 0,
					"y": 1,
					"z": 2,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":     3,
					"color": 4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
	}

	rt := New(prog, `{}`)
	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected 2 create commands, got %d", len(commands))
	}
	if commands[0].Kind != rootengine.CommandCreateObject || commands[1].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected create commands, got %#v", commands)
	}
}

func TestRuntimeTickProducesIncrementalMaterialAndTransformCommands(t *testing.T) {
	prog := &rootengine.Program{
		Name: "GeometryZoo",
		Nodes: []rootengine.Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.color", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 2},
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 3},
		},
	}

	rt := New(prog, `{}`)
	xSig := signal.New(vm.FloatVal(0))
	colorSig := signal.New(vm.StringVal("#8de1ff"))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.SetSharedSignal("$scene.color", colorSig)

	if commands := rt.Reconcile(); len(commands) != 1 {
		t.Fatalf("expected initial create command, got %d", len(commands))
	}

	xSig.Set(vm.FloatVal(3.25))
	colorSig.Set(vm.StringVal("#ff8f6b"))
	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected transform + material commands, got %#v", commands)
	}
	if commands[0].Kind != rootengine.CommandSetTransform {
		t.Fatalf("expected first command to be transform, got %v", commands[0].Kind)
	}
	if commands[1].Kind != rootengine.CommandSetMaterial {
		t.Fatalf("expected second command to be material, got %v", commands[1].Kind)
	}
}

func TestRuntimeMarksOnlyDependentNodesDirty(t *testing.T) {
	prog := &rootengine.Program{
		Name: "DirtyTracking",
		Nodes: []rootengine.Node{
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"x": 0,
				},
			},
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"y": 1,
				},
			},
			{
				Kind:   "mesh",
				Static: true,
				Props: map[string]islandprogram.ExprID{
					"z": 2,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.y", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.static", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 0},
			{Name: "$scene.y", Type: islandprogram.TypeFloat, Init: 1},
			{Name: "$scene.static", Type: islandprogram.TypeFloat, Init: 2},
		},
	}

	rt := New(prog, `{}`)
	rt.Reconcile()
	if got := rt.dirty; got[0] || got[1] || got[2] {
		t.Fatalf("expected clean runtime after initial reconcile, got %#v", got)
	}

	xSig := signal.New(vm.FloatVal(0))
	rt.SetSharedSignal("$scene.x", xSig)
	clearDirty(rt.dirty)

	xSig.Set(vm.FloatVal(2.5))
	if !rt.dirty[0] {
		t.Fatal("expected first node to be dirty after $scene.x change")
	}
	if rt.dirty[1] {
		t.Fatal("expected second node to remain clean after $scene.x change")
	}
	if rt.dirty[2] {
		t.Fatal("expected static node to remain clean after $scene.x change")
	}
}

func TestRuntimeClearsDirtyFlagsAfterReconcile(t *testing.T) {
	prog := &rootengine.Program{
		Name: "DirtyReconcile",
		Nodes: []rootengine.Node{
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.color", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 2},
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 3},
		},
	}

	rt := New(prog, `{}`)
	xSig := signal.New(vm.FloatVal(0))
	colorSig := signal.New(vm.StringVal("#8de1ff"))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.SetSharedSignal("$scene.color", colorSig)
	rt.Reconcile()

	xSig.Set(vm.FloatVal(1.25))
	colorSig.Set(vm.StringVal("#ffd48f"))
	if !rt.dirty[0] {
		t.Fatal("expected node to be dirty after shared signal changes")
	}

	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected transform + material commands, got %#v", commands)
	}
	if rt.dirty[0] {
		t.Fatal("expected dirty flag to clear after reconcile")
	}
}

func TestRuntimeRenderBundleAppliesSceneMotionOffsets(t *testing.T) {
	prog := &rootengine.Program{
		Name: "MotionOffsets",
		Nodes: []rootengine.Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":          0,
					"y":          1,
					"z":          2,
					"shiftX":     3,
					"shiftY":     4,
					"shiftZ":     5,
					"driftSpeed": 6,
					"driftPhase": 7,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.55", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.9", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.8", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.25", Type: islandprogram.TypeFloat},
		},
	}

	rt := New(prog, `{}`)
	start := rt.RenderBundle(640, 360, 0)
	later := rt.RenderBundle(640, 360, 1.8)
	if len(start.Objects) != 1 || len(later.Objects) != 1 {
		t.Fatalf("expected one render object in each bundle, got %#v and %#v", start.Objects, later.Objects)
	}

	startBounds := start.Objects[0].Bounds
	laterBounds := later.Objects[0].Bounds
	startCenterX := (startBounds.MinX + startBounds.MaxX) / 2
	startCenterY := (startBounds.MinY + startBounds.MaxY) / 2
	startCenterZ := (startBounds.MinZ + startBounds.MaxZ) / 2
	laterCenterX := (laterBounds.MinX + laterBounds.MaxX) / 2
	laterCenterY := (laterBounds.MinY + laterBounds.MaxY) / 2
	laterCenterZ := (laterBounds.MinZ + laterBounds.MaxZ) / 2

	if math.Abs(startCenterX-laterCenterX) < 0.001 {
		t.Fatalf("expected X center to drift, got start=%f later=%f", startCenterX, laterCenterX)
	}
	if math.Abs(startCenterY-laterCenterY) < 0.001 {
		t.Fatalf("expected Y center to drift, got start=%f later=%f", startCenterY, laterCenterY)
	}
	if math.Abs(startCenterZ-laterCenterZ) < 0.001 {
		t.Fatalf("expected Z center to drift, got start=%f later=%f", startCenterZ, laterCenterZ)
	}
}

func TestRuntimeRenderBundleSyncsDirtyNodes(t *testing.T) {
	prog := &rootengine.Program{
		Name: "RenderBundle",
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":     2,
					"color": 3,
					"size":  4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "75", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.color", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "1.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 5},
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 6},
		},
	}

	rt := New(prog, `{"background":"#102030"}`)
	xSig := signal.New(vm.FloatVal(0))
	colorSig := signal.New(vm.StringVal("#8de1ff"))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.SetSharedSignal("$scene.color", colorSig)
	rt.Reconcile()

	xSig.Set(vm.FloatVal(2.25))
	colorSig.Set(vm.StringVal("#ff8f6b"))
	if !rt.dirty[1] {
		t.Fatal("expected mesh node to be dirty before render bundle generation")
	}

	bundle := rt.RenderBundle(640, 360, 1.5)
	if bundle.Background != "#102030" {
		t.Fatalf("expected background from props, got %q", bundle.Background)
	}
	if bundle.Camera.Z != 6 {
		t.Fatalf("expected default camera to flow into bundle, got %#v", bundle.Camera)
	}
	if bundle.Camera.Near != 0.05 || bundle.Camera.Far != 128 {
		t.Fatalf("expected default clip planes in render bundle camera, got %#v", bundle.Camera)
	}
	if bundle.ObjectCount != 1 {
		t.Fatalf("expected 1 object, got %d", bundle.ObjectCount)
	}
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one resolved material, got %#v", bundle.Materials)
	}
	if len(bundle.Passes) < 2 {
		t.Fatalf("expected prebatched render passes, got %#v", bundle.Passes)
	}
	if len(bundle.Objects) != 1 {
		t.Fatalf("expected one render object, got %#v", bundle.Objects)
	}
	if bundle.Objects[0].MaterialIndex != 0 {
		t.Fatalf("expected render object to reference first material, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].RenderPass != "opaque" {
		t.Fatalf("expected render object to carry resolved render pass, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].DepthNear <= 0 || bundle.Objects[0].DepthFar <= bundle.Objects[0].DepthNear {
		t.Fatalf("expected render object depth metadata, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].Bounds.MaxX <= bundle.Objects[0].Bounds.MinX || bundle.Objects[0].Bounds.MaxZ <= bundle.Objects[0].Bounds.MinZ {
		t.Fatalf("expected render object bounds metadata, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].ViewCulled {
		t.Fatalf("expected visible object to stay in-bounds, got %#v", bundle.Objects[0])
	}
	if bundle.VertexCount == 0 {
		t.Fatal("expected projected vertices in render bundle")
	}
	if len(bundle.Positions) != bundle.VertexCount*2 {
		t.Fatalf("expected positions sized to vertex count, got %d for %d vertices", len(bundle.Positions), bundle.VertexCount)
	}
	if len(bundle.Colors) != bundle.VertexCount*4 {
		t.Fatalf("expected colors sized to vertex count, got %d for %d vertices", len(bundle.Colors), bundle.VertexCount)
	}
	if bundle.WorldVertexCount == 0 {
		t.Fatal("expected world vertices in render bundle")
	}
	if len(bundle.WorldPositions) != bundle.WorldVertexCount*3 {
		t.Fatalf("expected world positions sized to world vertex count, got %d for %d vertices", len(bundle.WorldPositions), bundle.WorldVertexCount)
	}
	if len(bundle.WorldColors) != bundle.WorldVertexCount*4 {
		t.Fatalf("expected world colors sized to world vertex count, got %d for %d vertices", len(bundle.WorldColors), bundle.WorldVertexCount)
	}
	if rt.dirty[1] {
		t.Fatal("expected render bundle generation to sync dirty node snapshot")
	}
	if bundle.Passes[0].Name != "staticOpaque" || bundle.Passes[0].CacheKey == "" {
		t.Fatalf("expected static opaque pass with cache key, got %#v", bundle.Passes[0])
	}
}

func TestRuntimeRenderBundleProjectsSceneLabels(t *testing.T) {
	prog := &rootengine.Program{
		Name: "SceneLabels",
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind: "label",
				Props: map[string]islandprogram.ExprID{
					"text":       2,
					"x":          3,
					"y":          4,
					"z":          5,
					"maxWidth":   6,
					"lineHeight": 7,
					"textAlign":  8,
					"anchorX":    9,
					"anchorY":    10,
					"className":  11,
					"priority":   12,
					"collision":  13,
					"occlude":    14,
					"maxLines":   15,
					"overflow":   16,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "72", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "Scene labels make overlays first-class.", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "184", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "18", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "center", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "hero-badge", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "avoid", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitBool, Value: "true", Type: islandprogram.TypeBool},
			{Op: islandprogram.OpLitFloat, Value: "2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "ellipsis", Type: islandprogram.TypeString},
		},
	}

	rt := New(prog, `{}`)
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Labels) != 1 {
		t.Fatalf("expected one projected label, got %#v", bundle.Labels)
	}

	label := bundle.Labels[0]
	if label.Text != "Scene labels make overlays first-class." {
		t.Fatalf("unexpected label text: %q", label.Text)
	}
	if label.Position.X < 250 || label.Position.X > 390 {
		t.Fatalf("expected projected X near center, got %#v", label.Position)
	}
	if label.Position.Y >= 180 {
		t.Fatalf("expected label above center point, got %#v", label.Position)
	}
	if label.MaxWidth != 184 {
		t.Fatalf("expected max width to flow into bundle, got %#v", label)
	}
	if label.LineHeight != 18 {
		t.Fatalf("expected line height to flow into bundle, got %#v", label)
	}
	if label.TextAlign != "center" {
		t.Fatalf("expected text alignment to flow into bundle, got %#v", label)
	}
	if label.AnchorX != 0.5 || label.AnchorY != 1 {
		t.Fatalf("expected anchor metadata in bundle, got %#v", label)
	}
	if label.ClassName != "hero-badge" {
		t.Fatalf("expected class metadata in bundle, got %#v", label)
	}
	if label.Priority != 4 || label.Collision != "avoid" || !label.Occlude {
		t.Fatalf("expected placement metadata in bundle, got %#v", label)
	}
	if label.MaxLines != 2 || label.Overflow != "ellipsis" {
		t.Fatalf("expected overflow metadata in bundle, got %#v", label)
	}
}

func TestRuntimeRenderBundleResolvesMaterialPresets(t *testing.T) {
	prog := &rootengine.Program{
		Name: "MaterialProfiles",
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "ghost",
				Props: map[string]islandprogram.ExprID{
					"size":  1,
					"color": 2,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "sphere",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"radius":    3,
					"color":     4,
					"opacity":   5,
					"blendMode": 6,
					"emissive":  7,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "pyramid",
				Material: "glow",
				Props: map[string]islandprogram.ExprID{
					"size":  8,
					"color": 9,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.9", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#ffd48f", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "opaque", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.35", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#ff9cff", Type: islandprogram.TypeString},
		},
	}

	rt := New(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 3 {
		t.Fatalf("expected three materials, got %#v", bundle.Materials)
	}

	ghost := bundle.Materials[0]
	if ghost.Kind != "ghost" || ghost.BlendMode != "alpha" || ghost.RenderPass != "alpha" || ghost.Opacity >= 1 || ghost.Emissive <= 0 || ghost.Key == "" || len(ghost.ShaderData) != 3 || ghost.ShaderData[0] != 1 {
		t.Fatalf("expected ghost preset material, got %#v", ghost)
	}

	flat := bundle.Materials[1]
	if flat.Kind != "flat" || flat.BlendMode != "alpha" || flat.RenderPass != "alpha" || flat.Opacity != 0.6 || flat.Emissive != 0.35 || flat.Key == "" || len(flat.ShaderData) != 3 || flat.ShaderData[0] != 0 {
		t.Fatalf("expected explicit flat material overrides, got %#v", flat)
	}

	glow := bundle.Materials[2]
	if glow.Kind != "glow" || glow.BlendMode != "additive" || glow.RenderPass != "additive" || glow.Opacity <= 0.5 || glow.Emissive <= 0 || glow.Key == "" || len(glow.ShaderData) != 3 || glow.ShaderData[0] != 3 {
		t.Fatalf("expected glow preset material, got %#v", glow)
	}
}

func TestRuntimeRenderBundleMarksOffscreenObjectsCulled(t *testing.T) {
	prog := &rootengine.Program{
		Name: "FrustumCull",
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":    2,
					"size": 3,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "75", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "120", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
		},
	}

	rt := New(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Objects) != 1 {
		t.Fatalf("expected one render object, got %#v", bundle.Objects)
	}
	if !bundle.Objects[0].ViewCulled {
		t.Fatalf("expected far offscreen object to be marked culled, got %#v", bundle.Objects[0])
	}
	if bundle.Objects[0].VertexCount != 0 {
		t.Fatalf("expected culled object to contribute no world vertices, got %#v", bundle.Objects[0])
	}
}

func TestClipWorldSegmentForCameraClipsNearPlane(t *testing.T) {
	camera := sceneCamera{Z: 6, FOV: 72, Near: 0.05, Far: 128}
	from, to, ok := clipWorldSegmentForCamera(
		point3{X: -2, Y: 0, Z: -7},
		point3{X: 2, Y: 0, Z: 1},
		camera,
		640.0/360.0,
	)
	if !ok {
		t.Fatal("expected segment crossing near plane to stay visible")
	}
	if math.Abs(from.X+1.475) > 0.001 || math.Abs(from.Y) > 0.001 || math.Abs(from.Z+5.95) > 0.001 {
		t.Fatalf("expected clipped near-plane point, got %#v", from)
	}
	if to != (point3{X: 2, Y: 0, Z: 1}) {
		t.Fatalf("expected far endpoint to stay intact, got %#v", to)
	}
}

func TestClipWorldSegmentForCameraCullsOffscreenSegment(t *testing.T) {
	camera := sceneCamera{Z: 6, FOV: 72, Near: 0.05, Far: 128}
	_, _, ok := clipWorldSegmentForCamera(
		point3{X: 100, Y: 0, Z: 1},
		point3{X: 120, Y: 0, Z: 1},
		camera,
		640.0/360.0,
	)
	if ok {
		t.Fatal("expected fully offscreen segment to be culled")
	}
}
