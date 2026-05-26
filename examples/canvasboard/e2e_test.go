package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/server"
)

// TestCanvasBoardExampleRendersCanvasPlaceholder is the E1.3 end-to-end
// acceptance: the example's HomePage emits a <canvas> placeholder with the
// data-gosx-surface-kind="canvas2d" dispatch flag so the WASM bootstrap
// hydrates it as a CanvasBoardAdapter. Full Playwright coverage (pan, zoom,
// click, marquee, drag, drop) requires a live browser harness; this test
// guards the SSR contract that drives every later hydration step.
func TestCanvasBoardExampleRendersCanvasPlaceholder(t *testing.T) {
	app := server.New()
	app.SetLayout(func(title string, body gosx.Node) gosx.Node {
		return server.HTMLDocument(title, gosx.Node{}, body)
	})
	app.Route("/", func(r *http.Request) gosx.Node {
		return HomePage()
	})

	handler := app.Build()
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readAll(t, resp)

	// Surface-kind dispatch attribute (Phase 1d's __gosx_hydrate(surfaceKind, ...))
	if !strings.Contains(body, `data-gosx-surface-kind="canvas2d"`) {
		t.Errorf("missing surface-kind dispatch attribute in response body")
	}
	// Component name lets the bootstrap pick the right WASM module.
	if !strings.Contains(body, `data-gosx-engine-component="CanvasBoard"`) {
		t.Errorf("missing engine-component attribute")
	}
	// Props payload must be present so the adapter has board state at mount.
	if !strings.Contains(body, `data-gosx-engine-props=`) {
		t.Errorf("missing engine-props attribute")
	}
	// Default dimensions resolved by the CanvasBoard helper.
	if !strings.Contains(body, `width="1280"`) || !strings.Contains(body, `height="720"`) {
		t.Errorf("missing 1280x720 canvas dimensions")
	}
	// Pick handler is opt-in; this example opts in.
	if !strings.Contains(body, `data-gosx-onpick="handleBoardPick"`) {
		t.Errorf("OnPick handler not propagated to DOM")
	}
}

// TestCanvasBoardExampleEmitsExpectedNodeCount checks the sample emits the
// full 100-rect board (E1.1 spec). A drift here would silently break the
// "100 nodes ≤ 50ms cold mount" bench in F1.1.
func TestCanvasBoardExampleEmitsExpectedNodeCount(t *testing.T) {
	nodes := generateBoardNodes()
	if got, want := len(nodes), boardCols*boardRows; got != want {
		t.Errorf("generateBoardNodes() = %d, want %d", got, want)
	}
}

// TestCanvasBoardExampleHomePageIsServerRenderable is a smoke check that
// HomePage produces a non-empty Node tree without panicking. Guards against
// future refactors that might break the SSR path.
func TestCanvasBoardExampleHomePageIsServerRenderable(t *testing.T) {
	node := HomePage()
	html := gosx.RenderHTML(node)
	if len(html) == 0 {
		t.Fatal("HomePage rendered empty HTML")
	}
	if !strings.Contains(html, "<canvas") {
		t.Errorf("HomePage HTML missing <canvas>: %s", html)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	buf := make([]byte, 0, 8192)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf)
}
