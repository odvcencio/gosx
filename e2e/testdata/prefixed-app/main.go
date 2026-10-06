package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/assetpipe"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

func main() {
	_, file, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(file)
	app := server.New()
	if err := app.SetBasePath("/.proxy/game"); err != nil {
		log.Fatal(err)
	}
	app.SetRuntimeRoot(root)
	app.SetPublicDir(filepath.Join(root, "public"))
	app.EnableISR()
	pages := route.NewRouter()
	pages.SetLayout(func(ctx *route.RouteContext, body gosx.Node) gosx.Node {
		ctx.AddHead(gosx.El("link", gosx.Attrs(gosx.Attr("rel", "icon"), gosx.Attr("href", "/assets/wood.png"))))
		ctx.Runtime().SetTextureVariants(assetpipe.VariantManifest{Assets: []assetpipe.ManifestAsset{{Path: "assets/wood.png", Variants: []assetpipe.ManifestVariant{{Kind: "texture", URI: "/assets/wood.bc7.ktx2", RequiredCapabilities: []string{"device-feature:texture-compression-bc"}}}}}})
		cfg := (scene.Props{Width: 160, Height: 100, Background: "#102030", MaxFPS: 1}).EngineConfig()
		cfg.MountID = "scene"
		body = gosx.Fragment(body, ctx.Runtime().Engine(cfg, gosx.Text("scene")))
		return server.HTMLDocument(ctx.Document("Prefixed production fixture", body))
	})
	if err := pages.AddDir(filepath.Join(root, "app"), route.FileRoutesOptions{ExternalCSS: "/_gosx/file-css/"}); err != nil {
		log.Fatal(err)
	}
	handler, err := pages.BuildChecked()
	if err != nil {
		log.Fatal(err)
	}
	app.Mount("/", handler)
	app.Mount("/fragment", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<p id=region-refreshed>refreshed</p>"))
	}))
	app.Mount("/invalidate", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.RevalidatePath("/")
		w.WriteHeader(http.StatusNoContent)
	}))
	log.Fatal(app.ListenAndServe(":" + os.Getenv("PORT")))
}
