package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"os"
	"path"
	"runtime"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/action"
	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/server"
)

const defaultPort = "8080"

const fixtureMP4Base64 = "AAAAIGZ0eXBpc29tAAACAGlzb21pc28yYXZjMW1wNDEAAAMObW9vdgAAAGxtdmhkAAAAAAAAAAAAAAAAAAAD6AAAA+gAAQAAAQAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAgAAAjl0cmFrAAAAXHRraGQAAAADAAAAAAAAAAAAAAABAAAAAAAAA+gAAAAAAAAAAAAAAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAABAAAAAAAAAAAAAAAAAABAAAAAABAAAAAQAAAAAAAkZWR0cwAAABxlbHN0AAAAAAAAAAEAAAPoAAAAAAABAAAAAAGxbWRpYQAAACBtZGhkAAAAAAAAAAAAAAAAAABAAAAAQABVxAAAAAAALWhkbHIAAAAAAAAAAHZpZGUAAAAAAAAAAAAAAABWaWRlb0hhbmRsZXIAAAABXG1pbmYAAAAUdm1oZAAAAAEAAAAAAAAAAAAAACRkaW5mAAAAHGRyZWYAAAAAAAAAAQAAAAx1cmwgAAAAAQAAARxzdGJsAAAAuHN0c2QAAAAAAAAAAQAAAKhhdmMxAAAAAAAAAAEAAAAAAAAAAAAAAAAAAAAAABAAEABIAAAASAAAAAAAAAABFExhdmM2MS4zLjEwMCBsaWJ4MjY0AAAAAAAAAAAAAAAAGP//AAAALmF2Y0MBQsAe/+EAFmdCwB7ZHsBEAAADAAQAAAMACDxYuSABAAVoy4PLIAAAABBwYXNwAAAAAQAAAAEAAAAUYnRydAAAAAAAABQwAAAUMAAAABhzdHRzAAAAAAAAAAEAAAABAABAAAAAABxzdHNjAAAAAAAAAAEAAAABAAAAAQAAAAEAAAAUc3RzegAAAAAAAAKGAAAAAQAAABRzdGNvAAAAAAAAAAEAAAM+AAAAYXVkdGEAAABZbWV0YQAAAAAAAAAhaGRscgAAAAAAAAAAbWRpcmFwcGwAAAAAAAAAAAAAAAAsaWxzdAAAACSpdG9vAAAAHGRhdGEAAAABAAAAAExhdmY2MS4xLjEwMAAAAAhmcmVlAAACjm1kYXQAAAJwBgX//2zcRem95tlIt5Ys2CDZI+7veDI2NCAtIGNvcmUgMTY0IHIzMTkxIDQ2MTNhYzMgLSBILjI2NC9NUEVHLTQgQVZDIGNvZGVjIC0gQ29weWxlZnQgMjAwMy0yMDI0IC0gaHR0cDovL3d3dy52aWRlb2xhbi5vcmcveDI2NC5odG1sIC0gb3B0aW9uczogY2FiYWM9MCByZWY9MyBkZWJsb2NrPTE6MDowIGFuYWx5c2U9MHgxOjB4MTExIG1lPWhleCBzdWJtZT03IHBzeT0xIHBzeV9yZD0xLjAwOjAuMDAgbWl4ZWRfcmVmPTEgbWVfcmFuZ2U9MTYgY2hyb21hX21lPTEgdHJlbGxpcz0xIDh4OGRjdD0wIGNxbT0wIGRlYWR6b25lPTIxLDExIGZhc3RfcHNraXA9MSBjaHJvbWFfcXBfb2Zmc2V0PS0yIHRocmVhZHM9MSBsb29rYWhlYWRfdGhyZWFkcz0xIHNsaWNlZF90aHJlYWRzPTAgbnI9MCBkZWNpbWF0ZT0xIGludGVybGFjZWQ9MCBibHVyYXlfY29tcGF0PTAgY29uc3RyYWluZWRfaW50cmE9MCBiZnJhbWVzPTAgd2VpZ2h0cD0wIGtleWludD0yNTAga2V5aW50X21pbj0xIHNjZW5lY3V0PTQwIGludHJhX3JlZnJlc2g9MCByY19sb29rYWhlYWQ9NDAgcmM9Y3JmIG1idHJlZT0xIGNyZj0yMy4wIHFjb21wPTAuNjAgcXBtaW49MCBxcG1heD02OSBxcHN0ZXA9NCBpcF9yYXRpbz0xLjQwIGFxPTE6MS4wMACAAAAADmWIhAV///8PRQABQt+A"

type appPrograms struct {
	counter []byte
	tabs    []byte
	toggle  []byte
	derived []byte
	shared  []byte
}

func main() {
	port := getenv("PORT", defaultPort)
	app, err := newApp()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("GoSX Ouroboros corpus fixture running at http://localhost:%s\n", port)
	log.Fatal(app.ListenAndServe(":" + port))
}

func newApp() (*server.App, error) {
	programs, err := encodePrograms()
	if err != nil {
		return nil, err
	}

	app := server.New()
	app.SetRuntimeRoot(runtimeRoot())
	app.UseReadyCheck("ouroboros-corpus", server.ReadyCheckFunc(func(context.Context) error { return nil }))
	app.Mount("/_ouroboros/islands/", islandProgramHandler(programs))
	app.Mount("/_ouroboros/hub/echo", newEchoHub())
	app.Mount("/_ouroboros/video-sync", newVideoSyncHub())
	app.Mount("/media/ouroboros-placeholder.mp4", http.HandlerFunc(serveFixtureVideo))
	app.Mount("/action/form/__actions/validate-name", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action.ServeHandler(w, r, validateNameAction)
	}))

	app.Page("GET /static", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R00 static"}})
		return pageShell("R00", "/static", "static", "no-runtime", staticBody())
	})
	app.Page("GET /lite", func(ctx *server.Context) gosx.Node {
		ctx.Runtime().EnableBootstrap()
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R01 lite"}})
		return pageShell("R01", "/lite", "lite", "lite", liteBody())
	})
	app.Page("GET /island/counter", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R02 counter island"}})
		body := gosx.El("section",
			routeAttrs("R02", "/island/counter", "island-counter", "islands"),
			gosx.El("h1", gosx.Text("Counter island")),
			ctx.Runtime().IslandWithProgramAsset(program.CounterProgram(), map[string]int{"initial": 2}, "/_ouroboros/islands/Counter.json", "json", ""),
			gosx.El("form", gosx.Attrs(gosx.Attr("method", "post"), gosx.Attr("action", "/action/form/__actions/validate-name")),
				gosx.El("input", gosx.Attrs(gosx.Attr("type", "hidden"), gosx.Attr("name", "name"), gosx.Attr("value", "counter"))),
				gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Submit action")),
			),
		)
		return pageShellNode(body)
	})
	app.Page("GET /islands/kitchen", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R03 island kitchen"}})
		body := gosx.El("section",
			routeAttrs("R03", "/islands/kitchen", "island-kitchen", "islands"),
			gosx.El("h1", gosx.Text("Island kitchen")),
			ctx.Runtime().IslandWithProgramAsset(program.CounterProgram(), map[string]int{"initial": 1}, "/_ouroboros/islands/Counter.json", "json", ""),
			ctx.Runtime().IslandWithProgramAsset(program.TabsProgram(), map[string]int{"initial": 0}, "/_ouroboros/islands/Tabs.json", "json", ""),
			ctx.Runtime().IslandWithProgramAsset(program.ToggleProgram(), map[string]bool{"initial": true}, "/_ouroboros/islands/Toggle.json", "json", ""),
			ctx.Runtime().IslandWithProgramAsset(sharedSelectionProgram(), map[string]string{"label": "left"}, "/_ouroboros/islands/SharedSelection.json", "json", ""),
			ctx.Runtime().IslandWithProgramAsset(sharedSelectionProgram(), map[string]string{"label": "right"}, "/_ouroboros/islands/SharedSelection.json", "json", ""),
		)
		return pageShellNode(body)
	})
	app.Page("GET /action/form", func(ctx *server.Context) gosx.Node {
		ctx.Runtime().EnableBootstrap()
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R04 action form"}})
		return pageShell("R04", "/action/form", "action-form", "action-bridge", actionFormBody("", "", "idle"))
	})
	app.Page("GET /canvas-board", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R05 CanvasBoard"}})
		return pageShell("R05", "/canvas-board", "canvas-board", "engine", canvasBoardBody(ctx))
	})
	app.Page("GET /hub/echo", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R06 hub echo"}})
		ctx.Runtime().BindHub("ouroboros-echo", "/_ouroboros/hub/echo", []hydrate.HubBinding{
			{Event: "echo", Signal: "$ouroboros.echo"},
			{Event: "presence", Signal: "$ouroboros.presence"},
		})
		return pageShell("R06", "/hub/echo", "hub-echo", "collab", hubBody())
	})
	app.Page("GET /video-sync", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R07 video sync"}})
		body := gosx.El("section",
			routeAttrs("R07", "/video-sync", "video-sync", "video"),
			gosx.El("h1", gosx.Text("Video sync")),
			ctx.Video(server.VideoProps{
				Src:         "/media/ouroboros-placeholder.mp4",
				Sync:        "/_ouroboros/video-sync",
				SyncMode:    "follow",
				Muted:       true,
				PlaysInline: true,
				Width:       320,
				Height:      180,
			}, gosx.El("p", gosx.Text("video fallback"))),
		)
		return pageShellNode(body)
	})
	app.Page("GET /scene/basic", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R08 Scene3D"}})
		return pageShell("R08", "/scene/basic", "scene-basic", "scene3d", sceneBody(ctx))
	})
	nav := server.New()
	nav.SetRuntimeRoot(runtimeRoot())
	nav.EnableNavigation()
	nav.Page("GET /a", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R09 navigation A"}})
		return pageShell("R09A", "/navigation/a", "navigation-a", "navigation", navigationBody(ctx, "a", "/navigation/b"))
	})
	nav.Page("GET /b", func(ctx *server.Context) gosx.Node {
		ctx.SetMetadata(server.Metadata{Title: server.Title{Absolute: "R09 navigation B"}})
		return pageShell("R09B", "/navigation/b", "navigation-b", "navigation", navigationBody(ctx, "b", "/navigation/a"))
	})
	app.MountApp("/navigation", nav)
	return app, nil
}

func validateNameAction(ctx *action.Context) error {
	name := strings.TrimSpace(ctx.Form.Value("name"))
	if name == "" {
		ctx.SetResult(action.Result{
			OK:          false,
			Message:     "name required",
			FieldErrors: map[string]string{"name": "name required"},
			Data:        actionData(map[string]string{"html": actionStateHTML("error", "name required")}),
		})
		ctx.SetStatus(http.StatusUnprocessableEntity)
		return nil
	}
	if !strings.Contains(ctx.Request.Header.Get("Accept"), "application/json") {
		ctx.Redirect("/action/form?ok=1")
		return nil
	}
	return ctx.Success("name accepted", map[string]string{
		"value": name,
		"html":  actionStateHTML("ok", "accepted "+name),
	})
}

func encodePrograms() (appPrograms, error) {
	encode := func(p *program.Program) ([]byte, error) {
		return program.EncodeJSON(p)
	}
	counter, err := encode(program.CounterProgram())
	if err != nil {
		return appPrograms{}, err
	}
	tabs, err := encode(program.TabsProgram())
	if err != nil {
		return appPrograms{}, err
	}
	toggle, err := encode(program.ToggleProgram())
	if err != nil {
		return appPrograms{}, err
	}
	derived, err := encode(program.DerivedProgram())
	if err != nil {
		return appPrograms{}, err
	}
	shared, err := encode(sharedSelectionProgram())
	if err != nil {
		return appPrograms{}, err
	}
	return appPrograms{counter: counter, tabs: tabs, toggle: toggle, derived: derived, shared: shared}, nil
}

func sharedSelectionProgram() *program.Program {
	exprs := []program.Expr{
		{Op: program.OpSignalGet, Value: "$ouroboros.selection", Type: program.TypeString},
		{Op: program.OpLitString, Value: "alpha", Type: program.TypeString},
		{Op: program.OpLitString, Value: "beta", Type: program.TypeString},
		{Op: program.OpSignalSet, Operands: []program.ExprID{2}, Value: "$ouroboros.selection", Type: program.TypeString},
	}
	nodes := []program.Node{
		{
			Kind: program.NodeElement,
			Tag:  "div",
			Attrs: []program.Attr{
				{Kind: program.AttrStatic, Name: "class", Value: "shared-selection"},
				{Kind: program.AttrStatic, Name: "data-shared-signal", Value: "$ouroboros.selection"},
			},
			Children: []program.NodeID{1, 2},
		},
		{Kind: program.NodeExpr, Expr: 0},
		{
			Kind: program.NodeElement,
			Tag:  "button",
			Attrs: []program.Attr{
				{Kind: program.AttrEvent, Name: "click", Event: "selectBeta"},
			},
			Children: []program.NodeID{3},
		},
		{Kind: program.NodeText, Text: "Select beta"},
	}
	return &program.Program{
		Name:  "SharedSelection",
		Root:  0,
		Nodes: nodes,
		Exprs: exprs,
		Signals: []program.SignalDef{
			{Name: "$ouroboros.selection", Type: program.TypeString, Init: 1},
		},
		Handlers: []program.Handler{
			{Name: "selectBeta", Body: []program.ExprID{3}},
		},
		StaticMask: []bool{false, false, true, true},
	}
}

func islandProgramHandler(programs appPrograms) http.Handler {
	assets := map[string][]byte{
		"/Counter.json":         programs.counter,
		"/Tabs.json":            programs.tabs,
		"/Toggle.json":          programs.toggle,
		"/Derived.json":         programs.derived,
		"/SharedSelection.json": programs.shared,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/_ouroboros/islands/"))
		data, ok := assets[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		_, _ = w.Write(data)
	})
}

func newEchoHub() http.Handler {
	h := hub.New("ouroboros-echo")
	h.SetState("status", "ready")
	h.On("echo", func(ctx *hub.Context) {
		ctx.Hub.Broadcast("echo", map[string]string{"status": "echo"})
	})
	return h
}

func newVideoSyncHub() http.Handler {
	h := hub.New("ouroboros-video-sync")
	h.Latch("sync")
	h.Broadcast("sync", map[string]any{
		"type":        "sync",
		"mediaID":     "/media/ouroboros-placeholder.mp4",
		"position":    0,
		"playing":     false,
		"rate":        1,
		"sentAtMS":    1700000000000,
		"viewerCount": 1,
	})
	return h
}

func serveFixtureVideo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	w.Header().Set("Accept-Ranges", "bytes")
	_, _ = w.Write(fixtureMP4())
}

func fixtureMP4() []byte {
	data, err := base64.StdEncoding.DecodeString(fixtureMP4Base64)
	if err != nil {
		panic("invalid embedded fixture MP4: " + err.Error())
	}
	return data
}

func pageShell(id, route, marker, capability string, body gosx.Node) gosx.Node {
	return pageShellNode(gosx.El("section", routeAttrs(id, route, marker, capability),
		gosx.El("h1", gosx.Text(marker)),
		body,
	))
}

func pageShellNode(body gosx.Node) gosx.Node {
	return gosx.El("main", gosx.Attrs(gosx.Attr("class", "ouroboros-shell")), body)
}

func routeAttrs(id, route, marker, capability string) gosx.AttrList {
	return gosx.Attrs(
		gosx.Attr("data-route-id", id),
		gosx.Attr("data-route-path", route),
		gosx.Attr("data-marker", marker),
		gosx.Attr("data-expected-capability", capability),
	)
}

func staticBody() gosx.Node {
	return gosx.El("p", gosx.Attrs(gosx.Attr("data-static-marker", "ssr-only")), gosx.Text("server rendered only"))
}

func liteBody() gosx.Node {
	return gosx.El("form", gosx.Attrs(gosx.Attr("method", "get"), gosx.Attr("action", "/lite"), gosx.Attr("data-lite-action", "query")),
		gosx.El("input", gosx.Attrs(gosx.Attr("name", "q"), gosx.Attr("value", "baseline"))),
		gosx.El("button", gosx.Attrs(gosx.Attr("type", "submit")), gosx.Text("Apply")),
	)
}

func actionData(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}

func actionStateHTML(status, message string) string {
	return `<p data-action-state="` + html.EscapeString(status) + `">` + html.EscapeString(message) + `</p>`
}

func actionFormBody(value, errText, state string) gosx.Node {
	children := []any{
		gosx.El("label", gosx.Text("Name"), gosx.El("input", gosx.Attrs(gosx.Attr("name", "name"), gosx.Attr("value", value)))),
		gosx.El("button", gosx.Attrs(
			gosx.Attr("type", "submit"),
			gosx.Attr("data-gosx-action-target", "#action-state"),
			gosx.Attr("data-gosx-action-signal", "$ouroboros.action.name"),
		), gosx.Text("Submit")),
		gosx.El("output", gosx.Attrs(gosx.Attr("id", "action-state"), gosx.Attr("data-action-state", state)), gosx.Text(state)),
	}
	if errText != "" {
		children = append(children, gosx.El("p", gosx.Attrs(gosx.Attr("data-field-error", "name")), gosx.Text(errText)))
	}
	return gosx.El("form", append([]any{gosx.Attrs(
		gosx.Attr("method", "post"),
		gosx.Attr("action", "/action/form/__actions/validate-name"),
		gosx.Attr("data-action-name", "validate-name"),
		gosx.Attr("data-gosx-action", "POST /action/form/__actions/validate-name"),
		gosx.Attr("data-gosx-action-target", "#action-state"),
		gosx.Attr("data-gosx-action-signal", "$ouroboros.action.name"),
	)}, children...)...)
}

func canvasBoardBody(ctx *server.Context) gosx.Node {
	nodes := []gosx.CanvasBoardNode{
		{ID: "alpha", Kind: "rect", X: -60, Y: -20, Width: 48, Height: 32, Color: "#2563eb"},
		{ID: "beta", Kind: "rect", X: 0, Y: 20, Width: 48, Height: 32, Color: "#16a34a"},
		{ID: "gamma", Kind: "rect", X: 60, Y: -20, Width: 48, Height: 32, Color: "#dc2626"},
	}
	props := gosx.CanvasBoardProps{
		ID:         "ouroboros-board",
		Width:      640,
		Height:     360,
		Background: "#111827",
		Pan:        gosx.CanvasBoardPan{X: 0, Y: 0},
		Zoom:       1,
		Nodes:      nodes,
		OnPick:     "selectFixtureNode",
		ClassName:  "ouroboros-board",
	}
	return gosx.CanvasBoard(props)
}

func hubBody() gosx.Node {
	return gosx.El("div",
		gosx.Attrs(gosx.Attr("data-hub", "ouroboros-echo")),
		gosx.El("p", gosx.Attrs(gosx.Attr("data-signal", "$ouroboros.echo")), gosx.Text("echo signal")),
	)
}

func sceneBody(ctx *server.Context) gosx.Node {
	raw, _ := json.Marshal(basicSceneProps().GoSXSpreadProps())
	return ctx.Engine(engine.Config{
		Name:         scene.DefaultEngineName,
		Kind:         engine.KindSurface,
		MountID:      "ouroboros-scene-basic",
		Capabilities: []engine.Capability{engine.CapCanvas, engine.CapWebGPU, engine.CapWebGL, engine.CapAnimation},
		Props:        raw,
		MountAttrs: map[string]any{
			"data-gosx-scene3d": true,
			"style":             "width:640px;height:360px;max-width:100%;",
		},
	}, gosx.El("p", gosx.Attrs(gosx.Attr("data-scene-fallback", "basic")), gosx.Text("scene fallback")))
}

func basicSceneProps() scene.Props {
	return scene.Props{
		Responsive: scene.Bool(true),
		Width:      640,
		Height:     360,
		Label:      "Ouroboros basic Scene3D",
		Background: "#0f172a",
		Controls:   scene.ControlOrbit,
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 1.4, 4.5),
			FOV:      55,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.55,
			SkyColor:         "#dbeafe",
			SkyIntensity:     0.35,
			GroundColor:      "#1f2937",
			GroundIntensity:  0.4,
		},
		Graph: scene.NewGraph(
			scene.Mesh{
				ID:       "center-box",
				Geometry: scene.BoxGeometry{Width: 1.2, Height: 1.2, Depth: 1.2},
				Material: scene.StandardMaterial{Color: "#38bdf8", Roughness: 0.45, Metalness: 0.1},
				Position: scene.Vec3(0, 0.35, 0),
				Rotation: scene.Euler{Y: 0.6},
			},
			scene.Mesh{
				ID:       "floor",
				Geometry: scene.PlaneGeometry{Width: 4, Height: 4},
				Material: scene.StandardMaterial{Color: "#334155", Roughness: 0.85},
				Position: scene.Vec3(0, -0.35, 0),
				Rotation: scene.Euler{X: -1.5708},
			},
		),
	}
}

func navigationBody(ctx *server.Context, current, target string) gosx.Node {
	return gosx.El("nav",
		gosx.Attrs(gosx.Attr("data-navigation-current", current)),
		server.Link(target, gosx.Text("Go")),
		gosx.El("div", gosx.Attrs(gosx.Attr("data-navigation-island", current)),
			ctx.Runtime().IslandWithProgramAsset(program.CounterProgram(), map[string]int{"initial": len(current)}, "/_ouroboros/islands/Counter.json", "json", ""),
		),
	)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func corpusRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return strings.TrimSuffix(file, "/main.go")
}

func repoRoot() string {
	return path.Clean(path.Join(corpusRoot(), "..", ".."))
}

func runtimeRoot() string {
	root := repoRoot()
	if _, err := os.Stat(path.Join(root, "build", "gosx-runtime.wasm")); err == nil {
		return root
	}
	return path.Join(root, "dist")
}
