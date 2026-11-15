package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	// The page.server.go files register each route's Load and Metadata in
	// init, so the packages must be linked in.
	_ "m31labs.dev/gosx/examples/goetrope-watch/app"
	_ "m31labs.dev/gosx/examples/goetrope-watch/app/watch"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)

	app, err := newApp(root)
	if err != nil {
		log.Fatal(err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("goetrope-watch listening on http://localhost:%s", port)
	log.Fatal(app.ListenAndServe(":" + port))
}

// newApp wires the file-routed pages under root/app to a server. The test uses
// it to render each route without opening a port.
func newApp(root string) (*server.App, error) {
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
