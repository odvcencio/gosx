package vm

import (
	"sort"
	"strings"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
)

// gridBoardProgram builds a multi-rect CanvasBoard program from a slice of
// (id, x, y, w, h) tuples so the marquee + nav tests can lay out a spatial
// grid of pickable nodes. The rects paint in slice order (back-to-front).
type rectSpec struct {
	id         string
	x, y, w, h float64
}

func gridBoardProgram(specs []rectSpec) *rootengine.Program {
	prog := &rootengine.Program{Name: "GridBoard"}
	exprID := func(v islandprogram.Expr) islandprogram.ExprID {
		prog.Exprs = append(prog.Exprs, v)
		return islandprogram.ExprID(len(prog.Exprs) - 1)
	}
	for _, s := range specs {
		node := rootengine.Node{
			Kind: "rect",
			Props: map[string]islandprogram.ExprID{
				"x":      exprID(islandprogram.Expr{Op: islandprogram.OpLitFloat, Value: ftoa(s.x), Type: islandprogram.TypeFloat}),
				"y":      exprID(islandprogram.Expr{Op: islandprogram.OpLitFloat, Value: ftoa(s.y), Type: islandprogram.TypeFloat}),
				"width":  exprID(islandprogram.Expr{Op: islandprogram.OpLitFloat, Value: ftoa(s.w), Type: islandprogram.TypeFloat}),
				"height": exprID(islandprogram.Expr{Op: islandprogram.OpLitFloat, Value: ftoa(s.h), Type: islandprogram.TypeFloat}),
				"id":     exprID(islandprogram.Expr{Op: islandprogram.OpLitString, Value: s.id, Type: islandprogram.TypeString}),
			},
		}
		prog.EngineNodes = append(prog.EngineNodes, node)
	}
	return prog
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// --- PickWorldRect -----------------------------------------------------------

// TestPickWorldRectReturnsIntersectingNodes is the marquee keystone: every
// PICKABLE rect whose world bounds intersect the rect is returned (back-to-front
// order). A rect entirely outside is excluded; a rect partially overlapping is
// included.
func TestPickWorldRectReturnsIntersectingNodes(t *testing.T) {
	// Three rects laid out left→right. The marquee covers world x∈[0,250],
	// y∈[0,120], which fully covers A and B and clips the left edge of C.
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		{"A", 0, 0, 100, 100},   // x∈[0,100]   — inside
		{"B", 120, 0, 100, 100}, // x∈[120,220] — inside
		{"C", 240, 0, 100, 100}, // x∈[240,340] — clipped at x=250 (intersects)
		{"D", 400, 0, 100, 100}, // x∈[400,500] — fully outside
	}), `{}`)

	got := rt.PickWorldRect(0, 0, 250, 120)
	want := []string{"A", "B", "C"}
	if strings.Join(sortedCopy(got), ",") != strings.Join(want, ",") {
		t.Errorf("PickWorldRect = %v, want %v (D must be excluded)", got, want)
	}
}

// TestPickWorldRectBackToFrontOrder verifies the returned ids follow painter's
// (slice) order so the FIRST id can serve as the primary selection consistently
// with PickWorld's topmost rule (last-painted wins → reversed here, but the
// multi-list itself is back-to-front).
func TestPickWorldRectBackToFrontOrder(t *testing.T) {
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		{"first", 0, 0, 50, 50},
		{"second", 10, 10, 50, 50},
		{"third", 20, 20, 50, 50},
	}), `{}`)
	got := rt.PickWorldRect(-10, -10, 100, 100)
	want := []string{"first", "second", "third"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("PickWorldRect order = %v, want %v (slice/back-to-front order)", got, want)
	}
}

// TestPickWorldRectSkipsNonPickable verifies pickable={false} rects are
// transparent to the marquee just as they are to a single pick.
func TestPickWorldRectSkipsNonPickable(t *testing.T) {
	prog := &rootengine.Program{
		Name: "MarqueeNonPickable",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "id": 4}},
			{Kind: "rect", Props: map[string]islandprogram.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "id": 5, "pickable": 6}},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "100", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "100", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "real", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitString, Value: "ghost", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitBool, Value: "false", Type: islandprogram.TypeBool},
		},
	}
	rt := NewCanvasBoardAdapter(prog, `{}`)
	got := rt.PickWorldRect(-10, -10, 200, 200)
	if strings.Join(got, ",") != "real" {
		t.Errorf("PickWorldRect = %v, want [real] (ghost has pickable=false)", got)
	}
}

// TestPickWorldRectStaticPropsNodes verifies the marquee works for the static
// props.nodes path — the exact shape Muddy's site-map board uses (no compiled
// EngineNodes).
func TestPickWorldRectStaticPropsNodes(t *testing.T) {
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "StaticMarquee"}, `{
		"nodes": [
			{"kind": "rect", "id": "page-home", "x": 0, "y": 0, "width": 100, "height": 60},
			{"kind": "rect", "id": "page-about", "x": 150, "y": 0, "width": 100, "height": 60},
			{"kind": "rect", "id": "page-faraway", "x": 900, "y": 900, "width": 100, "height": 60}
		]
	}`)
	got := rt.PickWorldRect(-20, -20, 300, 200)
	want := []string{"page-about", "page-home"}
	if strings.Join(sortedCopy(got), ",") != strings.Join(want, ",") {
		t.Errorf("static-props PickWorldRect = %v, want %v", got, want)
	}
}

// TestPickWorldRectNormalizesCorners verifies a rect given with swapped/inverted
// corners (max<min) is normalized so a marquee dragged up-left still selects.
func TestPickWorldRectNormalizesCorners(t *testing.T) {
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		{"A", 0, 0, 100, 100},
	}), `{}`)
	// Inverted: minX > maxX and minY > maxY (drag started bottom-right).
	got := rt.PickWorldRect(250, 250, -50, -50)
	if strings.Join(got, ",") != "A" {
		t.Errorf("PickWorldRect with inverted corners = %v, want [A]", got)
	}
}

// TestPickWorldRectEmptyOnNoHit verifies an empty (nil) slice when nothing
// intersects (a click-drag in empty space clears the multi-selection).
func TestPickWorldRectEmptyOnNoHit(t *testing.T) {
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		{"A", 0, 0, 100, 100},
	}), `{}`)
	if got := rt.PickWorldRect(500, 500, 600, 600); len(got) != 0 {
		t.Errorf("PickWorldRect in empty space = %v, want empty", got)
	}
}

// --- NavFrom -----------------------------------------------------------------

// TestNavFromDirectionalNeighbors lays out a plus-shaped grid and asserts each
// arrow direction lands on the directly-adjacent node. Coordinate orientation:
// the canvas paints with world +Y UP (the OrthoCamera2D flip), so "up" on screen
// is +Y in world. The cost model matches the DOM board's nearestNodeKey
// (primary-axis distance + 2× perpendicular penalty, half-plane filtered).
func TestNavFromDirectionalNeighbors(t *testing.T) {
	// Centers: C=(100,100). Up=(100,200) larger Y, Down=(100,0), Left=(0,100),
	// Right=(200,100). Rects are 20×20 centered on those points.
	mk := func(id string, cx, cy float64) rectSpec {
		return rectSpec{id, cx - 10, cy - 10, 20, 20}
	}
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		mk("C", 100, 100),
		mk("U", 100, 200),
		mk("D", 100, 0),
		mk("L", 0, 100),
		mk("R", 200, 100),
	}), `{}`)

	cases := []struct {
		dir  string
		want string
	}{
		{"up", "U"},
		{"down", "D"},
		{"left", "L"},
		{"right", "R"},
	}
	for _, c := range cases {
		if got := rt.NavFrom("C", c.dir); got != c.want {
			t.Errorf("NavFrom(C, %q) = %q, want %q", c.dir, got, c.want)
		}
	}
}

// TestNavFromHalfPlaneFilter verifies a node behind the pressed direction is
// never chosen even when it is much closer than the on-axis candidate (the
// strict half-plane filter, exactly like the DOM board).
func TestNavFromHalfPlaneFilter(t *testing.T) {
	mk := func(id string, cx, cy float64) rectSpec {
		return rectSpec{id, cx - 10, cy - 10, 20, 20}
	}
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		mk("C", 100, 100),
		mk("close-behind", 100, 95), // just below — must be ignored for "up"
		mk("far-ahead", 100, 400),   // far above — the only valid "up" target
	}), `{}`)
	if got := rt.NavFrom("C", "up"); got != "far-ahead" {
		t.Errorf("NavFrom(C, up) = %q, want far-ahead (close-behind is below, filtered out)", got)
	}
}

// TestNavFromPrefersOnAxis verifies the 2× perpendicular penalty makes a node
// directly in the pressed direction win over a closer-but-off-axis node.
func TestNavFromPrefersOnAxis(t *testing.T) {
	mk := func(id string, cx, cy float64) rectSpec {
		return rectSpec{id, cx - 10, cy - 10, 20, 20}
	}
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		mk("C", 0, 0),
		// on-axis: dx=100, perpendicular 0 → cost 100.
		mk("on-axis", 100, 0),
		// diagonal: dx=70, dy=70 → cost 70 + 2*70 = 210 (loses despite closer dx).
		mk("diagonal", 70, 70),
	}), `{}`)
	if got := rt.NavFrom("C", "right"); got != "on-axis" {
		t.Errorf("NavFrom(C, right) = %q, want on-axis (2x perpendicular penalty)", got)
	}
}

// TestNavFromEmptyReturnsTopmostLeftmost verifies an empty current id returns
// the topmost-leftmost node. Topmost on screen = largest world-Y (the Y flip);
// ties broken by smallest world-X.
func TestNavFromEmptyReturnsTopmostLeftmost(t *testing.T) {
	mk := func(id string, cx, cy float64) rectSpec {
		return rectSpec{id, cx - 10, cy - 10, 20, 20}
	}
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		mk("bottom", 50, 0),
		mk("top-right", 300, 500), // highest Y
		mk("top-left", 50, 500),   // same (highest) Y, smaller X → topmost-leftmost
	}), `{}`)
	if got := rt.NavFrom("", "down"); got != "top-left" {
		t.Errorf("NavFrom(\"\", down) = %q, want top-left (topmost-leftmost)", got)
	}
}

// TestNavFromNoCandidateReturnsEmpty verifies that when no node lies in the
// pressed direction, NavFrom returns "" (the selection stays put — the bridge
// leaves selectedID unchanged on an empty result).
func TestNavFromNoCandidateReturnsEmpty(t *testing.T) {
	mk := func(id string, cx, cy float64) rectSpec {
		return rectSpec{id, cx - 10, cy - 10, 20, 20}
	}
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		mk("C", 0, 0),
		mk("only-left", -100, 0),
	}), `{}`)
	if got := rt.NavFrom("C", "right"); got != "" {
		t.Errorf("NavFrom(C, right) = %q, want \"\" (no node to the right)", got)
	}
}

// TestNavFromUnknownCurrentReturnsEmpty verifies a current id that no longer
// matches any node yields "" (defensive — never panics, never guesses).
func TestNavFromUnknownCurrentReturnsEmpty(t *testing.T) {
	rt := NewCanvasBoardAdapter(gridBoardProgram([]rectSpec{
		{"A", 0, 0, 100, 100},
	}), `{}`)
	if got := rt.NavFrom("does-not-exist", "up"); got != "" {
		t.Errorf("NavFrom(unknown, up) = %q, want \"\"", got)
	}
}

// TestNavFromStaticPropsNodes verifies nav works on the static props.nodes path
// (the Muddy board shape).
func TestNavFromStaticPropsNodes(t *testing.T) {
	rt := NewCanvasBoardAdapter(&rootengine.Program{Name: "StaticNav"}, `{
		"nodes": [
			{"kind": "rect", "id": "home", "x": 90, "y": 90, "width": 20, "height": 20},
			{"kind": "rect", "id": "about", "x": 190, "y": 90, "width": 20, "height": 20}
		]
	}`)
	if got := rt.NavFrom("home", "right"); got != "about" {
		t.Errorf("static-props NavFrom(home, right) = %q, want about", got)
	}
}
