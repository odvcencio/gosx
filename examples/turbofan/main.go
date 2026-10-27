// Turbofan is a procedural Scene3D cutaway with a server-rendered GSX interface.
// Build the runtime first (see README.md), then: go run ./examples/turbofan
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	_ "m31labs.dev/gosx/examples/turbofan/app"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func newApp() (*server.App, error) {
	_, source, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(source)
	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		doc := ctx.Document("Inside the turbofan · Cycle Lab", body)
		doc.Head = gosx.Fragment(doc.Head, gosx.El("link", gosx.Attrs(gosx.Attr("rel", "stylesheet"), gosx.Attr("href", "/turbofan.css"))))
		return server.HTMLDocument(doc)
	})
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		return nil, err
	}
	handler, err := router.BuildChecked()
	if err != nil {
		return nil, err
	}
	app := server.New()
	app.SetRuntimeRoot(root)
	app.SetPublicDir(filepath.Join(root, "public"))
	app.Mount("/", handler)
	return app, nil
}

func main() {
	app, err := newApp()
	if err != nil {
		log.Fatal(err)
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Turbofan: http://localhost:%s", port)
	log.Fatal(app.ListenAndServe("127.0.0.1:" + port))
}
