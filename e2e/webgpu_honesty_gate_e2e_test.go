//go:build e2e

// Port of the retired e2e/webgpu_honesty_gate_e2e.test.mjs (playwright) to
// chromedp.
//
// Navigates to the skinned-GLB fixture page served by gosx-docs at
// /test/webgpu-honesty-gate. That route sets preferWebGPU=true and includes a
// model that the server-side SkinLookup marks as skinned, so the honesty gate
// must ship backendCaps that allow WebGPU/WebGL and exclude canvas2d for
// skinning. The JS runtime mounts WebGL when headless Chrome has no usable
// WebGPU adapter and reports the environment fallback honestly:
//
//	data-gosx-scene3d-backend           = "webgl"
//	data-gosx-scene3d-renderer-fallback = "webgpu-unavailable"
package e2e

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func honestyGateBaseURL() string {
	if url := os.Getenv("GOSX_E2E_BASE_URL"); url != "" {
		return url
	}
	// Distinct port so this suite can run alongside the other e2e suites.
	return "http://127.0.0.1:3071"
}

type backendCaps struct {
	Capable []string `json:"capable"`
	Reasons []struct {
		Feature  string `json:"feature"`
		Excludes string `json:"excludes"`
	} `json:"reasons"`
}

func TestWebGPUHonestyGate(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, honestyGateBaseURL())
	page := newBrowserPage(t, chrome, nil, 1280, 800, "", 90*time.Second)

	fixturePath := "/test/webgpu-honesty-gate"
	if status := page.navigate(t, app.baseURL+fixturePath); status < 200 || status > 299 {
		t.Fatalf("fixture page returned %d\n\nLogs:\n%s", status, app.logs.String())
	}

	// Prove the server-side honesty verdict itself reached the browser. The
	// renderer's fallback reason may still be environment-specific when
	// headless Chrome has no usable WebGPU adapter.
	var capsList []backendCaps
	page.eval(t, `(() => {
    const script = document.getElementById("gosx-manifest");
    if (!script?.textContent) return [];
    const manifest = JSON.parse(script.textContent);
    const matches = [];
    const seen = new Set();
    function visit(value) {
      if (!value || typeof value !== "object" || seen.has(value)) return;
      seen.add(value);
      if (value.backendCaps && typeof value.backendCaps === "object") {
        matches.push(value.backendCaps);
      }
      if (Array.isArray(value)) value.forEach(visit);
      else Object.values(value).forEach(visit);
    }
    visit(manifest);
    return matches;
  })()`, &capsList)

	found := false
	for _, caps := range capsList {
		if !containsString(caps.Capable, "webgpu") || !containsString(caps.Capable, "webgl") {
			continue
		}
		for _, reason := range caps.Reasons {
			if reason.Feature == "skinning" && reason.Excludes == "canvas2d" {
				found = true
				break
			}
		}
	}
	if !found {
		blob, _ := json.Marshal(capsList)
		t.Fatalf("fixture manifest omitted the current skinning capability contract: %s\n\nLogs:\n%s", blob, app.logs.String())
	}

	// Wait for the Scene3D mount to finish initialising. The runtime sets
	// data-gosx-scene3d-backend once it has selected and started the renderer.
	page.waitFor(t,
		`!!document.querySelector("[data-gosx-scene3d-mounted][data-gosx-scene3d-backend]")`,
		30*time.Second, "[data-gosx-scene3d-mounted][data-gosx-scene3d-backend]")

	var attrs struct {
		Backend  string `json:"backend"`
		Fallback string `json:"fallback"`
	}
	page.eval(t, `(() => {
    const el = document.querySelector("[data-gosx-scene3d-mounted]");
    if (!el) return null;
    return {
      backend: el.getAttribute("data-gosx-scene3d-backend"),
      fallback: el.getAttribute("data-gosx-scene3d-renderer-fallback"),
    };
  })()`, &attrs)

	if attrs.Backend == "" {
		t.Fatalf("no [data-gosx-scene3d-mounted] element found on %s\n\nLogs:\n%s", fixturePath, app.logs.String())
	}
	if attrs.Backend != "webgl" {
		t.Fatalf("expected data-gosx-scene3d-backend=%q (honesty gate must downgrade from webgpu), got %q\n\nLogs:\n%s",
			"webgl", attrs.Backend, app.logs.String())
	}
	if attrs.Fallback != "skinning" && attrs.Fallback != "webgpu-unavailable" {
		t.Fatalf("expected fallback %q from feature exclusion or %q without a usable adapter, got %q\n\nLogs:\n%s",
			"skinning", "webgpu-unavailable", attrs.Fallback, app.logs.String())
	}
}
