package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island"
	"m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/server"
)

const source = `package app
import "m31labs.dev/gosx/signal"
type Props struct { Flags map[string]bool; Labels map[string]string }
//gosx:island
func MapIndex(props Props) Node {
  shown := signal.New(false)
  show := func() { shown.Set(true) }
  return <div><button id="show" onClick={show}>show</button><If cond={shown.Get()}><span id="bool">{props.Flags["missing"]}</span><span id="string">{props.Labels["missing"]}</span></If></div>
}`

func main() {
	compiled, err := gosx.Compile([]byte(source))
	if err != nil {
		log.Fatal(err)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	compiled.Dir = filepath.Join(filepath.Dir(thisFile), "island")
	compiled.PackagePath = "example.invalid/map-index/island"
	lowered, err := ir.LowerIsland(compiled, 0)
	if err != nil {
		log.Fatal(err)
	}
	encoded, err := program.EncodeJSON(lowered)
	if err != nil {
		log.Fatal(err)
	}
	app := server.New()
	app.Mount("/map-program.json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(encoded)
	}))
	app.Mount("/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		renderer := island.NewRenderer("map-index")
		renderer.SetProgramAsset("MapIndex", "/map-program.json", "json", "")
		body := renderer.RenderIslandFromProgram(lowered, map[string]any{"Flags": map[string]string{"k": "present"}, "Labels": map[string]string(nil)})
		page := server.HTMLDocument(&server.DocumentContext{Title: "Map index", Head: renderer.PageHead(), Body: body})
		_, _ = w.Write([]byte(gosx.RenderHTML(page)))
	}))
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Fatal(app.ListenAndServe(":" + port))
}
