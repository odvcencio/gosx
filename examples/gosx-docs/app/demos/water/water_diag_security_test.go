package docs

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/internal/chrometest"
)

func waterDiagSource(t *testing.T) []byte {
	t.Helper()
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "public", "water-diag.js"))
	if err != nil {
		t.Fatalf("read water-diag.js: %v", err)
	}
	return source
}

func TestWaterDiagAvoidsHTMLParsingSinks(t *testing.T) {
	source := string(waterDiagSource(t))
	for _, sink := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write"} {
		if strings.Contains(source, sink) {
			t.Fatalf("water-diag.js uses HTML parsing sink %q", sink)
		}
	}
	for _, textAssignment := range []string{
		"name.textContent = String(label)",
		"result.textContent = String(value)",
		"element.textContent = String(text)",
	} {
		if !strings.Contains(source, textAssignment) {
			t.Fatalf("water-diag.js lost text-only DOM assignment %q", textAssignment)
		}
	}
}

func TestWaterDiagRendersQueryAndAttributeValuesAsText(t *testing.T) {
	const assertionTimeout = 15 * time.Second

	chromePath := ""
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			chromePath = path
			break
		}
	}
	if chromePath == "" {
		t.Skip("Chrome/Chromium is unavailable; text-sink source contract still ran")
	}

	source := waterDiagSource(t)
	queryAttack := `<img data-water-query-attack src=x onerror="window.__waterQueryAttack=true">QUERY_ATTACK_MARKER`
	attributeAttack := `<svg data-water-attribute-attack onload="window.__waterAttributeAttack=true">ATTRIBUTE_ATTACK_MARKER</svg>`

	mux := http.NewServeMux()
	mux.HandleFunc("/water-diag.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(source)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html>
<html><body>
<script>window.__waterQueryAttack=false;window.__waterAttributeAttack=false;</script>
<div data-gosx-scene3d data-gosx-scene3d-webgpu-adapter="` + html.EscapeString(attributeAttack) + `">
  <canvas width="8" height="8"></canvas>
</div>
<script src="/water-diag.js"></script>
</body></html>`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	browser, err := chrometest.Start(t.Context(), chromePath,
		"--no-sandbox",
		"--disable-gpu",
	)
	if err != nil {
		t.Fatalf("start Chrome for water diagnostics: %v", err)
	}
	defer browser.Close()

	// Startup and the payload-rendering/XSS assertion have independent hard
	// bounds, so a recovered transient launch cannot consume assertion time.
	ctx, cancelAssertion := context.WithTimeout(browser.Context, assertionTimeout)
	defer cancelAssertion()

	var got struct {
		Text            string `json:"text"`
		QueryNode       bool   `json:"queryNode"`
		AttributeNode   bool   `json:"attributeNode"`
		QueryExecuted   bool   `json:"queryExecuted"`
		AttributeRan    bool   `json:"attributeRan"`
		UnexpectedNodes int    `json:"unexpectedNodes"`
	}
	target := server.URL + "/?diag=1&meshRes=" + url.QueryEscape(queryAttack)
	err = chromedp.Run(ctx,
		chromedp.Navigate(target),
		chromedp.WaitReady(`[data-water-diag]`, chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const overlay = document.querySelector("[data-water-diag]");
			return {
				text: overlay.textContent,
				queryNode: Boolean(overlay.querySelector("[data-water-query-attack]")),
				attributeNode: Boolean(overlay.querySelector("[data-water-attribute-attack]")),
				queryExecuted: Boolean(window.__waterQueryAttack),
				attributeRan: Boolean(window.__waterAttributeAttack),
				unexpectedNodes: overlay.querySelectorAll("img,svg,script").length,
			};
		})()`, &got),
	)
	if err != nil {
		t.Fatalf("run water diagnostics in Chrome: %v", err)
	}
	if !strings.Contains(got.Text, queryAttack) {
		t.Fatalf("query payload was not preserved as text; overlay text = %q", got.Text)
	}
	if !strings.Contains(got.Text, attributeAttack) {
		t.Fatalf("attribute payload was not preserved as text; overlay text = %q", got.Text)
	}
	if got.QueryNode || got.AttributeNode || got.QueryExecuted || got.AttributeRan || got.UnexpectedNodes != 0 {
		t.Fatalf("attacker-controlled values reached executable markup: %+v", got)
	}
}
