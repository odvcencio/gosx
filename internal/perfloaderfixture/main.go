// Command perfloaderfixture renders private loader test documents with the
// production shell. A subprocess avoids the island/server test import cycle.
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/internal/basepath"
	"m31labs.dev/gosx/server"
)

func main() {
	var inputs []struct {
		Base, Head, Body string
		Runtime          server.PageRuntimeSummary
		Navigation       bool
	}
	if err := json.NewDecoder(os.Stdin).Decode(&inputs); err != nil {
		panic(err)
	}
	pages := []string{}
	for _, input := range inputs {
		w := httptest.NewRecorder()
		basepath.Handler(input.Base, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := server.NewPageStateForRequest(r)
			state.AddHead(gosx.RawHTML(input.Head))
			doc := server.DocumentContext{Request: r, Pattern: "GET /", Path: input.Base + "/", Status: 200, Title: "Loader fixture",
				Bootstrap: input.Runtime.Bootstrap, RuntimeActive: input.Runtime.Manifest, Runtime: input.Runtime, Navigation: input.Navigation,
				Head: state.Head(), Body: gosx.RawHTML(input.Body)}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(basepath.HTML(input.Base, gosx.RenderHTML(server.HTMLDocument(&doc)))))
		})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
		pages = append(pages, w.Body.String())
	}
	if err := json.NewEncoder(os.Stdout).Encode(pages); err != nil {
		panic(err)
	}
}
