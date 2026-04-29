// Example counter runs the standalone Counter island demo.
//
// Run: go run ./examples/counter
// Visit: http://localhost:8080
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/island"
	"github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/server"
)

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Dir(thisFile)

	counterProgram := program.CounterProgram()
	counterJSON, err := program.EncodeJSON(counterProgram)
	if err != nil {
		log.Fatalf("encode counter island: %v", err)
	}

	app := server.New()
	app.SetPublicDir(root)
	app.Mount("/gosx/islands/Counter.json", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		_, _ = w.Write(counterJSON)
	}))
	app.Route("/", func(r *http.Request) gosx.Node {
		return counterPage(counterProgram, queryCount(r))
	})

	port := getenv("PORT", "8080")
	fmt.Printf("GoSX counter example running at http://localhost:%s\n", port)
	log.Fatal(app.ListenAndServe(":" + port))
}

func queryCount(r *http.Request) int {
	count, _ := strconv.Atoi(r.URL.Query().Get("count"))
	return count
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func counterPage(counterProgram *program.Program, count int) gosx.Node {
	islands := island.NewRenderer("counter")
	islands.SetProgramAsset("Counter", "/gosx/islands/Counter.json", "json", "")

	body := gosx.El("main", gosx.Attrs(gosx.Attr("class", "page-shell")),
		gosx.El("section", gosx.Attrs(gosx.Attr("class", "counter-panel")),
			gosx.El("h1", gosx.Text("GoSX Counter")),
			islands.RenderIslandFromProgram(counterProgram, map[string]int{"initial": count}),
			gosx.RawHTML(fmt.Sprintf(
				`<noscript><div class="counter no-js-counter"><a href="/?count=%d">-</a><span class="count">%d</span><a href="/?count=%d">+</a></div></noscript>`,
				count-1,
				count,
				count+1,
			)),
		),
	)

	return server.HTMLDocument(
		"GoSX Counter",
		gosx.Fragment(
			gosx.RawHTML(`<meta name="viewport" content="width=device-width, initial-scale=1">`),
			gosx.RawHTML(`<link rel="stylesheet" href="/counter.css">`),
			islands.PageHead(),
		),
		body,
	)
}
