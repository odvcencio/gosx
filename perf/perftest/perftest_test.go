//go:build browser

package perftest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testPage = `<!DOCTYPE html>
<html>
<head><title>Perftest</title></head>
<body>
<span id="count">0</span>
<button id="inc" onclick="document.getElementById('count').textContent = parseInt(document.getElementById('count').textContent) + 1">+</button>
<script>
window.__gosx_runtime_ready = function() {};
window.__gosx = { islands: new Map(), engines: new Map() };
window.__gosx_runtime_ready();
document.dispatchEvent(new CustomEvent("gosx:ready"));
</script>
</body>
</html>`

func TestRunBasicPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, testPage)
	}))
	defer srv.Close()

	report := Run(t, srv.URL)
	if report.TTFBMs <= 0 {
		t.Fatalf("expected TTFB > 0, got %.2f", report.TTFBMs)
	}
	if report.DCLMs <= 0 {
		t.Fatalf("expected DCL > 0, got %.2f", report.DCLMs)
	}
}

func TestRunWithClick(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, testPage)
	}))
	defer srv.Close()

	report := Run(t, srv.URL, Click("#inc"))
	if len(report.Interactions) == 0 {
		t.Fatal("expected at least one interaction")
	}
}

func TestRunMultiPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>%s</title></head><body>
		<script>
		window.__gosx_runtime_ready = function() {};
		window.__gosx = { islands: new Map(), engines: new Map() };
		window.__gosx_runtime_ready();
		document.dispatchEvent(new CustomEvent("gosx:ready"));
		</script></body></html>`, r.URL.Path)
	}))
	defer srv.Close()

	report := RunMulti(t, []string{srv.URL + "/a", srv.URL + "/b"})
	if len(report.Pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(report.Pages))
	}
}
