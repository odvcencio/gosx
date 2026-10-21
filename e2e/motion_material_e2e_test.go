//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type emissiveReading struct {
	Found bool `json:"found"`
	Value any  `json:"value"`
}

// A Go MaterialAnims declaration on a standalone scene must load its native
// kernel and animate. Missing WASM is a failure, including on a Canvas fallback.
func TestMotionMaterialProgramShips(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startProductionDocsApp(t)
	checkNativeMaterialMotion(t, chrome, app, "production")
}

func TestMotionMaterialProgramDev(t *testing.T) {
	chrome := e2eChromePath(t)
	app := startDocsApp(t, fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t)))
	checkNativeMaterialMotion(t, chrome, app, "development")
}

func checkNativeMaterialMotion(t *testing.T, chrome string, app *docsApp, mode string) {
	t.Helper()
	proof := newRuntimeBrowserProof(t, filepath.Join(e2eRepoRoot(t), "examples", "gosx-docs", "app", "test", "motion-material", "page.server.go"))
	defer writeRuntimeBrowserProof(t, "material-motion-"+mode+".json", &proof)
	page := newBrowserPage(t, chrome, nil, 1280, 800, "", 120*time.Second)
	fixtureURL := app.baseURL + "/test/motion-material"
	response, err := http.Get(fixtureURL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("fixture SSR failed: status=%d err=%v\n%s", response.StatusCode, err, app.logs.String())
	}
	for _, declaration := range []string{"materialMotionProgram", "customUniforms", `"runtime":"kernel"`} {
		if !strings.Contains(string(body), declaration) {
			t.Fatalf("SSR missing %s", declaration)
		}
	}
	if status := page.navigate(t, fixtureURL); status < 200 || status > 299 {
		t.Fatalf("fixture page returned %d\n%s", status, app.logs.String())
	}
	page.waitFor(t, `typeof window.__gosx_motion_tick === "function" && !!document.querySelector("[data-gosx-scene3d-mounted]")`, 45*time.Second, "standalone material kernel and scene mount")
	page.eval(t, "navigator.userAgent", &proof.Browser)
	page.eval(t, `JSON.parse(document.getElementById("gosx-manifest").textContent).runtime`, &proof.Runtime)
	if mode == "development" && (!strings.HasPrefix(proof.Runtime.Path, "/gosx/runtime.wasm?v=") || len(proof.Runtime.Hash) != 64) {
		t.Fatalf("development served stale production runtime metadata: %+v", proof.Runtime)
	}
	var optedIn bool
	page.eval(t, `window.__gosx_motion_wasm === true`, &optedIn)
	if optedIn {
		t.Fatal("standalone material motion must work without an application opt-in flag")
	}
	const readUniform = `(() => {
    const mount = document.querySelector("[data-gosx-scene3d-mounted]");
    const state = mount && mount.__gosxScene3DState;
    const object = state && state.objects && state.objects.get("glow-cube");
    const material = object && (state.materials || []).find(item => item.name === object.material || item.id === object.material);
    const target = material || object;
    const value = target && target.customUniforms && target.customUniforms.emissive;
    return {found: Array.isArray(value), value: Array.isArray(value) ? value.slice() : null};
  })()`
	var first emissiveReading
	page.waitFor(t, "("+readUniform+").found", 15*time.Second, "live emissive uniform")
	page.eval(t, readUniform, &first)
	serialized, err := json.Marshal(first.Value)
	if err != nil {
		t.Fatal(err)
	}
	quoted, _ := json.Marshal(string(serialized))
	page.waitFor(t, fmt.Sprintf(`(() => { const next = %s; return next.found && JSON.stringify(next.value) !== %s; })()`, readUniform, quoted), 10*time.Second, "material motion advancing without another animation source")
	var advanced emissiveReading
	page.eval(t, readUniform, &advanced)
	proof.Steps = append(proof.Steps, devRuntimeStep{Input: "automatic native frames, no application opt-in flag", Expected: "uniform values advance", Observed: []any{first.Value, advanced.Value}})
	for _, sample := range []struct {
		time float64
		want []float64
	}{
		{0.5, []float64{0.65, 0.9, 1, 1}},
		{1.5, []float64{0.15, 0.2, 0.3, 1}},
	} {
		page.eval(t, fmt.Sprintf(`document.querySelector("[data-gosx-scene3d-mounted]").__gosxScene3DHandle.setAnimationClock({timeSeconds:%g,paused:true})`, sample.time), nil)
		page.waitFor(t, `(() => { const clock = document.querySelector("[data-gosx-scene3d-mounted]").__gosxScene3DHandle.getAnimationClock(); return clock.paused && !clock.pending; })()`, 5*time.Second, "paused material seek submission")
		var uniform []float64
		page.eval(t, "("+readUniform+").value", &uniform)
		if len(uniform) != len(sample.want) {
			t.Fatalf("seek %g returned %v", sample.time, uniform)
		}
		for i, want := range sample.want {
			if math.Abs(uniform[i]-want) > 1e-9 {
				t.Fatalf("seek %g component %d = %g, want %g", sample.time, i, uniform[i], want)
			}
		}
		proof.Steps = append(proof.Steps, devRuntimeStep{Input: fmt.Sprintf("pause and seek to %g seconds", sample.time), Expected: sample.want, Observed: uniform})
	}
	var backend string
	page.eval(t, `document.querySelector("[data-gosx-scene3d-mounted]").getAttribute("data-gosx-scene3d-backend")`, &backend)
	if !containsString([]string{"webgl", "webgpu", "canvas", "canvas2d"}, backend) {
		t.Fatalf("unexpected backend %q", backend)
	}
	t.Logf("standalone Go/Selena material animation advanced on %s without application flags", backend)
}
