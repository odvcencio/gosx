package vm

import (
	"strconv"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
)

// scene3DBenchProgram builds n mesh nodes (mix of box + sphere) with literal
// props so buildRenderBundle exercises the full 3D vertex-baking path
// (rotation + spin + camera projection), which is what runs in WASM in-browser.
func scene3DBenchProgram(n int, sphereEvery int, segments int) *rootengine.Program {
	prog := &rootengine.Program{Name: "Scene3DBench"}
	exprs := make([]islandprogram.Expr, 0, n*14)
	nodes := make([]rootengine.Node, 0, n)
	addFloat := func(v string) islandprogram.ExprID {
		id := islandprogram.ExprID(len(exprs))
		exprs = append(exprs, islandprogram.Expr{Op: islandprogram.OpLitFloat, Type: islandprogram.TypeFloat, Value: v})
		return id
	}
	addStr := func(v string) islandprogram.ExprID {
		id := islandprogram.ExprID(len(exprs))
		exprs = append(exprs, islandprogram.Expr{Op: islandprogram.OpLitString, Type: islandprogram.TypeString, Value: v})
		return id
	}
	for i := 0; i < n; i++ {
		isSphere := sphereEvery > 0 && i%sphereEvery == 0
		kind := "box"
		if isSphere {
			kind = "sphere"
		}
		props := map[string]islandprogram.ExprID{
			"kind":      addStr(kind),
			"x":         addFloat(strconv.Itoa((i%16)*2 - 16)),
			"y":         addFloat(strconv.Itoa((i/16)%16 - 8)),
			"z":         addFloat(strconv.Itoa(-(i / 256) - 5)),
			"width":     addFloat("1.0"),
			"height":    addFloat("1.0"),
			"depth":     addFloat("1.0"),
			"radius":    addFloat("0.7"),
			"color":     addStr("#8de1ff"),
			"rotationX": addFloat("0.3"),
			"rotationY": addFloat("0.6"),
			"rotationZ": addFloat("0.1"),
			"spinY":     addFloat("0.5"),
		}
		if isSphere {
			props["segments"] = addFloat(strconv.Itoa(segments))
		}
		nodes = append(nodes, rootengine.Node{Kind: "mesh", Geometry: kind, Props: props})
	}
	prog.Exprs = exprs
	prog.EngineNodes = nodes
	return prog
}

func benchScene3D(b *testing.B, n, sphereEvery, segments int) {
	prog := scene3DBenchProgram(n, sphereEvery, segments)
	rt := NewSceneAdapter(prog, `{}`)
	rt.Reconcile()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rt.RenderBundle(1280, 720, float64(i)/60)
	}
}

// BenchmarkScene3D_1000Boxes baking 1000 spinning boxes at 1280x720; this is
// the primary CPU target (per-vertex world position + normal + camera projection).
func BenchmarkScene3D_1000Boxes(b *testing.B) { benchScene3D(b, 1000, 0, 0) }

// BenchmarkScene3D_100Boxes is a smaller spinning-box scene.
func BenchmarkScene3D_100Boxes(b *testing.B) { benchScene3D(b, 100, 0, 0) }

// BenchmarkScene3D_100Spheres bakes 100 high-segment spheres (dense vertex
// count per object, so per-vertex rotation trig dominates).
func BenchmarkScene3D_100Spheres(b *testing.B) { benchScene3D(b, 100, 1, 32) }

// BenchmarkScene3D_500Mixed mixes boxes and spheres.
func BenchmarkScene3D_500Mixed(b *testing.B) { benchScene3D(b, 500, 5, 24) }

// scene3DLitBenchProgram builds n box mesh nodes plus a directional and a point
// light so that sceneLitColorRGBAResolved (Win B) exercises real light color math.
func scene3DLitBenchProgram(n int) *rootengine.Program {
	prog := &rootengine.Program{Name: "Scene3DLitBench"}
	exprs := make([]islandprogram.Expr, 0, n*14+8)
	nodes := make([]rootengine.Node, 0, n+2)
	addFloat := func(v string) islandprogram.ExprID {
		id := islandprogram.ExprID(len(exprs))
		exprs = append(exprs, islandprogram.Expr{Op: islandprogram.OpLitFloat, Type: islandprogram.TypeFloat, Value: v})
		return id
	}
	addStr := func(v string) islandprogram.ExprID {
		id := islandprogram.ExprID(len(exprs))
		exprs = append(exprs, islandprogram.Expr{Op: islandprogram.OpLitString, Type: islandprogram.TypeString, Value: v})
		return id
	}
	// Directional light
	nodes = append(nodes, rootengine.Node{Kind: "light", Props: map[string]islandprogram.ExprID{
		"kind":       addStr("directional"),
		"color":      addStr("#ffffff"),
		"intensity":  addFloat("1.2"),
		"directionX": addFloat("0.5"),
		"directionY": addFloat("-1"),
		"directionZ": addFloat("0.3"),
	}})
	// Point light
	nodes = append(nodes, rootengine.Node{Kind: "light", Props: map[string]islandprogram.ExprID{
		"kind":      addStr("point"),
		"color":     addStr("#ff8844"),
		"intensity": addFloat("2.0"),
		"x":         addFloat("0"),
		"y":         addFloat("4"),
		"z":         addFloat("-5"),
	}})
	for i := 0; i < n; i++ {
		nodes = append(nodes, rootengine.Node{Kind: "mesh", Geometry: "box", Props: map[string]islandprogram.ExprID{
			"kind":      addStr("box"),
			"x":         addFloat(strconv.Itoa((i%16)*2 - 16)),
			"y":         addFloat(strconv.Itoa((i/16)%16 - 8)),
			"z":         addFloat(strconv.Itoa(-(i / 256) - 5)),
			"width":     addFloat("1.0"),
			"height":    addFloat("1.0"),
			"depth":     addFloat("1.0"),
			"color":     addStr("#8de1ff"),
			"rotationX": addFloat("0.3"),
			"rotationY": addFloat("0.6"),
			"rotationZ": addFloat("0.1"),
			"spinY":     addFloat("0.5"),
		}})
	}
	prog.Exprs = exprs
	prog.EngineNodes = nodes
	return prog
}

func benchScene3DLit(b *testing.B, n int) {
	prog := scene3DLitBenchProgram(n)
	rt := NewSceneAdapter(prog, `{}`)
	rt.Reconcile()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rt.RenderBundle(1280, 720, float64(i)/60)
	}
}

// BenchmarkScene3D_1000BoxesLit is the primary Win B target: 1000 spinning boxes
// with 2 lights, exercising per-vertex lighting with pre-resolved color strings.
func BenchmarkScene3D_1000BoxesLit(b *testing.B) { benchScene3DLit(b, 1000) }

// BenchmarkScene3D_100BoxesLit is a smaller lit scene for quick iteration.
func BenchmarkScene3D_100BoxesLit(b *testing.B) { benchScene3DLit(b, 100) }
