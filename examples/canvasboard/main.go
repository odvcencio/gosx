// Example canvasboard demonstrates Phase 2's <CanvasBoard> primitive.
//
// The page renders a 100-rect Miro/Figma-style 2D editing surface served
// from a gosx server. Pan/zoom signals, pick events, and drag handles are
// supplied by the CanvasBoardAdapter mounted on the client side once the
// WASM runtime hydrates the <canvas> placeholder this Go code emits.
//
// Run:
//
//	go run ./examples/canvasboard
//
// Visit http://localhost:8080 and inspect the <canvas> element — the
// data-gosx-surface-kind="canvas2d" attribute is the dispatch flag the
// WASM bootstrap reads when calling __gosx_hydrate("canvas2d", ...).
//
// Note: this example serves SSR HTML only. To exercise the full WASM
// hydration path, build client/wasm and serve the WASM artifact alongside
// the bootstrap from engine/surface.RuntimeHandler. The board.gsx file in
// this directory documents the .gsx authoring shape; the actual SSR is
// produced by HomePage() below using the same gosx.CanvasBoard primitive
// that a .gsx compile-pass would emit.
package main

import (
	"fmt"
	"log"
	"net/http"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

const (
	boardWidth  = 1280
	boardHeight = 720
	boardCols   = 20
	boardRows   = 5
)

func main() {
	app := server.New()

	app.SetLayout(func(title string, body gosx.Node) gosx.Node {
		return server.HTMLDocument(
			"GoSX CanvasBoard Example — "+title,
			gosx.Node{},
			body,
		)
	})

	app.Route("/", func(r *http.Request) gosx.Node {
		return HomePage()
	})

	addr := ":8080"
	fmt.Println("CanvasBoard example server running at http://localhost" + addr)
	fmt.Println("Inspect the <canvas data-gosx-surface-kind=\"canvas2d\"> placeholder.")
	log.Fatal(app.ListenAndServe(addr))
}

// HomePage renders the CanvasBoard sample page. The board has 100 rectangles
// arranged in a grid, pan/zoom-ready, with onPick wired to a handler the
// WASM bootstrap will resolve.
func HomePage() gosx.Node {
	nodes := generateBoardNodes()
	return gosx.El("div",
		gosx.Attrs(gosx.Attr("class", "container")),
		gosx.El("h1", gosx.Text("CanvasBoard primitive")),
		gosx.El("p", gosx.Text(fmt.Sprintf(
			"Phase 2 sample — %d rects, drag canvas to pan, wheel to zoom, click to select.",
			len(nodes),
		))),
		gosx.CanvasBoard(gosx.CanvasBoardProps{
			ID:         "board",
			Width:      boardWidth,
			Height:     boardHeight,
			Background: "#0f1720",
			Pan:        gosx.CanvasBoardPan{X: 0, Y: 0},
			Zoom:       1.0,
			Nodes:      nodes,
			OnPick:     "handleBoardPick",
			ClassName:  "canvasboard-stage",
		}),
		gosx.El("p",
			gosx.Attrs(gosx.Attr("class", "hint")),
			gosx.Text("Pick events arrive via $surface.event.selectedID (ADR 0007)."),
		),
	)
}

// generateBoardNodes produces a 20×5 grid of rects with rotating colors.
// Each rect carries an explicit id so the pick handler can correlate
// selections back to source nodes.
func generateBoardNodes() []gosx.CanvasBoardNode {
	palette := []string{"#ff8866", "#88ddff", "#ffd866", "#a0ff88", "#ff88dd"}
	nodes := make([]gosx.CanvasBoardNode, 0, boardCols*boardRows)
	for row := 0; row < boardRows; row++ {
		for col := 0; col < boardCols; col++ {
			x := float64(col-boardCols/2) * 60
			y := float64(row-boardRows/2) * 50
			nodes = append(nodes, gosx.CanvasBoardNode{
				ID:     fmt.Sprintf("rect-%d-%d", row, col),
				Kind:   "rect",
				X:      x,
				Y:      y,
				Width:  44,
				Height: 32,
				Color:  palette[(row+col)%len(palette)],
			})
		}
	}
	return nodes
}
