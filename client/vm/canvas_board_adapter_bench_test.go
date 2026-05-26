package vm

import (
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
)

// canvasBoardBenchProgram builds an n-rect board program for adapter
// benchmarking. Each rect is laid out on a grid with literal x/y/width/color
// expressions so the VM walks a representative expression table.
func canvasBoardBenchProgram(n int) *rootengine.Program {
	prog := &rootengine.Program{Name: "BoardBench"}
	exprs := make([]islandprogram.Expr, 0, n*4)
	nodes := make([]rootengine.Node, 0, n)
	cols := 32
	exprID := func() islandprogram.ExprID {
		return islandprogram.ExprID(len(exprs))
	}
	for i := 0; i < n; i++ {
		col := i % cols
		row := i / cols
		xExpr := exprID()
		exprs = append(exprs, islandprogram.Expr{
			Op:    islandprogram.OpLitFloat,
			Type:  islandprogram.TypeFloat,
			Value: canvasBoardIntToString(col * 40),
		})
		yExpr := exprID()
		exprs = append(exprs, islandprogram.Expr{
			Op:    islandprogram.OpLitFloat,
			Type:  islandprogram.TypeFloat,
			Value: canvasBoardIntToString(row * 30),
		})
		wExpr := exprID()
		exprs = append(exprs, islandprogram.Expr{
			Op:    islandprogram.OpLitFloat,
			Type:  islandprogram.TypeFloat,
			Value: "36",
		})
		hExpr := exprID()
		exprs = append(exprs, islandprogram.Expr{
			Op:    islandprogram.OpLitFloat,
			Type:  islandprogram.TypeFloat,
			Value: "26",
		})
		colorExpr := exprID()
		exprs = append(exprs, islandprogram.Expr{
			Op:    islandprogram.OpLitString,
			Type:  islandprogram.TypeString,
			Value: "#ff8866",
		})
		nodes = append(nodes, rootengine.Node{
			Kind: "rect",
			Props: map[string]islandprogram.ExprID{
				"x":     xExpr,
				"y":     yExpr,
				"width": wExpr,
				"height": hExpr,
				"color":  colorExpr,
			},
		})
	}
	prog.Exprs = exprs
	prog.EngineNodes = nodes
	return prog
}

// BenchmarkCanvasBoardAdapterColdMount100Nodes is the F1.1 acceptance bench.
// Target: ≤ 50 ms cold mount on 100 nodes. ns/op below 50_000_000 passes.
func BenchmarkCanvasBoardAdapterColdMount100Nodes(b *testing.B) {
	prog := canvasBoardBenchProgram(100)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt := NewCanvasBoardAdapter(prog, `{}`)
		_ = rt.Reconcile()
		_ = rt.RenderBundle(1280, 720, 0)
		rt.Dispose()
	}
}

// BenchmarkCanvasBoardAdapterRenderBundle1000Nodes is the F1.1 sustained-fps
// bench. Target: 60 fps on 1000 nodes = ≤ 16.6 ms per Reconcile+RenderBundle.
// ns/op below 16_600_000 passes.
func BenchmarkCanvasBoardAdapterRenderBundle1000Nodes(b *testing.B) {
	prog := canvasBoardBenchProgram(1000)
	rt := NewCanvasBoardAdapter(prog, `{}`)
	rt.Reconcile() // warm
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rt.Reconcile()
		_ = rt.RenderBundle(1280, 720, float64(i)/60)
	}
}
