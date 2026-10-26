// Command islands-files is the e2e fixture for islands that live outside
// page.gsx, SVG that an island draws after load, and rows patched inside a
// table.
//
//   - app/counter.gsx is a sibling file of the page.
//   - app/ui/tally.gsx and app/ui/roster.gsx sit in a directory the page
//     imports by relative path (import ui "./ui").
//   - app/chart.gsx draws an <svg> from a patch after hydration.
//   - ui/roster.gsx patches <tr> rows inside <table>, with and without a
//     <thead> and <tfoot>.
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
		return server.HTMLDocument(ctx.Document("islands-files fixture", body))
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
	log.Printf("islands-files fixture listening on http://localhost:%s", port)
	log.Fatal(app.ListenAndServe(":" + port))
}
