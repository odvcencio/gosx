package surface

import "m31labs.dev/gosx/markup"

// HeadAssets returns a markup.Fragment containing the <script> tags hosts must
// inject into <head> to load the engine-surface JS runtime. Both scripts are
// emitted with the defer attribute so they execute after parsing, in document
// order. wasm_exec.js precedes runtime.js so the Go constructor is defined
// before the bootstrap probes for it.
//
// Mount the corresponding handler via RuntimeHandler() at /gosx/surface/.
//
// Typical host integration:
//
//	app.Mount("/gosx/engines/", surface.Handler())
//	app.Mount("/gosx/surface/", surface.RuntimeHandler())
//	// in your page <head>:
//	markup.Fragment(surface.HeadAssets(), /* other head nodes */)
func HeadAssets() markup.Node {
	return markup.Fragment(
		markup.El("script", markup.Attrs(
			markup.Attr("src", "/gosx/surface/wasm_exec.js"),
			markup.Attr("defer", "defer"),
		)),
		markup.El("script", markup.Attrs(
			markup.Attr("src", "/gosx/surface/runtime.js"),
			markup.Attr("defer", "defer"),
		)),
	)
}
