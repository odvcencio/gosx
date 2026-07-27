package graphsurface

import (
	"math"
	"math/rand"

	"m31labs.dev/gosx/engine/surface"
)

// GraphNode is the JSON shape for a graph node in the surface props.
type GraphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

// GraphEdge is the JSON shape for a graph edge in the surface props.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

// GraphProps is the server-side props struct for the Graph canvas surface.
// It is JSON-serialised by surface.NewRenderer("Graph").Mount(props) and
// delivered to the WASM module at mount time via data-gosx-engine-props.
type GraphProps struct {
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
	Center string      `json:"center,omitempty"`
}

// vec2 is a 2-D float64 vector.
type vec2 struct{ X, Y float64 }

// mat2d is the pan + zoom transform state (no rotation for v0.1).
type mat2d struct{ X, Y, Scale float64 }

// dragState holds pointer-drag bookkeeping.
type dragState struct {
	Active         bool
	NodeID         string  // empty when panning the whole canvas
	StartX, StartY float64 // screen coords at drag start
	OrigPos        vec2    // world pos of dragged node at drag start
	HasMoved       bool    // true once we see a pointermove during the drag
}

// ---- Per-surface state (single Graph surface per page for v0.1) ----

var (
	gNodes    []GraphNode
	gEdges    []GraphEdge
	gPos      = map[string]vec2{} // world-space position per node id
	gVel      = map[string]vec2{} // velocity per node id
	gSelected string              // currently selected node id
	gTx       = mat2d{Scale: 1.0} // pan + zoom transform
	gDrag     dragState
)

// ---- Force-directed layout constants (ported from pageJS) ----

const (
	fdRepel    = 4500.0
	fdAttract  = 0.012
	fdDamping  = 0.82
	fdIdeal    = 120.0
	fdMinSpeed = 0.1
)

// ---- Surface handlers ----

// Mount is called once when the WASM module mounts the canvas element.
// It decodes props, seeds positions, and starts the animation loop.
func Mount(ctx *surface.Context, c *surface.Canvas) {
	var props GraphProps
	_ = ctx.PropsInto(&props)
	gNodes = props.Nodes
	gEdges = props.Edges
	initPositions(c.Width(), c.Height())
	c.StartLoop(func(dt float64) {
		if stepLayout(dt) || gDrag.Active {
			draw(c)
		}
	})
}

// OnDown handles pointerdown: hit-test a node or start canvas pan.
func OnDown(_ *surface.Context, c *surface.Canvas, e surface.PointerEvent) {
	wx, wy := screenToWorld(e.X, e.Y)
	if hit := nodeAt(wx, wy); hit != "" {
		gDrag = dragState{
			Active:   true,
			NodeID:   hit,
			StartX:   e.X,
			StartY:   e.Y,
			OrigPos:  gPos[hit],
			HasMoved: false,
		}
		gSelected = hit
		draw(c)
	} else {
		gDrag = dragState{
			Active:   true,
			NodeID:   "",
			StartX:   e.X,
			StartY:   e.Y,
			HasMoved: false,
		}
	}
}

// OnMove handles pointermove: drag a node or pan the canvas.
func OnMove(_ *surface.Context, c *surface.Canvas, e surface.PointerEvent) {
	if !gDrag.Active {
		return
	}
	dx := e.X - gDrag.StartX
	dy := e.Y - gDrag.StartY
	if math.Abs(dx)+math.Abs(dy) > 2 {
		gDrag.HasMoved = true
	}
	if gDrag.NodeID != "" {
		wx, wy := screenToWorld(e.X, e.Y)
		gPos[gDrag.NodeID] = vec2{wx, wy}
		gVel[gDrag.NodeID] = vec2{}
	} else {
		gTx.X += dx
		gTx.Y += dy
		gDrag.StartX = e.X
		gDrag.StartY = e.Y
	}
	draw(c)
}

// OnUp handles pointerup: finish drag; treat as click if barely moved.
func OnUp(_ *surface.Context, c *surface.Canvas, e surface.PointerEvent) {
	if !gDrag.Active {
		return
	}
	if gDrag.NodeID != "" && !gDrag.HasMoved {
		// Treated as a click: selection is already set in onDown.
		//
		// v0.1 NOTE: Notifying the detail panel JS about the selected node
		// requires a back-channel to the DOM. In a future version this should call:
		//   js.Global().Call("__hypha_select_node", gSelected)
		// where "__hypha_select_node" is exposed by panelJS in page.go.
		// This wiring is left as a stub for v0.1.4 when the detail panel
		// becomes a //gosx:island.
		_ = gSelected
	}
	if gDrag.NodeID != "" {
		gVel[gDrag.NodeID] = vec2{}
	}
	gDrag = dragState{}
	draw(c)
}

// OnZoom handles wheel events: scale the transform centred on the cursor.
func OnZoom(_ *surface.Context, c *surface.Canvas, e surface.WheelEvent) {
	factor := 1.1
	if e.DeltaY > 0 {
		factor = 0.9
	}
	mx, my := e.X, e.Y
	gTx.X = mx - factor*(mx-gTx.X)
	gTx.Y = my - factor*(my-gTx.Y)
	gTx.Scale *= factor
	if gTx.Scale < 0.1 {
		gTx.Scale = 0.1
	}
	if gTx.Scale > 5.0 {
		gTx.Scale = 5.0
	}
	draw(c)
}

// OnDouble handles double-click: re-centre the graph on the hit node.
// For v0.1 this resets the pan/zoom so the selected node is centred on screen.
// A full graph reload (recenterOn in panelJS) will be wired in v0.1.4.
func OnDouble(_ *surface.Context, c *surface.Canvas, e surface.PointerEvent) {
	wx, wy := screenToWorld(e.X, e.Y)
	if hit := nodeAt(wx, wy); hit != "" {
		gSelected = hit
		p := gPos[hit]
		gTx.X = float64(c.Width())/2 - p.X*gTx.Scale
		gTx.Y = float64(c.Height())/2 - p.Y*gTx.Scale
		draw(c)
	}
}

// OnResize handles canvas resize events.
func OnResize(_ *surface.Context, c *surface.Canvas, _ surface.ResizeEvent) {
	draw(c)
}

// ---- Force-directed layout ----

// initPositions seeds positions for any node that doesn't have one yet.
// Uses a circular layout so the initial state is non-degenerate.
func initPositions(w, h int) {
	fw, fh := float64(w), float64(h)
	if fw == 0 {
		fw = 800
	}
	if fh == 0 {
		fh = 600
	}
	n := len(gNodes)
	r := math.Min(fw, fh) * 0.3
	for i, nd := range gNodes {
		if _, ok := gPos[nd.ID]; !ok {
			angle := float64(i) / math.Max(float64(n), 1) * math.Pi * 2
			gPos[nd.ID] = vec2{
				X: fw/2 + r*math.Cos(angle) + (rand.Float64()-0.5)*10,
				Y: fh/2 + r*math.Sin(angle) + (rand.Float64()-0.5)*10,
			}
		}
		if _, ok := gVel[nd.ID]; !ok {
			gVel[nd.ID] = vec2{}
		}
	}
}

// stepLayout runs one Euler integration step of the force-directed layout.
// Returns true if any node is still moving (so the loop keeps drawing).
func stepLayout(_ float64) bool {
	if len(gNodes) == 0 {
		return false
	}

	fx := make(map[string]float64, len(gNodes))
	fy := make(map[string]float64, len(gNodes))
	for _, n := range gNodes {
		fx[n.ID] = 0
		fy[n.ID] = 0
	}

	// Repulsion: every pair of nodes repels each other (Coulomb-ish).
	for i := 0; i < len(gNodes); i++ {
		for j := i + 1; j < len(gNodes); j++ {
			a, b := gNodes[i], gNodes[j]
			pa, paOK := gPos[a.ID]
			pb, pbOK := gPos[b.ID]
			if !paOK || !pbOK {
				continue
			}
			dx := pa.X - pb.X
			dy := pa.Y - pb.Y
			dist := math.Sqrt(dx*dx+dy*dy) + 1e-9
			force := fdRepel / (dist * dist)
			ux, uy := dx/dist, dy/dist
			fx[a.ID] += ux * force
			fy[a.ID] += uy * force
			fx[b.ID] -= ux * force
			fy[b.ID] -= uy * force
		}
	}

	// Attraction: edges act as springs.
	for _, e := range gEdges {
		pa, paOK := gPos[e.From]
		pb, pbOK := gPos[e.To]
		if !paOK || !pbOK {
			continue
		}
		dx := pb.X - pa.X
		dy := pb.Y - pa.Y
		dist := math.Sqrt(dx*dx+dy*dy) + 1e-9
		force := fdAttract * (dist - fdIdeal)
		ux, uy := dx/dist, dy/dist
		fx[e.From] += ux * force
		fy[e.From] += uy * force
		fx[e.To] -= ux * force
		fy[e.To] -= uy * force
	}

	// Euler integration with velocity damping.
	moving := false
	for _, n := range gNodes {
		// Don't integrate the node being dragged.
		if gDrag.Active && gDrag.NodeID == n.ID {
			continue
		}
		v := gVel[n.ID]
		v.X = (v.X + fx[n.ID]) * fdDamping
		v.Y = (v.Y + fy[n.ID]) * fdDamping
		gVel[n.ID] = v
		p := gPos[n.ID]
		p.X += v.X
		p.Y += v.Y
		gPos[n.ID] = p
		if math.Abs(v.X)+math.Abs(v.Y) > fdMinSpeed {
			moving = true
		}
	}
	return moving
}

// ---- Drawing ----

// typeColor returns the fill color for a node type, matching the JS palette.
func typeColor(t string) string {
	switch t {
	case "concept":
		return "#7b5c3a"
	case "decision":
		return "#5a7b3a"
	case "initiative":
		return "#3a5c7b"
	case "lesson":
		return "#7b3a5c"
	case "spec":
		return "#5c7b3a"
	case "plan":
		return "#3a7b5c"
	case "spore":
		return "#7b6b3a"
	case "skill":
		return "#3a6b7b"
	case "protocol":
		return "#6b3a7b"
	case "integration":
		return "#7b3a3a"
	case "readme":
		return "#9a8870"
	case "identity":
		return "#6b7b3a"
	default:
		return "#9a8870"
	}
}

const (
	colorEdge      = "rgba(120,100,75,0.25)"
	colorEdgeGraft = "rgba(160,90,40,0.55)"
	nodeRadius     = 7.0
	nodeRadiusSel  = 10.0
)

func isGraftKind(kind string) bool {
	return kind == "derived_from" || kind == "graft"
}

// draw clears and redraws the entire canvas.
func draw(c *surface.Canvas) {
	w := float64(c.Width())
	h := float64(c.Height())

	c.ClearRect(0, 0, w, h)
	c.Save()
	c.Translate(gTx.X, gTx.Y)
	c.Scale(gTx.Scale, gTx.Scale)

	// Draw edges first (below nodes).
	for _, e := range gEdges {
		pa, paOK := gPos[e.From]
		pb, pbOK := gPos[e.To]
		if !paOK || !pbOK {
			continue
		}
		c.BeginPath()
		c.MoveTo(pa.X, pa.Y)
		c.LineTo(pb.X, pb.Y)
		if isGraftKind(e.Kind) {
			c.SetStrokeStyle(colorEdgeGraft)
			c.SetLineWidth(1.5)
		} else {
			c.SetStrokeStyle(colorEdge)
			c.SetLineWidth(1.0)
		}
		c.Stroke()
	}

	// Draw nodes on top of edges.
	for _, n := range gNodes {
		p, ok := gPos[n.ID]
		if !ok {
			continue
		}
		isSel := gSelected == n.ID
		r := nodeRadius
		if isSel {
			r = nodeRadiusSel
		}
		color := typeColor(n.Type)

		c.BeginPath()
		c.Arc(p.X, p.Y, r, 0, math.Pi*2)
		if isSel {
			c.SetFillStyle(color)
		} else {
			c.SetFillStyle(color + "cc")
		}
		c.Fill()
		if isSel {
			c.SetStrokeStyle(color)
			c.SetLineWidth(2)
			c.Stroke()
		}

		// Label: always when selected, otherwise only when sufficiently zoomed in.
		if gTx.Scale > 0.8 || isSel {
			label := n.Label
			if len([]rune(label)) > 24 {
				label = string([]rune(label)[:22]) + "…"
			}
			if isSel {
				c.SetFillStyle("#2c2a26")
				c.SetFont("bold 11px sans-serif")
			} else {
				c.SetFillStyle("#5a5550")
				c.SetFont("10px sans-serif")
			}
			c.SetTextAlign("center")
			c.FillText(label, p.X, p.Y+r+12)
		}
	}

	c.Restore()
}

// ---- Coordinate helpers ----

// screenToWorld converts screen (canvas-element) coordinates to world space
// by applying the inverse of the current pan+zoom transform.
func screenToWorld(sx, sy float64) (wx, wy float64) {
	return (sx - gTx.X) / gTx.Scale, (sy - gTx.Y) / gTx.Scale
}

// nodeAt returns the id of the node nearest to world point (wx, wy) within
// the hit-test radius, or "" if no node is close enough.
func nodeAt(wx, wy float64) string {
	hitRadius := (nodeRadiusSel + 4) / gTx.Scale
	bestID := ""
	bestDist := hitRadius
	for _, n := range gNodes {
		p, ok := gPos[n.ID]
		if !ok {
			continue
		}
		dx := p.X - wx
		dy := p.Y - wy
		dist := math.Sqrt(dx*dx + dy*dy)
		if dist < bestDist {
			bestDist = dist
			bestID = n.ID
		}
	}
	return bestID
}
