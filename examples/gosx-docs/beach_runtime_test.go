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
	"testing"

	"m31labs.dev/gosx/server"
)

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
