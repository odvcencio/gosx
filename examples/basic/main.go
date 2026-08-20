// Example basic demonstrates a simple GoSX server application.
//
// Pages are authored in .gsx source files (see app/page.gsx and
// app/counter/page.gsx) and served through the file-based router — the
// same app/page.gsx + page.server.go convention `gosx init` scaffolds for
// a new application. main.go only wires the router to an HTTP server.
//
// Run: go run ./examples/basic
// Visit: http://localhost:8080
package main

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	_ "m31labs.dev/gosx/examples/basic/app"
	_ "m31labs.dev/gosx/examples/basic/app/counter"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(thisFile)

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		return server.HTMLDocument(&server.DocumentContext{
			Request:   ctx.Request,
			Status:    ctx.StatusCode(),
			Title:     ctx.Title("GoSX Example"),
			Head:      ctx.Head(),
			Body:      body,
			BodyAttrs: ctx.BodyAttrsValue(),
			Nonce:     ctx.Nonce(),
		})
	})
	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}

	rootHandler, err := router.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}

	app := server.New()
	app.Mount("/", rootHandler)

	fmt.Println("GoSX example server running at http://localhost:8080")
	log.Fatal(app.ListenAndServe(":8080"))
}
