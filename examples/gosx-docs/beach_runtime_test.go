package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"m31labs.dev/gosx/server"
)

func TestBlackglassBeachInstructionsFollowView(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	configureDocsTestSecret(t)
	app, err := buildDocsApp(server.ResolveAppRoot(thisFile), "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	for _, tc := range []struct {
		view, resolved, desktop, touch string
	}{
		{"shore", "shore", "WASD or arrows to walk", "Left thumb to move"},
		{"ship", "ship", "WASD or arrows to walk", "Left thumb to move"},
		{"glass", "glass", "WASD or arrows to walk", "Left thumb to move"},
		{"cliff", "cliff", "WASD or arrows to walk", "Left thumb to move"},
		{"pools", "pools", "WASD or arrows to walk", "Left thumb to move"},
		{"unknown", "shore", "WASD or arrows to walk", "Left thumb to move"},
	} {
		t.Run(tc.view, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/demos/beacon?view="+tc.view+"&period=blue-hour", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("beach returned %d", w.Code)
			}
			body := w.Body.String()
			for _, want := range []string{`data-view="` + tc.resolved + `"`, `data-period="blue-hour"`, tc.desktop, tc.touch} {
				if !strings.Contains(body, want) {
					t.Errorf("served beach is missing %q", want)
				}
			}
		})
	}
}

func TestBlackglassLivePresentationEndpoint(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	configureDocsTestSecret(t)
	app, err := buildDocsApp(server.ResolveAppRoot(file), "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/blackglass-view?view=cliff&from=noon&period=blue-hour", nil))
	var update struct {
		View, Period string
		Commands     []json.RawMessage
		Camera       map[string]any
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &update) != nil {
		t.Fatalf("presentation endpoint returned %d: %s", w.Code, w.Body.String())
	}
	if update.View != "cliff" || update.Period != "blue-hour" || len(update.Commands) == 0 || update.Camera["x"] != float64(-33) {
		t.Fatalf("incorrect authored presentation: %+v", update)
	}
	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/blackglass-view", nil))
	if post.Code == http.StatusOK {
		t.Fatal("presentation must be read-only")
	}
}

func TestBlackglassPoolJournalAndLazyCartridgeServe(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	configureDocsTestSecret(t)
	app, err := buildDocsApp(server.ResolveAppRoot(file), "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/demos/beacon?view=pools&period=blue-hour", nil))
	if page.Code != http.StatusOK {
		t.Fatalf("pool view returned %d", page.Code)
	}
	for _, want := range []string{`data-view="pools"`, `Field journal`, `Ochre stars`, `Anemone garden`, `limpet ledge`, `shore-handheld.glb`, `coast-handheld`, `data-period="blue-hour"`} {
		if !strings.Contains(page.Body.String(), want) {
			t.Fatalf("pool view missing %q", want)
		}
	}
	if strings.Contains(page.Body.String(), "boytacean_bg.wasm") || strings.Contains(page.Body.String(), "coast-walk.gb") {
		t.Fatal("cartridge assets must not be requested by the initial scene")
	}
	for _, retired := range []string{"blackglass-content.js", "blackglass-discoveries.js", "blackglass-chart.js", "blackglass-view.js", "blackglass-spyglass.js"} {
		if strings.Contains(page.Body.String(), retired) {
			t.Fatalf("Blackglass must express interactions through GoSX constructs: %s", retired)
		}
	}
	for path, contentType := range map[string]string{
		"/demos/blackglass-handheld/pocket.html":                  "text/html",
		"/gosx/gameboy/runtime.js":                                "javascript",
		"/gosx/gameboy/vendor/boytacean-0.13.2/boytacean_bg.wasm": "application/wasm",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Header().Get("Content-Type"), contentType) || response.Body.Len() == 0 {
			t.Fatalf("pocket asset %s: %d %s", path, response.Code, response.Header().Get("Content-Type"))
		}
		if strings.Contains(response.Body.String(), "coast-walk.gb") || strings.Contains(response.Body.String(), "included coast cartridge") {
			t.Fatalf("pocket asset %s must not preload or advertise the removed game", path)
		}
	}
	removed := httptest.NewRecorder()
	handler.ServeHTTP(removed, httptest.NewRequest(http.MethodGet, "/demos/blackglass-handheld/coast-walk.gb", nil))
	if removed.Code != http.StatusNotFound {
		t.Fatalf("removed cartridge still served: HTTP %d", removed.Code)
	}
}

// Exercise the served demo without a browser, including its engine manifest
// and walk data. These caps include the docs layout and the complete scene.
func TestBlackglassBeachDiscoveriesServeWithinWireBudget(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	root := server.ResolveAppRoot(thisFile)
	data, err := os.ReadFile(filepath.Join(root, "app", "demos", "beacon", "moments-budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	var limits struct {
		Page struct{ HTMLBytes, GzipBytes int }
	}
	if err := json.Unmarshal(data, &limits); err != nil {
		t.Fatal(err)
	}
	configureDocsTestSecret(t)
	app, err := buildDocsApp(root, "8080")
	if err != nil {
		t.Fatal(err)
	}
	handler := app.Build()
	for _, period := range []string{"golden-hour", "blue-hour", "noon"} {
		t.Run(period, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/demos/beacon?view=shore&period="+period, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("beach returned HTTP %d", w.Code)
			}
			body := w.Body.Bytes()
			if !bytes.Contains(body, []byte(`data-period="`+period+`"`)) || !bytes.Contains(body, []byte(`data-view="shore"`)) {
				t.Fatal("served beach must preserve the requested period and view")
			}
			for _, id := range []string{"beacon-beam", "tide-pool-0", "wreck-ribs", "glass-trail-soles", "sun-grotto-patch"} {
				if !bytes.Contains(body, []byte(id)) {
					t.Fatalf("served scene is missing %s", id)
				}
			}
			var compressed bytes.Buffer
			gz := gzip.NewWriter(&compressed)
			if _, err := gz.Write(body); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			t.Logf("served beach: %d HTML bytes, %d gzip bytes", len(body), compressed.Len())
			if len(body) > limits.Page.HTMLBytes || compressed.Len() > limits.Page.GzipBytes {
				t.Fatal("beach exceeded its HTML wire budget")
			}
		})
	}
}
