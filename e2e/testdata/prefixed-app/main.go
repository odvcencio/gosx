package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gorilla/websocket"
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
		cfg := (scene.Props{
			Width: 160, Height: 100, Background: "#102030", MaxFPS: 1,
			ForceWebGL: scene.Bool(true),
			Graph:      scene.Graph{Nodes: []scene.Node{scene.Model{ID: "city", Src: "/models/city.gltf"}}},
		}).EngineConfig()
		cfg.MountID = "scene"
		video := server.VideoProps{
			Src: "/media/movie.webm", Poster: "/assets/wood.png", Preload: "auto", Muted: true,
			Sync: "/video-sync", SubtitleTrack: "en",
			SubtitleTracks: []server.VideoTrack{{ID: "en", Src: "/media/en.vtt", Default: true}},
		}
		videoConfig := server.VideoEngineConfig(video)
		videoConfig.MountID = "video"
		body = gosx.Fragment(body, ctx.Runtime().Engine(cfg, gosx.Text("scene")), ctx.Runtime().Engine(videoConfig, server.Video(video)))
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
	app.Mount("/video-sync", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	app.Mount("/invalidate", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.RevalidatePath("/")
		w.WriteHeader(http.StatusNoContent)
	}))
	log.Fatal(app.ListenAndServe(":" + os.Getenv("PORT")))
}
