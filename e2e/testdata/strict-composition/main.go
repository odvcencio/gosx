// Command strict-composition is the e2e fixture for production strict island
// composition. The root island components do not accept caller children or
// named slots; they compose same-file pure-view helpers inside each island
// instead, matching ir.LowerIsland's v1 boundary.
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
		return server.HTMLDocument(ctx.Document("strict composition fixture", body))
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
	log.Printf("strict-composition fixture listening on http://localhost:%s", port)
	log.Fatal(app.ListenAndServe(":" + port))
}
