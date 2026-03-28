package main

import (
	"log"
	"path/filepath"
	"runtime"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)

	layout, err := route.FileLayout(filepath.Join(root, "app", "layout.gsx"))
	if err != nil {
		log.Fatal(err)
	}

	router := route.NewRouter()
	router.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.AddHead(server.Stylesheet("watch.css"))
		return layout(ctx, body)
	})

	if err := router.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{}); err != nil {
		log.Fatal(err)
	}

	app := server.New()
	router.SetRevalidator(app.Revalidator())
	app.EnableNavigation()
	app.SetPublicDir(filepath.Join(root, "public"))
	app.Mount("/", router.Build())

	log.Printf("goetrope-watch prototype at http://localhost:8080")
	log.Fatal(app.ListenAndServe(":8080"))
}
