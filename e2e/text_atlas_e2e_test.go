//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/scene"
)

// An unlit jersey: a blue shirt with the player's number drawn from one
// shared text atlas. labelRect picks the player's cell; numberRect is where
// the number sits on the shirt's UVs. The helpers come from Selena's game
// material module.
const textAtlasJerseyMaterial = `material AtlasJersey {
    param labels     : texture2d
    param labelRect  : vec4 = vec4(0.0, 0.0, 1.0, 1.0)
    param numberRect : vec4 = vec4(0.2, 0.15, 0.8, 0.85)
    surface(geo) -> color {
        let glyph = sample(labels, mlAtlasUV(mlRectLocal(geo.uv, numberRect), labelRect))
        let ink = glyph.a * mlRectMask(geo.uv, numberRect)
        return mix(rgb(0.1, 0.25, 0.7), rgb(1.0, 1.0, 1.0), ink)
    }
}`

const (
	textAtlasPlayers = 44
	textAtlasColumns = 11
	textAtlasWidth   = 1100 // canvas pixels; the camera shows 22 x 11.2 world units, 50 px each
	textAtlasHeight  = 560
	textAtlasPPU     = 50.0
)

// textAtlasQuad is a w by h rectangle facing +Z whose UVs run from (0, 0) at
// the top-left corner to (1, 1) at the bottom-right, the glTF convention.
func textAtlasQuad(w, h float64) scene.BufferGeometry {
	x, y := w/2, h/2
	return scene.BufferGeometry{
		Positions: []float64{-x, -y, 0, x, -y, 0, x, y, 0, -x, y, 0},
		Normals:   []float64{0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1},
		UVs:       []float64{0, 1, 1, 1, 1, 0, 0, 0},
		Indices:   []int{0, 1, 2, 0, 2, 3},
	}
}

// textAtlasCenter is the world position of character i: 11 columns by 4 rows.
func textAtlasCenter(i int) (x, y float64) {
	return -10 + 2*float64(i%textAtlasColumns), 4.2 - 2.8*float64(i/textAtlasColumns)
}

// TestTextAtlasDrawsDistinctNumbersOnCharacters builds 44 characters from one
// template (head, jersey, nameplate). Every jersey uses the same Selena
// material and the same number atlas texture and differs only in its
// labelRect param (TextAtlas.RegionUniform); every nameplate uses one
// standard material with the nameplate atlas and differs only in its quad's
// UVs (TextAtlas.Quad). Five nameplates are blank. The test reads the canvas
// and checks that every jersey shows a number, that the 44 numbers are
// pairwise different shapes, that the named plates show text on their fill
// and that the blank plates show nothing. Run on both GPU runners:
//
//	/home/draco/work/.runtime/tools/gpu/gpu-chrome.sh run -- go test -tags e2e -run TestTextAtlasDrawsDistinctNumbersOnCharacters -timeout 10m ./e2e/
//	/home/draco/.local/bin/mac-gpu-run <worktree> go test -tags e2e -run TestTextAtlasDrawsDistinctNumbersOnCharacters -timeout 10m ./e2e/
func TestTextAtlasDrawsDistinctNumbersOnCharacters(t *testing.T) {
	chrome := e2eChromePath(t)
	root := e2eRepoRoot(t)
	bootstrap, err := os.ReadFile(filepath.Join(root, "client/js/bootstrap.js"))
	if err != nil {
		t.Fatal(err)
	}
	jersey, _, err := scene.CompileSelenaMaterial([]byte(textAtlasJerseyMaterial), scene.SelenaMaterialOptions{Modules: []string{"game"}})
	if err != nil {
		t.Fatal(err)
	}
	numbers := scene.TextAtlas{ID: "numbers", CellWidth: 96, CellHeight: 96, Padding: 0.1}
	plates := scene.TextAtlas{ID: "plates", CellWidth: 192, CellHeight: 48, Background: "#f2d14b", Color: "#111111", Padding: 0.12}
	for i := 0; i < textAtlasPlayers; i++ {
		// 37 and 100 are coprime, so 44 steps give 44 different numbers.
		numbers.Labels = append(numbers.Labels, strconv.Itoa((i*37+11)%100))
		name := fmt.Sprintf("Player %d", i+1)
		if textAtlasBlankPlate(i) {
			name = ""
		}
		plates.Labels = append(plates.Labels, name)
	}
	for _, a := range []scene.TextAtlas{numbers, plates} {
		if err := a.Validate(); err != nil {
			t.Fatal(err)
		}
	}

	nodes := []scene.Node{
		scene.DirectionalLight{ID: "key", Color: "#ffffff", Intensity: 0.6, Direction: scene.Vector3{Z: -1}},
	}
	jerseyGeometry := textAtlasQuad(1.6, 1.3)
	head := scene.SphereGeometry{Radius: 0.28, Segments: 16}
	plateMaterial := scene.StandardMaterial{Color: "#ffffff", Texture: plates.Texture(), Roughness: 1, BlendMode: scene.BlendAlpha, Opacity: scene.Float(1)}
	for i := 0; i < textAtlasPlayers; i++ {
		x, y := textAtlasCenter(i)
		shirt := jersey
		shirt.Uniforms = map[string]any{}
		for k, v := range jersey.Uniforms {
			shirt.Uniforms[k] = v
		}
		shirt.Uniforms["labels"] = numbers.Texture()
		shirt.Uniforms["labelRect"] = numbers.RegionUniform(i)
		nodes = append(nodes,
			scene.Mesh{ID: fmt.Sprintf("head-%d", i), Geometry: head, Position: scene.Vector3{X: x, Y: y + 0.95}, Material: scene.StandardMaterial{Color: "#d9a07a", Roughness: 0.8}},
			scene.Mesh{ID: fmt.Sprintf("jersey-%d", i), Geometry: jerseyGeometry, Position: scene.Vector3{X: x, Y: y}, Material: shirt},
			scene.Mesh{ID: fmt.Sprintf("plate-%d", i), Geometry: plates.Quad(i, 1.8, 0.45), Position: scene.Vector3{X: x, Y: y - 1.05, Z: 0.1}, Material: plateMaterial},
		)
	}

	backends := []string{"webgl"}
	if os.Getenv("GPU_CHROME_ENDPOINT") != "" || os.Getenv("GPU_CHROME_STATE") != "" || os.Getenv("GOSX_TEXT_ATLAS_WEBGPU") == "1" {
		backends = append(backends, "webgpu")
	}
	for _, backend := range backends {
		t.Run(backend, func(t *testing.T) {
			yes := true
			props := scene.Props{
				Width: textAtlasWidth, Height: textAtlasHeight, Background: "#000000",
				OrthographicCamera: &scene.OrthographicCamera{Position: scene.Vector3{Z: 10}, Left: -11, Right: 11, Top: 5.6, Bottom: -5.6, Zoom: 1, Near: 0.1, Far: 50},
				Environment:        scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.8},
				TextAtlases:        []scene.TextAtlas{numbers, plates},
				Graph:              scene.NewGraph(nodes...),
			}
			if backend == "webgl" {
				props.ForceWebGL, props.RequireWebGL = &yes, &yes
			} else {
				props.PreferWebGPU = &yes
			}
			runTextAtlasScene(t, chrome, bootstrap, root, backend, props, numbers)
		})
	}
}

func textAtlasBlankPlate(i int) bool { return i%9 == 4 }

func runTextAtlasScene(t *testing.T, chrome string, bootstrap []byte, root, backend string, props scene.Props, numbers scene.TextAtlas) {
	t.Helper()
	manifest, err := json.Marshal(map[string]any{"engines": []any{map[string]any{
		"id": "text-atlas", "component": "GoSXScene3D", "kind": "surface", "mountId": "atlas-root", "props": props,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manifest, []byte(numbers.Texture())) {
		t.Fatal("the manifest does not carry the number atlas texture URL")
	}
	page := []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#atlas-root{width:%dpx;height:%dpx}</style></head><body><div id="atlas-root"></div><script id="gosx-manifest" type="application/json">%s</script><script src="/bootstrap.js"></script></body></html>`, textAtlasWidth, textAtlasHeight, manifest))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(page)
		case r.URL.Path == "/bootstrap.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write(bootstrap)
		case strings.HasPrefix(r.URL.Path, "/gosx/bootstrap-") && strings.HasSuffix(r.URL.Path, ".js"):
			http.ServeFile(w, r, filepath.Join(root, "client", "js", filepath.Base(r.URL.Path)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	url := server.URL
	if e2eWindowsRelay() {
		url = e2eExposeToWindows(t, server.URL)
	}
	flags := map[string]any{"use-angle": "swiftshader", "enable-unsafe-swiftshader": true}
	if backend == "webgl" {
		flags["disable-features"] = "WebGPU"
	} else {
		flags["enable-unsafe-webgpu"] = true
	}
	browser := newBrowserPage(t, chrome, flags, textAtlasWidth+80, textAtlasHeight+80, "", 180*time.Second)
	browser.navigate(t, url+"/")

	var report textAtlasReport
	var img image.Image
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(300 * time.Millisecond) {
		var state struct {
			Backend string `json:"backend"`
			Ready   string `json:"ready"`
		}
		browser.eval(t, `(() => { const e = document.getElementById('atlas-root'); return { backend: e.getAttribute('data-gosx-scene3d-backend') || '', ready: e.getAttribute('data-gosx-scene3d-ready') || '' }; })()`, &state)
		if state.Backend != backend || state.Ready != "true" {
			continue
		}
		var shot []byte
		if err := chromedp.Run(browser.ctx, chromedp.Screenshot("#atlas-root canvas", &shot, chromedp.ByQuery)); err != nil {
			t.Fatal(err)
		}
		if img, err = png.Decode(bytes.NewReader(shot)); err != nil {
			t.Fatal(err)
		}
		if report = inspectTextAtlasShot(img); report.complete() {
			break
		}
	}
	if img == nil {
		t.Fatalf("the scene never became ready on %s; errors=%v console=%s", backend, browser.PageErrors(), browser.Console())
	}
	if out := os.Getenv("GOSX_TEXT_ATLAS_SHOT"); out != "" {
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		_ = os.WriteFile(strings.ReplaceAll(out, "{backend}", backend), buf.Bytes(), 0o644)
	}
	for i := 0; i < textAtlasPlayers; i++ {
		if report.ink[i] < 0.03 || report.shirt[i] < 0.3 {
			t.Errorf("jersey %d (%s): shirt %.2f, number ink %.2f of the number area", i, numbers.Labels[i], report.shirt[i], report.ink[i])
		}
		plate := report.plates[i]
		if textAtlasBlankPlate(i) {
			if plate.lit > 0.02 {
				t.Errorf("blank plate %d drew: %.2f of its pixels are lit", i, plate.lit)
			}
		} else if plate.fill < 0.5 || plate.text < 0.01 {
			t.Errorf("plate %d: fill %.2f, text %.2f", i, plate.fill, plate.text)
		}
	}
	minDistance, pairA, pairB := 1<<30, 0, 0
	for a := 0; a < textAtlasPlayers; a++ {
		for b := a + 1; b < textAtlasPlayers; b++ {
			d := 0
			for k := range report.masks[a] {
				if report.masks[a][k] != report.masks[b][k] {
					d++
				}
			}
			if d < minDistance {
				minDistance, pairA, pairB = d, a, b
			}
		}
	}
	if minDistance < 4 {
		t.Errorf("jerseys %d (%s) and %d (%s) look alike: their 12x12 ink masks differ in %d cells", pairA, numbers.Labels[pairA], pairB, numbers.Labels[pairB], minDistance)
	}
	if errs := browser.PageErrors(); len(errs) != 0 {
		t.Fatalf("page errors: %v", errs)
	}
	t.Logf("%s: 44 jerseys from one material and one atlas; closest pair %d (%s) and %d (%s) differs in %d of 144 mask cells", backend, pairA, numbers.Labels[pairA], pairB, numbers.Labels[pairB], minDistance)
}

type textAtlasPlate struct{ fill, text, lit float64 }

type textAtlasReport struct {
	shirt, ink []float64
	masks      [][144]bool
	plates     []textAtlasPlate
}

func (r textAtlasReport) complete() bool {
	for i := 0; i < textAtlasPlayers; i++ {
		if r.ink[i] < 0.03 {
			return false
		}
		if p := r.plates[i]; !textAtlasBlankPlate(i) && p.text < 0.01 {
			return false
		}
	}
	return true
}

// inspectTextAtlasShot measures every character in the canvas screenshot.
// World (x, y) maps to canvas pixel ((x+11)*50, (5.6-y)*50).
func inspectTextAtlasShot(img image.Image) textAtlasReport {
	b := img.Bounds()
	scale := float64(b.Dx()) / textAtlasWidth
	at := func(x, y float64) (int, int) {
		return b.Min.X + int((x+11)*textAtlasPPU*scale), b.Min.Y + int((5.6-y)*textAtlasPPU*scale)
	}
	r := textAtlasReport{shirt: make([]float64, textAtlasPlayers), ink: make([]float64, textAtlasPlayers),
		masks: make([][144]bool, textAtlasPlayers), plates: make([]textAtlasPlate, textAtlasPlayers)}
	for i := 0; i < textAtlasPlayers; i++ {
		cx, cy := textAtlasCenter(i)
		// The jersey spans 1.6 x 1.3 units; its number area is UV
		// (0.2, 0.15)-(0.8, 0.85). Measure inside a 2 px inset.
		x0, y0 := at(cx-0.8+0.04, cy+0.65-0.04)
		x1, y1 := at(cx+0.8-0.04, cy-0.65+0.04)
		var blue, total int
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				p := pixel8(img, x, y)
				// The shirt is rgb(0.1, 0.25, 0.7), linear or sRGB-encoded.
				if p[2] > 120 && p[2] > p[0]+60 && p[2] > p[1]+30 {
					blue++
				}
				total++
			}
		}
		r.shirt[i] = float64(blue) / float64(max(total, 1))
		nx0, ny0 := at(cx-0.8+1.6*0.2, cy+0.65-1.3*0.15)
		nx1, ny1 := at(cx-0.8+1.6*0.8, cy+0.65-1.3*0.85)
		var inkCount, area int
		var cells, cellArea [144]int
		for y := ny0; y < ny1; y++ {
			for x := nx0; x < nx1; x++ {
				k := (y-ny0)*12/max(ny1-ny0, 1)*12 + (x-nx0)*12/max(nx1-nx0, 1)
				p := pixel8(img, x, y)
				cellArea[k]++
				area++
				if p[0] > 170 && p[1] > 170 && p[2] > 170 {
					inkCount++
					cells[k]++
				}
			}
		}
		r.ink[i] = float64(inkCount) / float64(max(area, 1))
		for k := range cells {
			r.masks[i][k] = cellArea[k] > 0 && cells[k]*3 > cellArea[k]
		}
		// The plate spans 1.8 x 0.45 units centered 1.05 below the
		// character: a yellow fill with near-black text, or nothing on a
		// blank plate. Measure inside a 5 px inset, clear of the fill's
		// transparent margin.
		px0, py0 := at(cx-0.9+0.1, cy-1.05+0.225-0.1)
		px1, py1 := at(cx+0.9-0.1, cy-1.05-0.225+0.1)
		var fill, dark, lit, plateArea int
		for y := py0; y < py1; y++ {
			for x := px0; x < px1; x++ {
				p := pixel8(img, x, y)
				plateArea++
				switch {
				case p[0] > 110 && p[1] > 90 && p[2] < p[1]:
					fill++
				case p[0]+p[1]+p[2] < 150:
					dark++
				}
				if p[0]+p[1]+p[2] > 30 {
					lit++
				}
			}
		}
		plateN := float64(max(plateArea, 1))
		pr := textAtlasPlate{fill: float64(fill) / plateN, text: float64(dark) / plateN, lit: float64(lit) / plateN}
		r.plates[i] = pr
	}
	return r
}
