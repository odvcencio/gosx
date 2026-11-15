package animgraph

import "errors"

// Feature is the runtime feature chunk that evaluates animation graph frames
// in the browser. A server-rendered page that mounts graph-driven models must
// require it, for example with server.PageRuntime.RequireFeature(Feature).
const Feature = "scene3d-animation-graph"

// ErrNotReady reports that the browser runtime cannot take a frame yet: the
// feature chunk or the Scene3D command bridge is still loading, or the target
// is not mounted. Frames carry complete state, so dropping one is safe; send
// the next frame as usual.
var ErrNotReady = errors.New("animgraph: Scene3D animation graph runtime is not ready")

// ErrUnsupported reports a Dispatch call outside a js/wasm build.
var ErrUnsupported = errors.New("animgraph: Dispatch needs a js/wasm build")
