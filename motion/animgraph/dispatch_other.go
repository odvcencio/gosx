//go:build !(js && wasm)

package animgraph

// Dispatch sends the instances' current poses to a mounted Scene3D engine. It
// needs a js/wasm build; elsewhere it returns ErrUnsupported. Use
// FrameEncoder and Program.RigJSON to carry frames over another transport.
func Dispatch(target string, models ...*Instance) error { return ErrUnsupported }
