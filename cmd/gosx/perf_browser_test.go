//go:build browser

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/perf"
)

func TestPerfCLIPrintsPartialJSONOnCollectionFailure(t *testing.T) {
	requireChromeForPerfCLI(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h1>ok</h1></body></html>`)
		case "/slow":
			time.Sleep(2 * time.Second)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h1>slow</h1></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cmd := exec.Command("go", "run", "./cmd/gosx", "perf",
		"--json",
		"--timeout", "750ms",
		srv.URL+"/ok",
		srv.URL+"/slow",
	)
	cmd.Dir = moduleRootForPerfCLITest(t)
	cmd.Env = append(os.Environ(), "GOSX_CHROME_NO_SANDBOX=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("gosx perf unexpectedly succeeded:\n%s", string(out))
	}
	text := string(out)
	start := strings.Index(text, "{")
	if start < 0 {
		t.Fatalf("expected partial JSON on stdout/stderr, got:\n%s", text)
	}
	end := strings.LastIndex(text, "}")
	if end < start {
		t.Fatalf("expected complete partial JSON object, got:\n%s", text)
	}
	var report perf.Report
	if err := json.Unmarshal([]byte(text[start:end+1]), &report); err != nil {
		t.Fatalf("partial JSON is not parseable: %v\n%s", err, text[start:end+1])
	}
	if len(report.Pages) != 1 || report.Pages[0].URL != srv.URL+"/ok" {
		t.Fatalf("expected exactly the completed first route in partial JSON, got %#v", report.Pages)
	}
	if !strings.Contains(text, "route 2/2 "+srv.URL+"/slow") ||
		!strings.Contains(text, "context deadline exceeded") {
		t.Fatalf("expected failing route identity in CLI output, got:\n%s", text)
	}
}

func requireChromeForPerfCLI(t *testing.T) {
	t.Helper()
	if _, err := perf.FindChrome(); err != nil {
		if os.Getenv("GOSX_REQUIRE_CHROME") != "" {
			t.Fatalf("GOSX_REQUIRE_CHROME is set, so Chrome is required for perf CLI regression: %v", err)
		}
		t.Skipf("skipping perf CLI regression: %v", err)
	}
}

func moduleRootForPerfCLITest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("resolve module root from %s: %v", wd, err)
	}
	return root
}
