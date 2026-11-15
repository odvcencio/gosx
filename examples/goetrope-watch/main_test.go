package main

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
)

// TestRoutesRenderServerData loads each route through the real file router and
// checks that the data from page.server.go reaches the strict components.
func TestRoutesRenderServerData(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	app, err := newApp(server.ResolveAppRoot(thisFile))
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()

	cases := []struct {
		path string
		want []string
	}{
		{"/", []string{
			"Reimagine the viewer",    // hero data, read as data.hero.title
			"Canonical state stays",   // HighlightCard, spread from a struct
			"Aliens: Special Edition", // RoomPreview, spread from a struct
			"Prewarmed",               // RoomPreview subtitle state
		}},
		{"/watch/alpha", []string{
			"Room Alpha",
			"Transport shell",     // TransportShell
			"Reserve client work", // TransportShell subtitle
			"Alien Resurrection",  // QueueRow
			"Held across reloads", // Moment
		}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d\n%s", rec.Code, rec.Body)
			}
			body := rec.Body.String()
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("response lacks %q", want)
				}
			}
		})
	}
}
