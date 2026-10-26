package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Exercise GSX evaluation and the document shell together: a mount without its
// runtime head looks like a valid page but can never render the engine.
func TestTurbofanPageIncludesRuntime(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	for _, target := range []string{"/", "/?shell=full&motion=paused&view=intake"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", target, response.Code, response.Body.String())
		}
		body := response.Body.String()
		for _, fragment := range []string{"Inside the turbofan", "turbofan.css", "GoSXScene3D", "bootstrap", `"id":"fan"`, "Fan &amp; bypass",
			`data-gosx-engine="TurbofanCycle"`, `"runtime":"go-wasm"`, `/gosx/assets/go-wasm/cycle.`, `data-cycle-output="net-thrust">99.5`, `data-cycle-station="45"`} {
			if !strings.Contains(body, fragment) {
				t.Fatalf("%s: page is missing %q", target, fragment)
			}
		}
	}
}

func TestMappedProfileServerFallback(t *testing.T) {
	app, err := newApp()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	app.Build().ServeHTTP(w, httptest.NewRequest("GET", "/?model=jt9d", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	for _, s := range []string{`data-cycle-mode="mapped"`, `data-cycle-output="net-thrust">213.5`, `ICAO 1PW026`, `NASA MAP SHAPES`, `unfitted check`} {
		if !strings.Contains(w.Body.String(), s) {
			t.Errorf("mapped server page missing %q", s)
		}
	}
}
