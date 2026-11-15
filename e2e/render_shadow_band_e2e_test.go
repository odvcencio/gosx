//go:build e2e

// Real-GPU check that cascaded directional shadows do not self-shadow a lit,
// convex surface: the Blackglass Coast beacon tower under the noon sun. Run on
// the charter GPU runners like the other render-upgrade checks, for example:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -timeout 20m -run TestBeaconTowerShadowBand ./e2e/
package e2e

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestBeaconTowerShadowBand renders the beacon view with the sun's shadow on
// and off, for the authored three cascades and for four cascades within 80 m.
// No caster sits between the sun and the tower's middle, so every row of the
// tower region must keep at least 85% of its unshadowed brightness. A
// self-shadow band (depth acne, or a filter kernel reaching past a sloped
// receiver) darkens whole rows and fails the check.
func TestBeaconTowerShadowBand(t *testing.T) {
	requireRenderGPU(t)
	root := e2eRepoRoot(t)
	for _, backend := range renderUpgradeBackends() {
		t.Run(backend, func(t *testing.T) {
			off := beaconTowerShot(t, root, backend, 0)
			for _, cascades := range []int{3, 4} {
				on := beaconTowerShot(t, root, backend, cascades)
				worst, row, mean := beaconTowerRowRatio(on, off)
				t.Logf("%s cascades=%d tower brightness with shadows: worst row %.2f (row %d), mean %.2f", backend, cascades, worst, row, mean)
				if worst < 0.85 {
					t.Errorf("%s cascades=%d self-shadows the tower: row %d keeps %.2f of its unshadowed brightness", backend, cascades, row, worst)
				}
			}
		})
	}
}

// beaconTowerShot captures the beacon view with the sun casting shadows
// through the given number of cascades, or with no sun shadow when cascades
// is 0.
func beaconTowerShot(t *testing.T, root, backend string, cascades int) image.Image {
	t.Helper()
	props := beaconProps(t, backend, false)
	sceneRecord, _ := props["scene"].(map[string]any)
	if sceneRecord == nil {
		sceneRecord = props
	}
	found := false
	if lights, ok := sceneRecord["lights"].([]any); ok {
		for _, raw := range lights {
			light, ok := raw.(map[string]any)
			if !ok || light["id"] != "sun" {
				continue
			}
			found = true
			light["castShadow"] = cascades > 0
			if cascades > 0 {
				light["shadowCascades"] = cascades
			}
			if cascades == 4 {
				light["shadowDistance"] = 80
			}
		}
	}
	if !found {
		t.Fatal("beacon scene has no light with id sun")
	}
	url := renderUpgradeServer(t, root, renderUpgradePage(t, "band-root", 1280, 720, props))
	browser := renderUpgradeBrowser(t, backend, 1280, 720, "")
	browser.navigate(t, url+"/")
	mounted := false
	for deadline := time.Now().Add(90 * time.Second); time.Now().Before(deadline); time.Sleep(500 * time.Millisecond) {
		var active string
		browser.eval(t, `document.querySelector('[data-gosx-scene3d-mounted]')?.getAttribute('data-gosx-scene3d-backend')||''`, &active)
		if active == backend {
			mounted = true
			break
		}
	}
	if !mounted {
		t.Fatalf("%s never mounted; pageErrors=%v console=%s", backend, browser.PageErrors(), browser.Console())
	}
	time.Sleep(6 * time.Second) // models load and pipelines compile
	var shot []byte
	if err := chromedp.Run(browser.ctx, chromedp.Screenshot("canvas", &shot, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("GOSX_RENDER_OUT"); out != "" {
		_ = os.MkdirAll(out, 0o755)
		_ = os.WriteFile(filepath.Join(out, fmt.Sprintf("band-%s-%d.png", backend, cascades)), shot, 0o644)
	}
	if errors := browser.PageErrors(); len(errors) != 0 {
		t.Fatalf("page errors: %v", errors)
	}
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// beaconTowerRowRatio compares the tower's middle row by row: the red
// stripe (31-50% of the height) and the white wall below it (57-64%). It
// skips the stripe's black lower trim (50-57%): that lip stands proud of the
// wall and casts a real contact shadow, and its dark rows turn small
// differences into large ratios. It returns the darkest row's brightness
// ratio, that row, and the mean ratio over the compared rows.
func beaconTowerRowRatio(on, off image.Image) (worst float64, worstRow int, mean float64) {
	b := on.Bounds()
	x0, x1 := b.Min.X+b.Dx()*37/100, b.Min.X+b.Dx()*63/100
	y0, y1 := b.Min.Y+b.Dy()*31/100, b.Min.Y+b.Dy()*64/100
	trim0, trim1 := b.Min.Y+b.Dy()*50/100, b.Min.Y+b.Dy()*57/100
	worst, worstRow = 2, y0
	var sumOn, sumOff float64
	for y := y0; y < y1; y++ {
		if y >= trim0 && y < trim1 {
			continue
		}
		var rowOn, rowOff float64
		for x := x0; x < x1; x++ {
			rowOn += lumaAt(on, x, y)
			rowOff += lumaAt(off, x, y)
		}
		sumOn += rowOn
		sumOff += rowOff
		if rowOff > 0 && rowOn/rowOff < worst {
			worst, worstRow = rowOn/rowOff, y
		}
	}
	if sumOff > 0 {
		mean = sumOn / sumOff
	}
	return worst, worstRow, mean
}
