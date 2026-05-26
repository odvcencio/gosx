package surface

import gosx "m31labs.dev/gosx"

// HeadAssets returns a gosx.Fragment containing the <script> tags hosts must
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
//	gosx.Fragment(surface.HeadAssets(), /* other head nodes */)
func HeadAssets() gosx.Node {
	return gosx.Fragment(
		gosx.El("script", gosx.Attrs(
			gosx.Attr("src", "/gosx/surface/wasm_exec.js"),
			gosx.Attr("defer", "defer"),
		)),
		gosx.El("script", gosx.Attrs(
			gosx.Attr("src", "/gosx/surface/runtime.js"),
			gosx.Attr("defer", "defer"),
		)),
	)
}
