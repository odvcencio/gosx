//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"maps"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
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
    param numberRect : vec4 = vec4(0.1, 0.1, 0.9, 0.9)
    surface(geo) -> color {
        let glyph = sample(labels, mlAtlasUV(mlRectLocal(geo.uv, numberRect), labelRect))
        let ink = glyph.a * mlRectMask(geo.uv, numberRect)
        return mix(rgb(0.1, 0.25, 0.7), rgb(1.0, 1.0, 1.0), ink)
    }
}`

const (
	textAtlasPlayers = 44
	textAtlasColumns = 11
	textAtlasWidth   = 1100 // canvas pixels; the camera shows 24 x 12.2 world units, a margin around the 22 x 11.2 grid
	textAtlasHeight  = 560
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
// and checks that every jersey shows its own number (its ink pattern is
// closest to its own cell of the atlas the browser drew, so the 44 numbers on
// screen are 44 different shapes), that the named plates show text on their
// fill and that the blank plates show nothing. The characters are separate
// meshes, not one InstancedMesh, because instanced draws have no
// per-instance UV region. Run on both GPU runners:
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
	// The number cells match the aspect of the shirt's number area (1.44 x
	// 1.04 units), so two-digit numbers shrink less and keep more pixels.
	numbers := scene.TextAtlas{ID: "numbers", CellWidth: 128, CellHeight: 96, Padding: 0.1}
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
	jerseyGeometry := textAtlasQuad(1.8, 1.3)
	head := scene.SphereGeometry{Radius: 0.28, Segments: 16}
	plateMaterial := scene.StandardMaterial{Color: "#ffffff", Texture: plates.Texture(), Roughness: 1, BlendMode: scene.BlendAlpha, Opacity: scene.Float(1)}
	for i := 0; i < textAtlasPlayers; i++ {
		x, y := textAtlasCenter(i)
		shirt := jersey
		shirt.Uniforms = maps.Clone(jersey.Uniforms)
		if shirt.Uniforms == nil {
			shirt.Uniforms = map[string]any{}
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
	// The monolith bootstrap.js carries the text atlas module inline; the split
	// page loads it from its opt-in chunk.
	for _, run := range backends {
		for _, chunked := range []bool{false, true} {
			backend, chunked := run, chunked
			name := backend
			if chunked {
				name += "/split"
			}
			t.Run(name, func(t *testing.T) {
				yes := true
				props := scene.Props{
					Width: textAtlasWidth, Height: textAtlasHeight, Background: "#000000",
					OrthographicCamera: &scene.OrthographicCamera{Position: scene.Vector3{Z: 10}, Left: -12, Right: 12, Top: 6.11, Bottom: -6.11, Zoom: 1, Near: 0.1, Far: 50},
					Environment:        scene.Environment{AmbientColor: "#ffffff", AmbientIntensity: 0.8},
					TextAtlases:        []scene.TextAtlas{numbers, plates},
					Graph:              scene.NewGraph(nodes...),
				}
				if backend == "webgl" {
					props.ForceWebGL, props.RequireWebGL = &yes, &yes
				} else {
					props.PreferWebGPU = &yes
				}
				runTextAtlasScene(t, chrome, bootstrap, root, backend, chunked, props, numbers)
			})
		}
	}
}

func textAtlasBlankPlate(i int) bool { return i%9 == 4 }

func runTextAtlasScene(t *testing.T, chrome string, bootstrap []byte, root, backend string, chunked bool, props scene.Props, numbers scene.TextAtlas) {
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
	if chunked {
		propsJSON, err := json.Marshal(props)
		if err != nil {
			t.Fatal(err)
		}
		page = []byte(fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;background:#000}#atlas-root{width:%dpx;height:%dpx}</style></head><body><div id="atlas-root"></div>
<script src="/gosx/bootstrap-runtime.js"></script>
<script data-gosx-script="feature-scene3d" data-gosx-scene3d-webgl-url="/gosx/bootstrap-feature-scene3d-webgl.js" data-gosx-scene3d-webgpu-url="/gosx/bootstrap-feature-scene3d-webgpu.js" data-gosx-scene3d-command-url="/gosx/bootstrap-feature-scene3d-command.js" data-gosx-scene3d-text-atlas-url="/gosx/bootstrap-feature-scene3d-text-atlas.js" src="/gosx/bootstrap-feature-scene3d.js"></script>
<script>window.__gosx_engine_factories.GoSXScene3D({mount:document.getElementById('atlas-root'),emit(){},props:%s}).catch(error=>{window.mountError=String(error);});</script></body></html>`, textAtlasWidth, textAtlasHeight, propsJSON))
	}
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
	// The GPU runners attach to a browser with its own window size; fix the
	// viewport so the canvas keeps its full size.
	if err := chromedp.Run(browser.ctx, chromedp.EmulateViewport(textAtlasWidth+80, textAtlasHeight+80)); err != nil {
		t.Fatal(err)
	}
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
	atlasChunk := browser.anyRequest(func(url string) bool { return strings.Contains(url, "bootstrap-feature-scene3d-text-atlas") })
	if atlasChunk != chunked {
		t.Fatalf("text atlas chunk requested = %v, want %v (split page: %v)", atlasChunk, chunked, chunked)
	}
	if out := os.Getenv("GOSX_TEXT_ATLAS_SHOT"); out != "" {
		var buf bytes.Buffer
		_ = png.Encode(&buf, img)
		_ = os.WriteFile(strings.ReplaceAll(out, "{backend}", backend), buf.Bytes(), 0o644)
	}
	if report.found != textAtlasPlayers {
		t.Fatalf("found %d jerseys on screen, want %d", report.found, textAtlasPlayers)
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
		} else if plate.fill < 0.5 || plate.text < 0.03 {
			t.Errorf("plate %d: fill %.2f, text %.2f", i, plate.fill, plate.text)
		}
	}
	// Identify each jersey's number: compare the glyph alpha recovered from
	// its pixels with every cell of the atlas the browser drew, scaled to
	// the jersey's number area. Each jersey must be closest to its own
	// label, which also shows the 44 numbers on screen are 44 different
	// shapes.
	var atlasURL string
	browser.eval(t, fmt.Sprintf(`window.__gosx_scene3d_api.sceneTextAtlasURL(%q)`, numbers.Texture()), &atlasURL)
	refs := textAtlasReferenceCells(t, atlasURL, numbers)
	if out := os.Getenv("GOSX_TEXT_ATLAS_SHOT"); out != "" {
		data, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(atlasURL, "data:image/png;base64,"))
		_ = os.WriteFile(strings.ReplaceAll(out, "{backend}", backend+"-atlas"), data, 0o644)
	}
	worstRatio, worst := math.Inf(1), 0
	for i, number := range report.numbers {
		own := textAtlasMatchDistance(number, refs[i].resample(number.w, number.h))
		best, bestJ := math.Inf(1), -1
		for j := range refs {
			if j == i {
				continue
			}
			if d := textAtlasMatchDistance(number, refs[j].resample(number.w, number.h)); d < best {
				best, bestJ = d, j
			}
		}
		if own >= best {
			t.Errorf("jersey %d should show %s but looks closer to %s (mean alpha error %.3f vs %.3f)", i, numbers.Labels[i], numbers.Labels[bestJ], own, best)
		}
		if ratio := best / math.Max(own, 1e-6); ratio < worstRatio {
			worstRatio, worst = ratio, i
		}
	}
	if errs := browser.PageErrors(); len(errs) != 0 {
		t.Fatalf("page errors: %v", errs)
	}
	shape := "monolith"
	if chunked {
		shape = "split page, atlas chunk loaded on demand"
	}
	t.Logf("%s (%s): 44 jerseys share one material and one atlas texture; each matches its own number; the closest call (jersey %d, %s) is %.2fx nearer its own label than any other", backend, shape, worst, numbers.Labels[worst], worstRatio)
}

// textAtlasCell is one atlas cell's alpha, 0..1, row by row.
type textAtlasCell struct {
	w, h  int
	alpha []float64
}

// resample box-filters the cell to w by h pixels.
func (c textAtlasCell) resample(w, h int) []float64 {
	out := make([]float64, w*h)
	for ty := 0; ty < h; ty++ {
		y0, y1 := ty*c.h/h, max((ty+1)*c.h/h, ty*c.h/h+1)
		for tx := 0; tx < w; tx++ {
			x0, x1 := tx*c.w/w, max((tx+1)*c.w/w, tx*c.w/w+1)
			sum := 0.0
			for y := y0; y < y1; y++ {
				for x := x0; x < x1; x++ {
					sum += c.alpha[y*c.w+x]
				}
			}
			out[ty*w+tx] = sum / float64((y1-y0)*(x1-x0))
		}
	}
	return out
}

// textAtlasReferenceCells decodes the atlas image the browser drew and
// returns each label's cell.
func textAtlasReferenceCells(t *testing.T, dataURL string, atlas scene.TextAtlas) []textAtlasCell {
	t.Helper()
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) {
		t.Fatalf("the atlas did not resolve to a PNG data URL: %.60q", dataURL)
	}
	data, err := base64.StdEncoding.DecodeString(dataURL[len(prefix):])
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if w, h := atlas.Size(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("atlas image is %dx%d, want %dx%d", b.Dx(), b.Dy(), w, h)
	}
	out := make([]textAtlasCell, len(atlas.Labels))
	for i := range atlas.Labels {
		r := atlas.Region(i)
		x0, y0 := int(r[0]*float64(b.Dx())), int(r[1]*float64(b.Dy()))
		x1, y1 := int(r[2]*float64(b.Dx())), int(r[3]*float64(b.Dy()))
		cell := textAtlasCell{w: x1 - x0, h: y1 - y0, alpha: make([]float64, (x1-x0)*(y1-y0))}
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				cell.alpha[(y-y0)*cell.w+x-x0] = float64(color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA).A) / 255
			}
		}
		out[i] = cell
	}
	return out
}

// textAtlasNumber is the glyph alpha recovered from a jersey's number area,
// w by h pixels, stored with one extra pixel on every side so the match can
// try the area shifted by a pixel (its edges come from a detected box).
type textAtlasNumber struct {
	w, h  int
	alpha []float64 // (w+2) * (h+2)
}

// textAtlasMatchDistance is the mean absolute alpha error between a jersey's
// number and a reference resampled to its size, at the best of the nine
// one-pixel shifts.
func textAtlasMatchDistance(n textAtlasNumber, ref []float64) float64 {
	best := math.Inf(1)
	for dy := 0; dy <= 2; dy++ {
		for dx := 0; dx <= 2; dx++ {
			sum := 0.0
			for y := 0; y < n.h; y++ {
				row := n.alpha[(y+dy)*(n.w+2)+dx:]
				for x := 0; x < n.w; x++ {
					sum += math.Abs(row[x] - ref[y*n.w+x])
				}
			}
			best = math.Min(best, sum/float64(max(n.w*n.h, 1)))
		}
	}
	return best
}

type textAtlasPlate struct{ fill, text, lit float64 }

type textAtlasReport struct {
	shirt, ink []float64
	numbers    []textAtlasNumber
	found      int // jerseys found on screen
	plates     []textAtlasPlate
}

func (r textAtlasReport) complete() bool {
	for i := 0; i < textAtlasPlayers; i++ {
		if r.ink[i] < 0.03 {
			return false
		}
		if p := r.plates[i]; !textAtlasBlankPlate(i) && p.text < 0.03 {
			return false
		}
	}
	return true
}

// inspectTextAtlasShot measures every character in the canvas screenshot. It
// finds the jerseys as the 44 connected regions of shirt-blue pixels, orders
// them by row and column, then places the number area and the nameplate
// relative to each jersey's box, so the check does not depend on the
// camera's exact pixel mapping.
func inspectTextAtlasShot(img image.Image) textAtlasReport {
	r := textAtlasReport{shirt: make([]float64, textAtlasPlayers), ink: make([]float64, textAtlasPlayers),
		numbers: make([]textAtlasNumber, textAtlasPlayers), plates: make([]textAtlasPlate, textAtlasPlayers)}
	boxes := textAtlasJerseyBoxes(img)
	if r.found = len(boxes); r.found != textAtlasPlayers {
		return r
	}
	for i, jersey := range boxes {
		jx0, jy0 := jersey.Min.X, jersey.Min.Y
		// The jersey is 1.8 x 1.3 units; box fractions map onto it.
		jw, jh := float64(jersey.Dx()), float64(jersey.Dy())
		box := func(fx0, fy0, fx1, fy1 float64) (int, int, int, int) {
			return jx0 + int(fx0*jw), jy0 + int(fy0*jh), jx0 + int(fx1*jw), jy0 + int(fy1*jh)
		}
		var blue, total int
		var shirtRG float64 // mean red plus green of the shirt pixels
		x0, y0, x1, y1 := box(0.03, 0.03, 0.97, 0.97)
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				if p := pixel8(img, x, y); textAtlasIsShirt(p) {
					blue++
					shirtRG += float64(p[0] + p[1])
				}
				total++
			}
		}
		r.shirt[i] = float64(blue) / float64(max(total, 1))
		shirtRG /= float64(max(blue, 1))
		// The number area is the shirt's UV rect numberRect. The shader
		// mixes shirt and white by the glyph's alpha, so red plus green
		// recovers that alpha per pixel.
		nx0, ny0, nx1, ny1 := box(0.1, 0.1, 0.9, 0.9)
		number := textAtlasNumber{w: nx1 - nx0, h: ny1 - ny0, alpha: make([]float64, (nx1-nx0+2)*(ny1-ny0+2))}
		var inkCount int
		for y := -1; y <= number.h; y++ {
			for x := -1; x <= number.w; x++ {
				p := pixel8(img, nx0+x, ny0+y)
				number.alpha[(y+1)*(number.w+2)+x+1] = math.Max(0, math.Min(1, (float64(p[0]+p[1])-shirtRG)/(510-shirtRG)))
				if x >= 0 && y >= 0 && x < number.w && y < number.h && p[0] > 170 && p[1] > 170 && p[2] > 170 {
					inkCount++
				}
			}
		}
		r.numbers[i] = number
		r.ink[i] = float64(inkCount) / float64(max(number.w*number.h, 1))
		// The plate is 1.8 x 0.45 units, centered 1.05 units below the
		// jersey's center: a yellow fill with near-black text, or nothing on
		// a blank plate. Measure 0.1 units inside its edges, clear of the
		// fill's transparent margin: x -0.8..0.8 and y -0.925..-1.175 from
		// the jersey center, which is 0.65 units below the jersey's top.
		px0, py0, px1, py1 := box(0.1/1.8, (0.65+0.925)/1.3, 1.7/1.8, (0.65+1.175)/1.3)
		var fill, dark, lit, plateArea int
		for y := py0; y < min(py1, img.Bounds().Max.Y); y++ {
			for x := px0; x < px1; x++ {
				p := pixel8(img, x, y)
				plateArea++
				// Bright yellow is fill; anything darker inside the
				// plate is text or its anti-aliased edge.
				if p[0] > 110 && p[1] > 90 && p[2] < p[1] && p[0]+p[1]+p[2] > 400 {
					fill++
				} else {
					dark++
				}
				if p[0]+p[1]+p[2] > 30 {
					lit++
				}
			}
		}
		plateN := float64(max(plateArea, 1))
		r.plates[i] = textAtlasPlate{fill: float64(fill) / plateN, text: float64(dark) / plateN, lit: float64(lit) / plateN}
	}
	return r
}

// textAtlasIsShirt reports the jersey color, rgb(0.1, 0.25, 0.7), linear or
// sRGB-encoded.
func textAtlasIsShirt(p [3]int) bool { return p[2] > 120 && p[2] > p[0]+60 && p[2] > p[1]+30 }

// textAtlasJerseyBoxes returns the bounding boxes of the shirt-blue regions
// larger than 100 pixels that no other region's box contains, in reading
// order: four rows of 11, top to bottom and left to right.
func textAtlasJerseyBoxes(img image.Image) []image.Rectangle {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	seen := make([]bool, w*h)
	var boxes []image.Rectangle
	var queue []int
	for start := range seen {
		if seen[start] || !textAtlasIsShirt(pixel8(img, b.Min.X+start%w, b.Min.Y+start/w)) {
			continue
		}
		seen[start] = true
		queue = append(queue[:0], start)
		box, n := image.Rect(start%w, start/w, start%w+1, start/w+1), 0
		for len(queue) > 0 {
			at := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			x, y := at%w, at/w
			box, n = box.Union(image.Rect(x, y, x+1, y+1)), n+1
			for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := x+d[0], y+d[1]
				if nx < 0 || ny < 0 || nx >= w || ny >= h || seen[ny*w+nx] {
					continue
				}
				if textAtlasIsShirt(pixel8(img, b.Min.X+nx, b.Min.Y+ny)) {
					seen[ny*w+nx] = true
					queue = append(queue, ny*w+nx)
				}
			}
		}
		if n > 100 {
			boxes = append(boxes, box.Add(b.Min))
		}
	}
	// Shirt showing through a digit's loop (0, 4, 6, 8, 9) is its own
	// region inside a jersey's box; drop those.
	jerseys := boxes[:0]
	for i, inner := range boxes {
		nested := false
		for j, outer := range boxes {
			if i != j && inner.In(outer) && inner != outer {
				nested = true
				break
			}
		}
		if !nested {
			jerseys = append(jerseys, inner)
		}
	}
	boxes = jerseys
	if len(boxes) != textAtlasPlayers {
		return boxes
	}
	center := func(r image.Rectangle) (int, int) { return r.Min.X + r.Dx()/2, r.Min.Y + r.Dy()/2 }
	sort.Slice(boxes, func(i, j int) bool { _, yi := center(boxes[i]); _, yj := center(boxes[j]); return yi < yj })
	for row := 0; row < textAtlasPlayers; row += textAtlasColumns {
		line := boxes[row : row+textAtlasColumns]
		sort.Slice(line, func(i, j int) bool { xi, _ := center(line[i]); xj, _ := center(line[j]); return xi < xj })
	}
	return boxes
}
