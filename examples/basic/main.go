// Example basic demonstrates a simple GoSX server application.
//
// This example uses the runtime Node API directly (rather than .gsx source files)
// to build and render components on the server.
//
// Run: go run ./examples/basic
// Visit: http://localhost:8080
package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/server"
)

func main() {
	app := server.New()

	app.SetLayout(func(title string, body gosx.Node) gosx.Node {
		return server.HTMLDocument("GoSX Example - "+title, gosx.Node{}, body)
	})

	app.Route("/", func(r *http.Request) gosx.Node {
		return HomePage()
	})

	app.Route("/counter", func(r *http.Request) gosx.Node {
		countStr := r.URL.Query().Get("count")
		count, _ := strconv.Atoi(countStr)
		return CounterPage(count)
	})

	fmt.Println("GoSX example server running at http://localhost:8080")
	log.Fatal(app.ListenAndServe(":8080"))
}

// HomePage renders the home page.
func HomePage() gosx.Node {
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "container")),
		gosx.El("h1", gosx.Text("GoSX Example")),
		gosx.El("p", gosx.Text("A Go-native web application platform.")),
		gosx.El("nav",
			gosx.El("a", gosx.Attrs(gosx.Attr("href", "/")), gosx.Text("Home")),
			gosx.Text(" | "),
			gosx.El("a", gosx.Attrs(gosx.Attr("href", "/counter")), gosx.Text("Counter")),
		),
		gosx.El("hr"),
		gosx.El("p", gosx.Text("Server rendered at: "+time.Now().Format(time.RFC3339))),
	)
}

// CounterPage renders a server-side counter demonstration.
// Each button click does a full page navigation (server-first, no client JS).
func CounterPage(count int) gosx.Node {
	return gosx.El("div", gosx.Attrs(gosx.Attr("class", "container")),
		gosx.El("h1", gosx.Text("Counter")),
		gosx.El("nav",
			gosx.El("a", gosx.Attrs(gosx.Attr("href", "/")), gosx.Text("Home")),
			gosx.Text(" | "),
			gosx.El("a", gosx.Attrs(gosx.Attr("href", "/counter")), gosx.Text("Counter")),
		),
		gosx.El("hr"),
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "counter")),
			gosx.El("a",
				gosx.Attrs(gosx.Attr("href", fmt.Sprintf("/counter?count=%d", count-1))),
				gosx.Text("[ - ]"),
			),
			gosx.El("span",
				gosx.Attrs(gosx.Attr("style", "margin: 0 1em; font-size: 2em")),
				gosx.Expr(count),
			),
			gosx.El("a",
				gosx.Attrs(gosx.Attr("href", fmt.Sprintf("/counter?count=%d", count+1))),
				gosx.Text("[ + ]"),
			),
		),
	)
}
