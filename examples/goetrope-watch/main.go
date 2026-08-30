package main

import (
	"log"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	_ "m31labs.dev/gosx/examples/goetrope-watch/modules"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	app, err := newGoetropeWatchApp(root)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("goetrope-watch prototype at http://localhost:8080")
	log.Fatal(app.ListenAndServe(":8080"))
}

func newGoetropeWatchApp(root string) (*server.App, error) {
	layout, err := route.FileLayout(filepath.Join(root, "app", "layout.gsx"))
	if err != nil {
		return nil, err
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.AddHead(server.Stylesheet("watch.css"))
		return layout(ctx, body)
	})

	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		return nil, err
	}

	app := server.New()
	router.SetRevalidator(app.Revalidator())
	app.EnableNavigation()
	app.SetPublicDir(filepath.Join(root, "public"))
	rootHandler, err := router.BuildChecked()
	if err != nil {
		return nil, err
	}
	app.Mount("/", rootHandler)
	return app, nil
}
