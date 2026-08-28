// Command strict-island is the e2e fixture for a strict //gosx:island.
//
// app/page.gsx declares CounterProps, a strict `component Counter(props:
// CounterProps)` island, and a strict `component Page()` that calls it with
// named attributes. The e2e test builds this fixture with `gosx build
// --prod`, serves the production bundle, and drives a real browser to prove
// the island renders the proven props server-side and hydrates them
// client-side, responding to a real click.
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.SetLanguage("en")
		return server.HTMLDocument(ctx.Document("strict-island fixture", body))
	})
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}

	app := server.New()
	rootHandler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	app.Mount("/", rootHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("strict-island fixture listening on http://localhost:%s", port)
	log.Fatal(app.ListenAndServe(":" + port))
}
