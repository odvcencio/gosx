package budget

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/buildmanifest"
)

func servedMeasurementBodies(t *testing.T, bodies map[string][]byte) (string, *http.Client) {
	t.Helper()
	served := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		kind := "text/html"
		if strings.HasSuffix(r.URL.Path, ".js") {
			kind = "text/javascript"
		}
		if strings.HasSuffix(r.URL.Path, ".css") {
			kind = "text/css"
		}
		w.Header().Set("Content-Type", kind)
		_, _ = w.Write(body)
	}))
	t.Cleanup(served.Close)
	return served.URL, served.Client()
}

func TestMeasureAnalysisLimitKeepsConservativeCosts(t *testing.T) {
	opts, manifest, document, _ := testRouteMeasurement(t)
	script := []byte(strings.Repeat("function f(){"+strings.Repeat("x;", 49)+"}\n", 2450) + strings.Repeat("x;", 50))
	style := []byte("body{color:blue}")
	// Inventory startup proves the script may run independently of the root.
	// Its unscanned suffix must keep all potential bodies charged at startup.
	manifest.Assets = []buildmanifest.PerfAssetUse{manifest.Assets[0],
		graphAsset("framework/runtime/limited.js", "/limited.js", "js", "startup", "always", script),
		graphAsset("app/fixture/public/potential.css", "/potential.css", "css", "dormant", "always", style)}
	for name, body := range map[string][]byte{"limited.js": script, "potential.css": style} {
		if err := os.WriteFile(filepath.Join(opts.DistDir, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFixtureManifest(t, opts.DistDir, manifest)
	// Use the actual HTTP collector against the staged verified bodies.
	opts.BaseURL, opts.Client = servedMeasurementBodies(t, map[string][]byte{"/counter/": document, "/limited.js": script, "/potential.css": style})
	report, err := measureApp(context.Background(), opts, testBodyNormalizer)
	if err != nil {
		t.Fatal("bounded scan became a fixture rejection", err)
	}
	docSizes, _ := testBodyNormalizer(document)
	scriptSizes, _ := testBodyNormalizer(script)
	styleSizes, _ := testBodyNormalizer(style)
	row := report.Rows[0]
	if report.Coverage.Reachability != "unknown" || row.Requests != 3 || row.NormalizedBytes != docSizes.Brotli+scriptSizes.Brotli+styleSizes.Brotli || row.PhaseBytes.Startup != scriptSizes.Brotli+styleSizes.Brotli || row.PhaseBytes.Dormant != 0 {
		t.Fatalf("bounded scan lost conservative accounting: %+v", report)
	}
	for _, policy := range row.Policies {
		if policy.Name == "declared-fetches" && policy.Passed {
			t.Fatal("bounded scan certified declared fetches")
		}
	}
}
