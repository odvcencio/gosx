package main

import (
	"log"
	"os"
	"path/filepath"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/server"
)

func main() {
	root := server.ResolveAppRoot("")
	app := server.New()
	app.SetRuntimeRoot(root)
	app.SetPublicDir(filepath.Join(root, "public"))
	app.EnableNavigation()
	renderMixed := func(ctx *server.Context) gosx.Node {
		runtime := ctx.Runtime()
		counter := runtime.Island(program.CounterProgram(), map[string]int{"initial": 0})
		goEngine := ctx.Engine(engine.Config{
			Name:         "GoWASMFixture",
			Kind:         engine.KindSurface,
			Runtime:      engine.RuntimeGoWASM,
			WASMPath:     "__ENGINE_WASM_PATH__",
			Capabilities: []engine.Capability{engine.CapWASM},
		}, gosx.El("span", gosx.Attrs(gosx.Attr("data-server-fallback", "true")), gosx.Text("server fallback")))
		return gosx.El("main", counter, goEngine)
	}
	app.Page("/", func(ctx *server.Context) gosx.Node {
		return renderMixed(ctx)
	})
	app.Page("/blank", func(ctx *server.Context) gosx.Node {
		return gosx.El("main",
			gosx.Attrs(gosx.Attr("data-gosx-main", "")),
			server.Link("/managed", gosx.Attrs(gosx.Attr("id", "managed-runtime-link")), gosx.Text("Load mixed runtime")),
		)
	})
	app.Page("/managed", func(ctx *server.Context) gosx.Node {
		return renderMixed(ctx)
	})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(app.ListenAndServe(":" + port))
}
