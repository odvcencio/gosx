// board.gsx — the .gsx authoring shape for the CanvasBoard sample.
//
// The .gsx compiler lowers this file into a Go function equivalent to
// HomePage in main.go. The CanvasBoard primitive itself is a built-in
// Go component (gosx.CanvasBoard) — authors write declarative JSX-like
// markup and the compiler emits the Node tree.
//
// This file is reference documentation for the .gsx authoring shape
// Phase 2 ships. Once the .gsx grammar discovers <CanvasBoard> as a
// first-class primitive (Phase 2 plan task D1.3 — currently a no-op
// because <CanvasBoard> is a Go component call, not a grammar tag),
// authors can write the JSX form below directly.

package canvasboard

import (
    "m31labs.dev/gosx"
)

// BoardPageProps is the typed prop shape for the .gsx page.
type BoardPageProps struct {
    PanX  float64
    PanY  float64
    Zoom  float64
    Nodes []gosx.CanvasBoardNode
}

// BoardPage is the .gsx page lowered to Go. The .gsx source for this page
// looked like:
//
//     <div class="container">
//       <h1>CanvasBoard primitive</h1>
//       <CanvasBoard
//         id="board"
//         width={1280}
//         height={720}
//         background="#0f1720"
//         pan={Pan(panSignal)}
//         zoom={zoomSignal}
//         nodes={boardNodes}
//         onPick={handleBoardPick}
//       />
//     </div>
//
// The lowering pass resolves the prop expressions through the shared
// expression VM and emits the equivalent gosx.El + gosx.CanvasBoard tree.
func BoardPage(props BoardPageProps) gosx.Node {
    return gosx.El("div", gosx.Attrs(gosx.Attr("class", "container")),
        gosx.El("h1", gosx.Text("CanvasBoard primitive")),
        gosx.CanvasBoard(gosx.CanvasBoardProps{
            ID:         "board",
            Width:      1280,
            Height:     720,
            Background: "#0f1720",
            Pan:        gosx.CanvasBoardPan{X: props.PanX, Y: props.PanY},
            Zoom:       props.Zoom,
            Nodes:      props.Nodes,
            OnPick:     "handleBoardPick",
        }),
    )
}
