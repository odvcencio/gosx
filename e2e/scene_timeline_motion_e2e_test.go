//go:build e2e

package e2e

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
	"m31labs.dev/gosx"
)

// The shipped Go/Selena example exercises compiled scene plans through real
// semantic controls. JavaScript below only observes the framework for the test.
func TestSceneTimelineGoControls(t *testing.T) {
	chrome, root := e2eChromePath(t), e2eRepoRoot(t)
	fixture := filepath.Join(root, "examples", "motion-shader")
	appDir := t.TempDir()
	copyFixtureTree(t, fixture, appDir)
	module := "module example.invalid/gosx-timeline-proof\n\ngo 1.26.0\n\nrequire m31labs.dev/gosx v" + strings.TrimPrefix(gosx.Version, "v") + "\n\nreplace m31labs.dev/gosx => " + strconv.Quote(filepath.ToSlash(root)) + "\n"
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	app := startDevApp(t, appDir, fmt.Sprintf("http://127.0.0.1:%d", freeE2EPort(t)))
	for _, viewport := range []struct {
		name          string
		width, height int
	}{{"desktop", 1280, 900}, {"mobile", 390, 844}} {
		t.Run(viewport.name, func(t *testing.T) {
			proof := newRuntimeBrowserProof(t, filepath.Join(fixture, "main.go"))
			defer writeRuntimeBrowserProof(t, "scene-timeline-"+viewport.name+".json", &proof)
			page := newBrowserPage(t, chrome, map[string]any{"use-angle": "swiftshader", "enable-unsafe-swiftshader": true}, viewport.width, viewport.height, "", 120*time.Second)
			if err := chromedp.Run(page.ctx, emulation.SetDeviceMetricsOverride(int64(viewport.width), int64(viewport.height), 1, viewport.name == "mobile")); err != nil {
				t.Fatal(err)
			}
			if status := page.navigate(t, app.baseURL); status != http.StatusOK {
				t.Fatalf("fixture HTTP %d\n%s", status, app.logs.String())
			}
			page.waitFor(t, `!!document.querySelector('#form')?.__gosxScene3DHandle?.getCamera && !document.querySelector('[data-gosx-transport-action="pause"]').disabled`, 30*time.Second, "scene and Go controls ready")
			page.eval(t, "navigator.userAgent", &proof.Browser)
			click := func(action, label string) {
				t.Helper()
				selector := `[data-gosx-transport-action="` + action + `"]`
				if label != "" {
					selector += `[data-gosx-transport-label="` + label + `"]`
				}
				if err := chromedp.Run(page.ctx, chromedp.Click(selector, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
			}
			const state = `(() => {const mount=document.getElementById('form'), h=mount.__gosxScene3DHandle; return {camera:h.getCamera(),clock:h.getAnimationClock(),transport:window.__gosx.motion.transport('tide-story').state(),chapter:parseFloat(document.querySelector('.chapter-0 .chapter-fill').style.width),backend:mount.getAttribute('data-gosx-scene3d-backend'),viewport:{width:innerWidth,height:innerHeight}};})()`
			read := func() map[string]any {
				t.Helper()
				var got map[string]any
				page.eval(t, state, &got)
				return got
			}
			pose := func(z, y, fov, seconds float64) map[string]any {
				t.Helper()
				page.waitFor(t, fmt.Sprintf(`(() => {const s=%s;return Math.abs(s.camera.z-%g)<1e-8&&Math.abs(s.camera.y-%g)<1e-8&&Math.abs(s.camera.fov-%g)<1e-8&&!s.clock.pending&&s.clock.paused&&Math.abs(s.clock.timeSeconds-%g)<1e-8;})()`, state, z, y, fov, seconds), 10*time.Second, "rendered camera and shader clock match shared seek")
				return read()
			}
			screenshot := func(name string) {
				t.Helper()
				if dir := os.Getenv("GOSX_E2E_PROOF_DIR"); dir != "" {
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "scene-timeline-"+viewport.name+"-"+name+".png"), page.screenshotElement(t, ".study"), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			click("pause", "")
			click("seek", "bloom")
			bloom := pose(8.725, .255, 45.75, 5)
			if got := bloom["viewport"].(map[string]any); got["width"].(float64) != float64(viewport.width) || got["height"].(float64) != float64(viewport.height) {
				t.Fatalf("viewport does not match requested metrics: %v", got)
			}
			if bloom["chapter"].(float64) != 100 {
				t.Fatalf("DOM chapter did not seek alongside camera: %v", bloom)
			}
			proof.Steps = append(proof.Steps, devRuntimeStep{Input: "click Pause, then The bloom · 5s", Expected: "camera z=8.725, y=.255, fov=45.75; shader clock=5s paused; DOM chapter=100%", Observed: bloom})
			screenshot("bloom")
			click("reverse", "")
			page.waitFor(t, `window.__gosx.motion.transport('tide-story').state().time<4800`, 5*time.Second, "reverse advances backward")
			click("pause", "")
			backward := read()
			if camera := backward["camera"].(map[string]any); !(camera["z"].(float64) > 8.725 && camera["z"].(float64) < 10) {
				t.Fatalf("reverse did not reconstruct camera pose: %v", backward)
			}
			proof.Steps = append(proof.Steps, devRuntimeStep{Input: "click Reverse, then Pause", Expected: "time decreases; camera moves back toward initial pose", Observed: backward})
			pausedTime := backward["transport"].(map[string]any)["time"].(float64)
			if err := chromedp.Run(page.ctx, chromedp.Focus(`[data-gosx-transport-action="play"]`, chromedp.ByQuery), chromedp.KeyEvent(kb.Enter)); err != nil {
				t.Fatal(err)
			}
			page.waitFor(t, fmt.Sprintf(`window.__gosx.motion.transport('tide-story').state().time>%g`, pausedTime+50), 5*time.Second, "keyboard Play resumes")
			click("pause", "")
			click("replay", "")
			initial := pose(10, 0, 50, 0)
			proof.Steps = append(proof.Steps, devRuntimeStep{Input: "focus Play and press Enter, then Pause and Back to start", Expected: "forward playback resumes, then all bindings return to initial pose", Observed: initial})
			screenshot("start")
			if err := chromedp.Run(page.ctx, emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}})); err != nil {
				t.Fatal(err)
			}
			settled := pose(8.5, .3, 45, 0)
			if at := settled["transport"].(map[string]any)["time"].(float64); math.Abs(at-14000) > 1e-8 {
				t.Fatalf("reduced motion did not settle transport: %v", settled)
			}
			proof.Steps = append(proof.Steps, devRuntimeStep{Input: "enable prefers-reduced-motion", Expected: "final camera; transport at 14s; shader clock at 0s paused", Observed: settled})
			if errors := page.PageErrors(); len(errors) != 0 {
				t.Fatalf("page errors: %v", errors)
			}
		})
	}
}
