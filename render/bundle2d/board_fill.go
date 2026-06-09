package bundle2d

import _ "embed"

// BoardFillSelenaSource is the Selena (.sel) source for the canvas board's rect
// fill material. It is embedded as a string (no compiler dependency here); the
// WebGPU board path compiles it via scene.CompileSelenaMaterial and attaches the
// emitted WGSL to the rect RenderMaterial's CustomVertexWGSL/CustomFragmentWGSL
// so the 16a JS WebGPU renderer draws the fill unlit at full color (fixing the
// lit-pipeline ambient dimming). The per-rect color rides the baseColor uniform.
//
//go:embed board_fill.sel
var BoardFillSelenaSource string
