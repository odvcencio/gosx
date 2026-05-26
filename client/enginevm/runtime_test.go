package enginevm

import (
	"math"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

func TestRuntimeInitialReconcileCreatesObjects(t *testing.T) {
	prog := &rootengine.Program{
		Name: "GeometryZoo",
		EngineNodes: []rootengine.Node{
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
		EngineNodes: []rootengine.Node{
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

func TestRuntimeTickProducesIncrementalLightCommands(t *testing.T) {
	prog := &rootengine.Program{
		Name: "SceneLights",
		EngineNodes: []rootengine.Node{
			{
				Kind: "light",
				Props: map[string]islandprogram.ExprID{
					"kind":      0,
					"color":     1,
					"intensity": 2,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitString, Value: "directional", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "#f4fbff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpSignalGet, Value: "$scene.light", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.8", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.light", Type: islandprogram.TypeFloat, Init: 3},
		},
	}

	rt := New(prog, `{}`)
	intensitySig := signal.New(vm.FloatVal(0.8))
	rt.SetSharedSignal("$scene.light", intensitySig)

	if commands := rt.Reconcile(); len(commands) != 1 || commands[0].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected initial create command, got %#v", commands)
	}

	intensitySig.Set(vm.FloatVal(1.6))
	commands := rt.Reconcile()
	if len(commands) != 1 {
		t.Fatalf("expected one light command, got %#v", commands)
	}
	if commands[0].Kind != rootengine.CommandSetLight {
		t.Fatalf("expected SetLight command, got %v", commands[0].Kind)
	}
}

func TestRuntimeMarksOnlyDependentNodesDirty(t *testing.T) {
	prog := &rootengine.Program{
		Name: "DirtyTracking",
		EngineNodes: []rootengine.Node{
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
		EngineNodes: []rootengine.Node{
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
		EngineNodes: []rootengine.Node{
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
		EngineNodes: []rootengine.Node{
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
		EngineNodes: []rootengine.Node{
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

func TestRuntimeRenderBundleProjectsSceneSprites(t *testing.T) {
	prog := &rootengine.Program{
		Name: "SceneSprites",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind: "sprite",
				Props: map[string]islandprogram.ExprID{
					"src":       2,
					"x":         3,
					"y":         4,
					"z":         5,
					"width":     6,
					"height":    7,
					"scale":     8,
					"opacity":   9,
					"className": 10,
					"priority":  11,
					"anchorX":   12,
					"anchorY":   13,
					"fit":       14,
					"occlude":   15,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "72", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "/paper-card.png", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.25", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.55", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.02", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.94", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "hero-card", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "3", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "cover", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitBool, Value: "true", Type: islandprogram.TypeBool},
		},
	}

	rt := New(prog, `{}`)
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Sprites) != 1 {
		t.Fatalf("expected one projected sprite, got %#v", bundle.Sprites)
	}
	sprite := bundle.Sprites[0]
	if sprite.Src != "/paper-card.png" {
		t.Fatalf("unexpected sprite src: %#v", sprite)
	}
	if sprite.Position.X < 250 || sprite.Position.X > 410 {
		t.Fatalf("expected sprite projected near center, got %#v", sprite.Position)
	}
	if sprite.Width <= 30 || sprite.Height <= 20 {
		t.Fatalf("expected projected sprite dimensions, got %#v", sprite)
	}
	if sprite.ClassName != "hero-card" || sprite.Fit != "cover" || !sprite.Occlude {
		t.Fatalf("expected sprite metadata in bundle, got %#v", sprite)
	}
	if sprite.Opacity != 0.94 || sprite.AnchorX != 0.5 || sprite.AnchorY != 0.5 {
		t.Fatalf("expected sprite presentation metadata in bundle, got %#v", sprite)
	}
}

func TestRuntimeRenderBundleResolvesMaterialPresets(t *testing.T) {
	prog := &rootengine.Program{
		Name: "MaterialProfiles",
		EngineNodes: []rootengine.Node{
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

func TestRuntimeRenderBundleUsesRegisteredMaterialProfile(t *testing.T) {
	cleanup := RegisterMaterialProfile("cloth", MaterialProfile{
		Opacity:       0.64,
		HasOpacity:    true,
		BlendMode:     "alpha",
		HasBlendMode:  true,
		Emissive:      0.18,
		HasEmissive:   true,
		Clearcoat:     0.22,
		HasClearcoat:  true,
		Anisotropy:    0.4,
		HasAnisotropy: true,
		ShaderData:    []float64{7, 0.18, 0.44},
	})
	defer cleanup()

	prog := &rootengine.Program{
		Name: "CustomMaterialProfile",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "cloth",
				Props: map[string]islandprogram.ExprID{
					"size":  1,
					"color": 2,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#d8b4fe", Type: islandprogram.TypeString},
		},
	}

	rt := New(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one material, got %#v", bundle.Materials)
	}
	material := bundle.Materials[0]
	if material.Kind != "cloth" || material.Opacity != 0.64 || material.BlendMode != "alpha" || material.RenderPass != "alpha" || material.Emissive != 0.18 || material.Clearcoat != 0.22 || material.Anisotropy != 0.4 {
		t.Fatalf("expected registered cloth defaults, got %#v", material)
	}
	if len(material.ShaderData) != 3 || material.ShaderData[0] != 7 || material.ShaderData[2] != 0.44 {
		t.Fatalf("expected registered cloth shader data, got %#v", material.ShaderData)
	}
}

func TestRuntimeRenderBundlePreservesCustomWGSLMaterial(t *testing.T) {
	prog := &rootengine.Program{
		Name: "CustomWGSLMaterial",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "custom",
				Props: map[string]islandprogram.ExprID{
					"size":               1,
					"color":              2,
					"customVertexWGSL":   3,
					"customFragmentWGSL": 4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#f5c76b", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "fn gosx_vertex() {}", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "fn gosx_fragment() -> vec4f { return vec4f(1.0); }", Type: islandprogram.TypeString},
		},
	}

	rt := New(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one material, got %#v", bundle.Materials)
	}
	material := bundle.Materials[0]
	if material.Kind != "custom" || material.Color != "#f5c76b" {
		t.Fatalf("expected custom material, got %#v", material)
	}
	if material.CustomVertexWGSL != "fn gosx_vertex() {}" {
		t.Fatalf("CustomVertexWGSL = %q", material.CustomVertexWGSL)
	}
	if material.CustomFragmentWGSL != "fn gosx_fragment() -> vec4f { return vec4f(1.0); }" {
		t.Fatalf("CustomFragmentWGSL = %q", material.CustomFragmentWGSL)
	}
	if !strings.Contains(material.Key, "fn gosx_fragment") {
		t.Fatalf("material key should include custom WGSL, got %q", material.Key)
	}
}

func TestRuntimeRenderBundlePreservesPBRMaterialFields(t *testing.T) {
	prog := &rootengine.Program{
		Name: "PBRMaterial",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "sphere",
				Material: "standard",
				Props: map[string]islandprogram.ExprID{
					"size":         1,
					"color":        2,
					"roughness":    3,
					"metalness":    4,
					"texture":      5,
					"normalMap":    6,
					"roughnessMap": 7,
					"metalnessMap": 8,
					"emissive":     9,
					"emissiveMap":  10,
					"clearcoat":    11,
					"sheen":        12,
					"transmission": 13,
					"iridescence":  14,
					"anisotropy":   15,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#77c6ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.32", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.8", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "/albedo.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "/normal.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "/roughness.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "/metalness.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.27", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "/emissive.webp", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.35", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.12", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.18", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-0.25", Type: islandprogram.TypeFloat},
		},
	}

	rt := New(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one material, got %#v", bundle.Materials)
	}
	material := bundle.Materials[0]
	if material.Kind != "standard" || material.Color != "#77c6ff" {
		t.Fatalf("unexpected material identity: %#v", material)
	}
	if material.Roughness != 0.32 || material.Metalness != 0.8 || material.Emissive != 0.27 {
		t.Fatalf("PBR scalar fields were not preserved: %#v", material)
	}
	if material.Clearcoat != 0.35 || material.Sheen != 0.2 || material.Transmission != 0.12 || material.Iridescence != 0.18 || material.Anisotropy != -0.25 {
		t.Fatalf("physical PBR fields were not preserved: %#v", material)
	}
	if material.Texture != "/albedo.webp" || material.NormalMap != "/normal.webp" || material.RoughnessMap != "/roughness.webp" || material.MetalnessMap != "/metalness.webp" || material.EmissiveMap != "/emissive.webp" {
		t.Fatalf("PBR texture maps were not preserved: %#v", material)
	}
	for _, fragment := range []string{"/normal.webp", "/roughness.webp", "/metalness.webp", "/emissive.webp", "0.320", "0.800", "0.350", "0.200", "0.120", "0.180", "-0.250"} {
		if !strings.Contains(material.Key, fragment) {
			t.Fatalf("material key %q does not include %q", material.Key, fragment)
		}
	}
}

func TestRuntimeRenderBundlePropagatesNativePostEffects(t *testing.T) {
	props := `{
		"scene": {
			"postEffects": [
				{"kind": "bloom", "threshold": 0.7, "intensity": 0.45, "radius": 6, "scale": 0.5},
				{"kind": "dof", "focusDistance": 7, "aperture": 0.05, "maxBlur": 4},
				{"kind": "vignette", "intensity": 0.2},
				{"kind": "colorGrade", "exposure": 1.1, "contrast": 0.9, "saturation": 0.8},
				{"kind": "toneMapping", "mode": "reinhard", "exposure": 1.2}
			],
			"postFXMaxPixels": 921600
		}
	}`
	rt := New(&rootengine.Program{}, props)
	bundle := rt.RenderBundle(640, 360, 0)

	if bundle.PostFXMaxPixels != 921600 {
		t.Fatalf("PostFXMaxPixels = %d, want 921600", bundle.PostFXMaxPixels)
	}
	if len(bundle.PostEffects) != 5 {
		t.Fatalf("PostEffects = %#v, want bloom, native DOF, vignette, colorGrade, and preserved toneMapping", bundle.PostEffects)
	}
	bloom := bundle.PostEffects[0]
	if bloom.Kind != "bloom" || bloom.Threshold != 0.7 || bloom.Intensity != 0.45 || bloom.Radius != 6 || bloom.Scale != 0.5 {
		t.Fatalf("unexpected bloom effect: %#v", bloom)
	}
	if bloom.Params["threshold"] != 0.7 || bloom.Params["intensity"] != 0.45 {
		t.Fatalf("bloom params = %#v", bloom.Params)
	}
	dof := bundle.PostEffects[1]
	if dof.Kind != "dof" || dof.Params["focusDistance"] != 7 || dof.Params["aperture"] != 0.05 || dof.Params["maxBlur"] != 4 {
		t.Fatalf("DOF should be preserved with params, got %#v", dof)
	}
	vignette := bundle.PostEffects[2]
	if vignette.Kind != "vignette" || vignette.Params["intensity"] != 0.2 {
		t.Fatalf("vignette should be preserved with params, got %#v", vignette)
	}
	colorGrade := bundle.PostEffects[3]
	if colorGrade.Kind != "colorGrade" || colorGrade.Params["exposure"] != 1.1 || colorGrade.Params["contrast"] != 0.9 || colorGrade.Params["saturation"] != 0.8 {
		t.Fatalf("color grade should be preserved with params, got %#v", colorGrade)
	}
	toneMapping := bundle.PostEffects[4]
	if toneMapping.Kind != "toneMapping" || toneMapping.Mode != "reinhard" || toneMapping.Params["exposure"] != 1.2 {
		t.Fatalf("toneMapping should be preserved with params, got %#v", toneMapping)
	}
	if len(bundle.Diagnostics) != 0 {
		t.Fatalf("Diagnostics = %#v, want all listed post-FX supported by native engine VM", bundle.Diagnostics)
	}
}

func TestRuntimeRenderBundlePreservesSceneAnimations(t *testing.T) {
	props := `{
		"scene": {
			"animations": [{
				"name": "pulse",
				"duration": 1.5,
				"channels": [{
					"targetNode": 4,
					"property": "rotationY",
					"times": [0, 1.5],
					"values": [0, 3.14],
					"interpolation": "LINEAR"
				}]
			}]
		}
	}`
	rt := New(&rootengine.Program{}, props)
	bundle := rt.RenderBundle(640, 360, 0)

	if len(bundle.Animations) != 1 {
		t.Fatalf("Animations = %#v, want one clip", bundle.Animations)
	}
	clip := bundle.Animations[0]
	if clip.Name != "pulse" || clip.Duration != 1.5 || len(clip.Channels) != 1 {
		t.Fatalf("animation clip = %#v", clip)
	}
	channel := clip.Channels[0]
	if channel.TargetID != "4" || channel.Property != "rotationY" || channel.Interpolation != "LINEAR" {
		t.Fatalf("animation channel = %#v", channel)
	}
	if len(channel.Times) != 2 || channel.Times[1] != 1.5 || len(channel.Values) != 2 || channel.Values[1] != 3.14 {
		t.Fatalf("animation keyframes = times %#v values %#v", channel.Times, channel.Values)
	}
}

func TestRuntimeRenderBundleEmitsTexturedPlaneSurfaces(t *testing.T) {
	prog := &rootengine.Program{
		Name: "TexturedPlane",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z":   0,
					"fov": 1,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "plane",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"width":     2,
					"height":    3,
					"texture":   4,
					"wireframe": 5,
					"y":         6,
					"z":         7,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "72", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.55", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.02", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "/paper-card.png", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitBool, Value: "false", Type: islandprogram.TypeBool},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.25", Type: islandprogram.TypeFloat},
		},
	}

	rt := New(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Materials) != 1 {
		t.Fatalf("expected one resolved material, got %#v", bundle.Materials)
	}
	if bundle.Materials[0].Texture != "/paper-card.png" {
		t.Fatalf("expected texture to flow into resolved material, got %#v", bundle.Materials[0])
	}
	if len(bundle.Surfaces) != 1 {
		t.Fatalf("expected one textured surface, got %#v", bundle.Surfaces)
	}
	surface := bundle.Surfaces[0]
	if surface.Kind != "plane" {
		t.Fatalf("expected plane surface, got %#v", surface)
	}
	if surface.MaterialIndex != 0 {
		t.Fatalf("expected surface to reference first material, got %#v", surface)
	}
	if surface.RenderPass != "opaque" {
		t.Fatalf("expected opaque textured surface, got %#v", surface)
	}
	if surface.VertexCount != 6 {
		t.Fatalf("expected two surface triangles, got %#v", surface)
	}
	if len(surface.Positions) != 18 || len(surface.UV) != 12 {
		t.Fatalf("expected surface vertex buffers, got %#v", surface)
	}
	if surface.ViewCulled {
		t.Fatalf("expected textured plane to remain visible, got %#v", surface)
	}
}

func TestRuntimeRenderBundleCarriesPickabilityMetadata(t *testing.T) {
	prog := &rootengine.Program{
		Name: "Pickability",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"z": 0,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":        1,
					"pickable": 2,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":        3,
					"pickable": 4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-1.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitBool, Value: "false", Type: islandprogram.TypeBool},
			{Op: islandprogram.OpLitFloat, Value: "1.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitBool, Value: "true", Type: islandprogram.TypeBool},
		},
	}

	rt := New(prog, "")
	bundle := rt.RenderBundle(640, 360, 0)
	if len(bundle.Objects) != 2 {
		t.Fatalf("expected two render objects, got %#v", bundle.Objects)
	}
	if bundle.Objects[0].Pickable == nil || *bundle.Objects[0].Pickable {
		t.Fatalf("expected explicit non-pickable metadata, got %#v", bundle.Objects[0].Pickable)
	}
	if bundle.Objects[1].Pickable == nil || !*bundle.Objects[1].Pickable {
		t.Fatalf("expected explicit pickable metadata, got %#v", bundle.Objects[1].Pickable)
	}
}

func TestRuntimeRenderBundleAppliesSceneLightingAndInvalidatesStaticPassCache(t *testing.T) {
	prog := &rootengine.Program{
		Name: "LightingCache",
		EngineNodes: []rootengine.Node{
			{
				Kind: "light",
				Props: map[string]islandprogram.ExprID{
					"kind":       0,
					"color":      1,
					"intensity":  2,
					"directionX": 3,
					"directionY": 4,
					"directionZ": 5,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Static:   true,
				Props: map[string]islandprogram.ExprID{
					"size":  6,
					"color": 7,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitString, Value: "directional", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "#fff1d6", Type: islandprogram.TypeString},
			{Op: islandprogram.OpSignalGet, Value: "$scene.sun", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.35", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-0.4", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "1.6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0.9", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.sun", Type: islandprogram.TypeFloat, Init: 8},
		},
	}

	rt := New(prog, `{"scene":{"environment":{"ambientColor":"#f4fbff","ambientIntensity":0.15,"skyColor":"#b9deff","skyIntensity":0.1,"groundColor":"#102030","groundIntensity":0.04,"exposure":1.1}}}`)
	intensitySig := signal.New(vm.FloatVal(0.9))
	rt.SetSharedSignal("$scene.sun", intensitySig)
	rt.Reconcile()

	first := rt.RenderBundle(640, 360, 0)
	if len(first.Lights) != 1 {
		t.Fatalf("expected one render light, got %#v", first.Lights)
	}
	if first.Environment.AmbientIntensity != 0.15 || first.Environment.Exposure != 1.1 {
		t.Fatalf("expected environment from props, got %#v", first.Environment)
	}
	if len(first.Passes) == 0 || first.Passes[0].CacheKey == "" {
		t.Fatalf("expected static pass cache key, got %#v", first.Passes)
	}

	intensitySig.Set(vm.FloatVal(1.8))
	second := rt.RenderBundle(640, 360, 0)
	if len(second.WorldColors) == 0 || len(first.WorldColors) != len(second.WorldColors) {
		t.Fatalf("expected comparable lit world colors, got %#v and %#v", first.WorldColors, second.WorldColors)
	}
	if second.Passes[0].CacheKey == first.Passes[0].CacheKey {
		t.Fatalf("expected static pass cache key to change with lighting, got %q", second.Passes[0].CacheKey)
	}
	if second.WorldColors[0] == first.WorldColors[0] && second.WorldColors[1] == first.WorldColors[1] && second.WorldColors[2] == first.WorldColors[2] {
		t.Fatalf("expected lighting to alter world colors, got %#v and %#v", first.WorldColors[:4], second.WorldColors[:4])
	}
}

func TestRuntimeRenderBundleMarksOffscreenObjectsCulled(t *testing.T) {
	prog := &rootengine.Program{
		Name: "FrustumCull",
		EngineNodes: []rootengine.Node{
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
