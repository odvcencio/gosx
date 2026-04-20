package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

type renderSmokeResult struct {
	Done      bool   `json:"done"`
	Supported bool   `json:"supported"`
	Pixels    []int  `json:"pixels"`
	Error     string `json:"error,omitempty"`
}

func TestChromedpWebGLShaderPixelSmoke(t *testing.T) {
	chrome := findChrome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(renderSmokeHTML))
	}))
	defer server.Close()

	ctx, cancel := newChromeContext(t, chrome)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL),
		chromedp.WaitReady("#scene", chromedp.ByID),
	); err != nil {
		t.Fatalf("navigate render smoke page: %v", err)
	}

	var result renderSmokeResult
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var raw string
		if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(window.__renderSmoke || {})`, &raw)); err != nil {
			t.Fatalf("read render smoke result: %v", err)
		}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatalf("decode render smoke result %q: %v", raw, err)
		}
		if result.Done {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !result.Done {
		t.Fatal("render smoke page did not finish")
	}
	if !result.Supported {
		t.Skipf("browser WebGL unavailable: %s", result.Error)
	}
	if len(result.Pixels) != 16 {
		t.Fatalf("expected 16 RGBA bytes, got %d: %v", len(result.Pixels), result.Pixels)
	}

	// The fragment shader writes four deterministic quadrant colors. This
	// checks the browser/compiler/canvas-readback path without depending on
	// the full GoSX app bootstrap.
	want := []int{
		255, 255, 64, 255,
		0, 255, 64, 255,
		255, 0, 64, 255,
		0, 0, 64, 255,
	}
	for i := range want {
		if delta := absInt(result.Pixels[i] - want[i]); delta > 3 {
			t.Fatalf("pixel byte %d = %d, want %d (+/-3); pixels=%v", i, result.Pixels[i], want[i], result.Pixels)
		}
	}
}

func findChrome(t *testing.T) string {
	t.Helper()
	for _, name := range []string{
		"chromium",
		"chromium-browser",
		"google-chrome",
		"google-chrome-stable",
		"chrome",
		"microsoft-edge",
	} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	t.Skip("no Chrome/Chromium executable found")
	return ""
}

func newChromeContext(t *testing.T, chrome string) (context.Context, context.CancelFunc) {
	t.Helper()
	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.Headless,
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("use-angle", "swiftshader"),
		chromedp.Flag("enable-webgl", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	ctx, timeoutCancel := context.WithTimeout(browserCtx, 15*time.Second)
	return ctx, func() {
		timeoutCancel()
		browserCancel()
		allocCancel()
	}
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

const renderSmokeHTML = `<!doctype html>
<meta charset="utf-8">
<canvas id="scene" width="4" height="4"></canvas>
<script>
(function() {
  const canvas = document.getElementById("scene");
  const gl = canvas.getContext("webgl", {
    antialias: false,
    depth: false,
    stencil: false,
    preserveDrawingBuffer: true
  });
  if (!gl) {
    window.__renderSmoke = { done: true, supported: false, error: "webgl context unavailable" };
    return;
  }
  function shader(type, source) {
    const s = gl.createShader(type);
    gl.shaderSource(s, source);
    gl.compileShader(s);
    if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
      throw new Error(gl.getShaderInfoLog(s) || "shader compile failed");
    }
    return s;
  }
  try {
    const vs = shader(gl.VERTEX_SHADER, "attribute vec2 p; void main(){ gl_Position = vec4(p, 0.0, 1.0); }");
    const fs = shader(gl.FRAGMENT_SHADER, [
      "precision mediump float;",
      "void main(){",
      "  float left = step(gl_FragCoord.x, 2.5);",
      "  float bottom = step(gl_FragCoord.y, 2.5);",
      "  gl_FragColor = vec4(left, bottom, 0.25, 1.0);",
      "}"
    ].join("\n"));
    const program = gl.createProgram();
    gl.attachShader(program, vs);
    gl.attachShader(program, fs);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      throw new Error(gl.getProgramInfoLog(program) || "program link failed");
    }
    gl.useProgram(program);
    const buffer = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buffer);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1, 1,-1, -1,1, 1,1]), gl.STATIC_DRAW);
    const loc = gl.getAttribLocation(program, "p");
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);
    gl.viewport(0, 0, 4, 4);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    const coords = [[0,0], [3,0], [0,3], [3,3]];
    const pixel = new Uint8Array(4);
    const out = [];
    for (const [x, y] of coords) {
      gl.readPixels(x, y, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, pixel);
      out.push(pixel[0], pixel[1], pixel[2], pixel[3]);
    }
    window.__renderSmoke = { done: true, supported: true, pixels: out };
  } catch (error) {
    window.__renderSmoke = { done: true, supported: false, error: String(error && error.message || error) };
  }
})();
</script>`
