package beachgen

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

const generatorSeed int64 = 0xB1AC6A55

type vec3 struct{ x, y, z float64 }
type geometry struct {
	positions []float64
	normals   []float64
	uvs       []float64
	indices   []uint16
}

// Generate builds all Blackglass Beach assets using deterministic code and the
// supplied seed. The returned map is independent of the output path.
func Generate(seed int64) (map[string][]byte, error) {
	noise := newNoise(seed)
	albedo, err := makeAlbedo(seed, noise)
	if err != nil {
		return nil, err
	}
	mr, err := makeMetalRoughness(noise)
	if err != nil {
		return nil, err
	}
	normal, err := makeSandNormal()
	if err != nil {
		return nil, err
	}
	rockNormal, err := makeRockNormal(seed)
	if err != nil {
		return nil, err
	}
	beach, err := terrainGeometry(noise)
	if err != nil {
		return nil, err
	}
	beachGLB, err := writeGLB(beach, terrainMaterial(), []embeddedImage{{name: "albedo", data: albedo, mime: "image/jpeg"}, {name: "metallic-roughness", data: mr}, {name: "sand-normal", data: normal, mime: "image/jpeg"}})
	if err != nil {
		return nil, fmt.Errorf("beach GLB: %w", err)
	}
	stacks, err := stackGeometry(seed)
	if err != nil {
		return nil, err
	}
	stacksGLB, err := writeGLB(stacks, rockMaterial(), []embeddedImage{{name: "rock-normal", data: rockNormal, mime: "image/jpeg"}})
	if err != nil {
		return nil, fmt.Errorf("stacks GLB: %w", err)
	}
	monolith, err := monolithGeometry()
	if err != nil {
		return nil, err
	}
	monolithGLB, err := writeGLB(monolith, solidMaterial([4]float64{srgbLinear(12.0 / 255), srgbLinear(13.0 / 255), srgbLinear(16.0 / 255), 1}, .05, 0), nil)
	if err != nil {
		return nil, fmt.Errorf("monolith GLB: %w", err)
	}
	height, err := makeBathymetry(noise)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{
		"beach-v2-height.png": height,
		"beach-v2.glb":        beachGLB,
		"beach-v2-albedo.jpg": albedo,
		"beach-v2-mr.png":     mr,
		"sand-normal.jpg":     normal,
		"rock-normal.jpg":     rockNormal,
		"stacks-v2.glb":       stacksGLB,
		"monolith-v2.glb":     monolithGLB,
	}, nil
}

// Write writes the generated files into outDir.
func Write(outDir string, seed int64) error {
	files, err := Generate(seed)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"beach-v2.glb", "beach-v2-albedo.jpg", "beach-v2-mr.png", "beach-v2-height.png", "sand-normal.jpg", "rock-normal.jpg", "stacks-v2.glb", "monolith-v2.glb"} {
		if err := os.WriteFile(filepath.Join(outDir, name), files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// TerrainHeight evaluates the deterministic terrain surface at x,z.
func TerrainHeight(x, z float64, seed int64) float64 { return terrainHeight(newNoise(seed), x, z) }

type cliffPoint struct{ x, z float64 }
type cliffSpec struct {
	front  [3]cliffPoint
	height float64
	seed   int64
	face   float64
}

func terrainHeight(n noiseField, x, z float64) float64 {
	var h float64
	switch {
	case z < 0:
		h = math.Max(-6, .12*z)
	case z <= 18:
		h = .045*z + .35*smoothstep(6, 18, z)
	default:
		h = 1.16 + 2.34*smoothstep(18, 50, z) + 1.2*n.fbm(x/12, z/12, 4)
	}
	sand := n.fbm(x*.17+19, z*.17-7, 4) * .08
	h += sand
	zGate := smoothstep(-40, -30, z)
	leftFront := [3]cliffPoint{{-28, -30}, {-30, 10}, {-36, 50}}
	leftDistance := cliffFrontX(leftFront, z) - x
	leftTarget := lerp(h, 1.5, smoothstep(-6, 0, leftDistance))
	if leftDistance > 0 {
		leftTarget = lerp(1.5, headlandHeight(n, x, z, true), smoothstep(20, 30, leftDistance))
	}
	h = lerp(h, leftTarget, zGate)
	rightFront := [3]cliffPoint{{34, -30}, {35, 12}, {40, 50}}
	rightDistance := x - cliffFrontX(rightFront, z)
	rightTarget := lerp(h, 1.5, smoothstep(-6, 0, rightDistance))
	if rightDistance > 0 {
		rightTarget = lerp(1.5, headlandHeight(n, x, z, false), smoothstep(20, 30, rightDistance))
	}
	h = lerp(h, rightTarget, zGate)
	return h
}

func headlandHeight(n noiseField, x, z float64, left bool) float64 {
	base, offsetX, offsetZ := 18.0, 47.0, -11.0
	if !left {
		base, offsetX, offsetZ = 12, -31, 23
	}
	value := n.fbm(x/4.8+offsetX, z/7.2+offsetZ, 5)
	return base + (1-2*math.Abs(value))*3
}

func cliffFrontX(front [3]cliffPoint, z float64) float64 {
	for segment := 0; segment < 2; segment++ {
		a, b := front[segment], front[segment+1]
		if z <= b.z {
			return lerp(a.x, b.x, clamp((z-a.z)/(b.z-a.z), 0, 1))
		}
	}
	return front[2].x
}

func terrainGeometry(n noiseField) (*geometry, error) {
	const columns, rows = 129, 129
	g := &geometry{positions: make([]float64, 0, columns*rows*3), normals: make([]float64, 0, columns*rows*3), uvs: make([]float64, 0, columns*rows*2), indices: make([]uint16, 0, (columns-1)*(rows-1)*6)}
	zs := make([]float64, rows)
	// Half of the rows cover the shoreline and foreground (z in [-12, 22]).
	far, near := rows/4, rows/2
	for row := 0; row < rows; row++ {
		switch {
		case row < far:
			zs[row] = lerp(-40, -12, float64(row)/float64(far))
		case row < far+near:
			zs[row] = lerp(-12, 22, float64(row-far)/float64(near))
		default:
			zs[row] = lerp(22, 50, float64(row-far-near)/float64(rows-1-far-near))
		}
	}
	for _, z := range zs {
		for column := 0; column < columns; column++ {
			x := -60 + 120*float64(column)/float64(columns-1)
			y := terrainHeight(n, x, z)
			dx := (terrainHeight(n, x+.2, z) - terrainHeight(n, x-.2, z)) / .4
			dz := (terrainHeight(n, x, z+.2) - terrainHeight(n, x, z-.2)) / .4
			normal := normalize(vec3{-dx, 1, -dz})
			g.positions = append(g.positions, x, y, z)
			g.normals = append(g.normals, normal.x, normal.y, normal.z)
			g.uvs = append(g.uvs, (x+60)/120, (z+40)/90)
		}
	}
	for row := 0; row < rows-1; row++ {
		for column := 0; column < columns-1; column++ {
			a := uint16(row*columns + column)
			b := a + 1
			d := uint16((row+1)*columns + column)
			e := d + 1
			g.indices = append(g.indices, a, d, b, b, d, e)
		}
	}
	return g, nil
}

func makeAlbedo(seed int64, n noiseField) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for py := 0; py < 1024; py++ {
		z := -40 + 90*float64(py)/1023
		for px := 0; px < 1024; px++ {
			x := -60 + 120*float64(px)/1023
			h := terrainHeight(n, x, z)
			dx := terrainHeight(n, x+.5, z) - terrainHeight(n, x-.5, z)
			dz := terrainHeight(n, x, z+.5) - terrainHeight(n, x, z-.5)
			leftDistance := cliffFrontX([3]cliffPoint{{-28, -30}, {-30, 10}, {-36, 50}}, z) - x
			rightDistance := x - cliffFrontX([3]cliffPoint{{34, -30}, {35, 12}, {40, 50}}, z)
			headland := (leftDistance > -2 || rightDistance > -2) && z > -35
			rock := math.Hypot(dx, dz) > .6 || headland
			ao := ambientOcclusion(n, x, z, h)
			factor := .45 + .55*ao
			base := [3]float64{62, 58, 54}
			if rock {
				base = [3]float64{40, 42, 45}
			} else {
				mottle := n.fbm(x/3+float64(seed%19), z/3-float64(seed%23), 3) * .10
				streaks := n.fbm(x/9+float64(seed%7), z*1.4, 3) * .08 // wind streaks run across the beach
				grain := n.value(x*18+float64(seed%19), z*18-float64(seed%23)) * .10
				factor *= 1 + mottle + streaks + grain
				// Swash marks: thin light lines of shell grit left along the
				// contours the surge reached, broken up by noise.
				if h > .25 && h < 1.0 && z < 10 {
					phase := h*38 + 2.2*n.fbm(x/6, z/6, 2)
					line := math.Pow(math.Max(0, math.Cos(phase)), 24) * smoothstep(.2, .6, n.fbm(x/2.5+3, z/2.5, 2))
					factor *= 1 + .45*line
				}
				// Wet sand is darker; its roughness band makes it mirror the sky.
				wet := smoothstep(.5, .2, h)
				if z < 4 {
					factor *= 1 - .55*wet
				}
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(base[0] * factor)),
				G: uint8(math.Round(base[1] * factor)),
				B: uint8(math.Round(base[2] * factor)), A: 255,
			})
		}
	}
	// Noisy sand compresses poorly as PNG; JPEG keeps the GLB small.
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func ambientOcclusion(n noiseField, x, z, origin float64) float64 {
	directions := [8][2]float64{{1, 0}, {.7071067811865476, .7071067811865476}, {0, 1}, {-.7071067811865476, .7071067811865476}, {-1, 0}, {-.7071067811865476, -.7071067811865476}, {0, -1}, {.7071067811865476, -.7071067811865476}}
	maxAngle := 0.0
	for _, direction := range directions {
		for _, distance := range [...]float64{1.5, 3.5, 6} {
			rise := terrainHeight(n, x+direction[0]*distance, z+direction[1]*distance) - origin
			angle := math.Atan2(rise, distance)
			if angle > maxAngle {
				maxAngle = angle
			}
		}
	}
	return clamp(1-.55*maxAngle/1.1, .45, 1)
}

func makeMetalRoughness(n noiseField) ([]byte, error) {
	palette := make(color.Palette, 16)
	for value := range palette {
		palette[value] = color.RGBA{R: 0, G: uint8(value * 17), B: 0, A: 255}
	}
	img := image.NewPaletted(image.Rect(0, 0, 256, 256), palette)
	for py := 0; py < 256; py++ {
		z := -40 + 90*float64(py)/255
		for px := 0; px < 256; px++ {
			x := -60 + 120*float64(px)/255
			h := terrainHeight(n, x, z)
			dx := terrainHeight(n, x+.5, z) - terrainHeight(n, x-.5, z)
			dz := terrainHeight(n, x, z+.5) - terrainHeight(n, x, z-.5)
			slope := math.Hypot(dx, dz)
			roughness := .8
			if slope > .6 || (math.Abs(x) > 34 && h > 2) {
				roughness = .70
			} else if h < -.2 {
				roughness = .25
			} else if z < 4 && h > -.2 && h < .45 {
				roughness = .1 + .7*smoothstep(-.2, .45, h)
			}
			img.SetColorIndex(px, py, uint8(math.Round(clamp(roughness, 0, 1)*15)))
		}
	}
	return encodePNG(img)
}

func makeSandNormal() ([]byte, error) {
	// Full-precision tangent-space normals: a palette quantizes the ripple
	// slopes away and the sand renders flat.
	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	const tileMeters = 2.5
	const strength = 5.0
	for y := 0; y < 512; y++ {
		v := float64(y) / 512
		for x := 0; x < 512; x++ {
			u := float64(x) / 512
			du := 1.0 / 512
			dhdu := (rippleHeight(u+du, v) - rippleHeight(u-du, v)) / (2 * du * tileMeters)
			dhdv := (rippleHeight(u, v+du) - rippleHeight(u, v-du)) / (2 * du * tileMeters)
			n := normalize(vec3{-dhdu * strength, -dhdv * strength, 1})
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(math.Round((n.x*.5 + .5) * 255)), G: uint8(math.Round((n.y*.5 + .5) * 255)), B: uint8(math.Round((n.z*.5 + .5) * 255)), A: 255})
		}
	}
	return encodeJPEG(img, 92)
}

func periodicRockHeight(n noiseField, u, v float64) float64 {
	total, weight, amplitude := 0.0, 0.0, 1.0
	for octave := 0; octave < 5; octave++ {
		frequency := math.Exp2(float64(octave))
		angleU, angleV := 2*math.Pi*u, 2*math.Pi*v
		x := frequency * (math.Cos(angleU) + .73*math.Cos(angleV))
		y := frequency * (math.Sin(angleU) + .73*math.Sin(angleV))
		value := 1 - math.Abs(n.value(x, y))
		total += amplitude * value
		weight += amplitude
		amplitude *= .5
	}
	return total/weight - .5
}

func makeRockNormal(seed int64) ([]byte, error) {
	const size = 256
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	n := newNoise(seed + 0x70C)
	const du = 1.0 / size
	for y := 0; y < size; y++ {
		v := float64(y) / size
		for x := 0; x < size; x++ {
			u := float64(x) / size
			dhdu := (periodicRockHeight(n, u+du, v) - periodicRockHeight(n, u-du, v)) / (2 * du) * .025
			dhdv := (periodicRockHeight(n, u, v+du) - periodicRockHeight(n, u, v-du)) / (2 * du) * .025
			normal := normalize(vec3{-dhdu * 1.5, -dhdv * 1.5, 1})
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(math.Round((normal.x*.5 + .5) * 255)), G: uint8(math.Round((normal.y*.5 + .5) * 255)), B: uint8(math.Round((normal.z*.5 + .5) * 255)), A: 255})
		}
	}
	return encodeJPEG(img, 92)
}
func rippleHeight(u, v float64) float64 {
	warp := .045*math.Sin(2*math.Pi*3*u)*math.Sin(2*math.Pi*2*v) + .025*math.Sin(2*math.Pi*(5*u+4*v))
	phase := 2 * math.Pi * 9 * (v + warp)
	grain := .00018*math.Sin(2*math.Pi*(37*u+19*v)) + .00012*math.Sin(2*math.Pi*(23*u-41*v))
	return .00224*math.Sin(phase) + grain
}

// encodeJPEG keeps noisy texture maps small; quality 92 hides block
// artifacts in tangent-space normals of sand and rock.
func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodePNG(img image.Image) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buffer, img); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func terrainMaterial() map[string]any {
	transform := map[string]any{"scale": []float64{60, 45}}
	return map[string]any{
		"name": "Black volcanic sand",
		"pbrMetallicRoughness": map[string]any{
			"baseColorTexture":         map[string]any{"index": 0},
			"metallicRoughnessTexture": map[string]any{"index": 1},
			"metallicFactor":           0.0, // sand is a dielectric; the paletted MR map repeats roughness in B
			"roughnessFactor":          1.0,
		},
		"normalTexture": map[string]any{"index": 2, "scale": .6, "extensions": map[string]any{"KHR_texture_transform": transform}},
	}
}

func rockMaterial() map[string]any {
	return map[string]any{
		"name": "Weathered basalt",
		"pbrMetallicRoughness": map[string]any{
			"baseColorFactor": []float64{srgbLinear(28.0 / 255), srgbLinear(29.0 / 255), srgbLinear(31.0 / 255), 1},
			"metallicFactor":  0,
			"roughnessFactor": .85,
		},
		"normalTexture": map[string]any{"index": 0, "scale": .9, "extensions": map[string]any{"KHR_texture_transform": map[string]any{"scale": []float64{3, 3}}}},
	}
}

func solidMaterial(base [4]float64, roughness, metalness float64) map[string]any {
	return map[string]any{"pbrMetallicRoughness": map[string]any{"baseColorFactor": base[:], "metallicFactor": metalness, "roughnessFactor": roughness}}
}

func stackGeometry(seed int64) (*geometry, error) {
	g := &geometry{}
	for _, stack := range stackSpecs(seed) {
		addStack(g, stack)
	}
	for _, cliff := range cliffSpecs(seed) {
		addCliffWall(g, cliff)
	}
	for _, boulder := range boulderSpecs(seed) {
		addBoulder(g, boulder, newNoise(seed+int64(boulder.id)*101), newNoise(seed))
	}
	averageVertexNormals(g)
	return g, nil
}

func cliffSpecs(seed int64) []cliffSpec {
	return []cliffSpec{
		{front: [3]cliffPoint{{-28, -30}, {-30, 10}, {-36, 50}}, height: 18, seed: seed + 0xC11F, face: 1},
		{front: [3]cliffPoint{{34, -30}, {35, 12}, {40, 50}}, height: 12, seed: seed + 0xC12F, face: -1},
	}
}

type stackSpec struct {
	x, z, height, radius, lean float64
	seed                       int64
}

type boulderSpec struct {
	x, z, radius float64
	id           int
}

func stackSpecs(seed int64) []stackSpec {
	values := []struct{ x, z, height, radius float64 }{
		{-16, -32, 18, 5}, {-10, -29, 6, 2}, {-21, -36, 9, 2.6},
		{11, -46, 12, 4}, {26, -22, 2.5, 6},
	}
	rng := newRandom(seed + 0x51ac)
	out := make([]stackSpec, len(values))
	for i, value := range values {
		lean := (rng.float64()*2 - 1) * math.Tan(4*math.Pi/180)
		out[i] = stackSpec{value.x, value.z, value.height, value.radius, lean, seed + int64(i+1)*7919}
	}
	return out
}

func boulderSpecs(seed int64) []boulderSpec {
	rng := newRandom(seed + 0xB01D3)
	out := make([]boulderSpec, 0, 14)
	for attempt := 0; len(out) < 14 && attempt < 10000; attempt++ {
		x := -18 + rng.float64()*36
		z := -7 + rng.float64()*9.5
		if math.Hypot(x+6.2, z-2.6) < 3 {
			continue
		}
		spaced := true
		for _, other := range out {
			if math.Hypot(x-other.x, z-other.z) < 1.5 {
				spaced = false
				break
			}
		}
		if !spaced {
			continue
		}
		out = append(out, boulderSpec{x, z, .3 + rng.float64(), len(out)})
	}
	return out
}

func noiseLattice3(n noiseField, x, y, z int64) float64 {
	h := n.seed ^ uint64(x)*0x9e3779b97f4a7c15 ^ uint64(y)*0xbf58476d1ce4e5b9 ^ uint64(z)*0x94d049bb133111eb
	h += 0x9e3779b97f4a7c15
	h = (h ^ (h >> 30)) * 0xbf58476d1ce4e5b9
	h = (h ^ (h >> 27)) * 0x94d049bb133111eb
	h ^= h >> 31
	return float64(h>>11)/float64(uint64(1)<<53)*2 - 1
}

func noiseValue3(n noiseField, x, y, z float64) float64 {
	x0, y0, z0 := int64(math.Floor(x)), int64(math.Floor(y)), int64(math.Floor(z))
	tx, ty, tz := fade(x-float64(x0)), fade(y-float64(y0)), fade(z-float64(z0))
	zLow := lerp(
		lerp(noiseLattice3(n, x0, y0, z0), noiseLattice3(n, x0+1, y0, z0), tx),
		lerp(noiseLattice3(n, x0, y0+1, z0), noiseLattice3(n, x0+1, y0+1, z0), tx), ty,
	)
	zHigh := lerp(
		lerp(noiseLattice3(n, x0, y0, z0+1), noiseLattice3(n, x0+1, y0, z0+1), tx),
		lerp(noiseLattice3(n, x0, y0+1, z0+1), noiseLattice3(n, x0+1, y0+1, z0+1), tx), ty,
	)
	return lerp(zLow, zHigh, tz)
}

// fbm3 sums octaves of 3D value noise, roughly in [-1, 1].
func fbm3(n noiseField, x, y, z float64, octaves int) float64 {
	amplitude, frequency, total, weight := 1.0, 1.0, 0.0, 0.0
	for octave := 0; octave < octaves; octave++ {
		total += amplitude * noiseValue3(n, x*frequency, y*frequency, z*frequency)
		weight += amplitude
		amplitude *= .5
		frequency *= 2.03
	}
	return total / weight
}

func ridgeFbm3(n noiseField, x, y, z float64, octaves int) float64 {
	amplitude, frequency, total, weight := 1.0, 1.0, 0.0, 0.0
	for octave := 0; octave < octaves; octave++ {
		value := noiseValue3(n, x*frequency, y*frequency, z*frequency)
		ridge := 1 - math.Abs(value)
		total += amplitude * ridge
		weight += amplitude
		amplitude *= .5
		frequency *= 2
	}
	if weight == 0 {
		return 0
	}
	return total/weight - .5
}

func appendVertex(g *geometry, p vec3, u, v float64) uint16 {
	index := uint16(len(g.positions) / 3)
	g.positions = append(g.positions, p.x, p.y, p.z)
	g.normals = append(g.normals, 0, 0, 0)
	g.uvs = append(g.uvs, u, v)
	return index
}

func addStack(g *geometry, stack stackSpec) {
	const sides, rings = 40, 52
	noise := newNoise(stack.seed)
	baseY, topY := -6.0, stack.height
	first := len(g.positions) / 3
	for row := 0; row <= rings; row++ {
		t := float64(row) / rings
		y := lerp(baseY, topY, t)
		domeStart := topY - math.Min((topY-baseY)*.16, stack.radius*.45)
		dome := smoothstep(domeStart, topY, y)
		profile := 1 - .45*dome
		for side := 0; side < sides; side++ {
			angle := 2 * math.Pi * float64(side) / sides
			dx, dz := math.Cos(angle), math.Sin(angle)
			// A jagged crown: notch the top rows by noise around the circumference.
			worldY := y - dome*stack.radius*.35*math.Abs(noiseValue3(noise, dx*1.7+stack.x, 3.3, dz*1.7+stack.z))
			// Weathered basalt: broad lumps, sharper ridges, vertical fissures
			// (noise stretched along Y), a gentle taper and faint irregular strata.
			px, pz := dx*stack.radius, dz*stack.radius
			r := stack.radius
			lumps := fbm3(noise, px/(r*1.3), worldY/(r*1.1), pz/(r*1.3), 4)
			ridges := ridgeFbm3(noise, px/(r*.45)+5, worldY/(r*.45), pz/(r*.45)+9, 4)
			cracks := fbm3(noise, px/(r*.22)+11, worldY*.07, pz/(r*.22)+7, 3)
			strata := .03 * math.Sin(worldY*1.1+2.5*noiseValue3(noise, px*.1, worldY*.05, pz*.1))
			taper := 1 - .22*(worldY-baseY)/(topY-baseY)
			// Bulges and waists that change with height break the bottle outline.
			girth := noiseValue3(noise, stack.x*.31, worldY/(r*.9), stack.z*.31) + .5*noiseValue3(noise, stack.x*.7, worldY/(r*.4), stack.z*.7)
			radius := r * profile * taper * (1 + .22*girth + .32*lumps + .16*ridges - .14*math.Abs(cracks) + strata)
			notch := smoothstep(-1, -.6, worldY) * (1 - smoothstep(1.2, 1.6, worldY))
			radius *= 1 - .18*notch
			leanFactor := (worldY - baseY) * stack.lean
			p := vec3{stack.x + dx*radius + leanFactor, worldY, stack.z + dz*radius}
			appendVertex(g, p, angle/(2*math.Pi), worldY/8-math.Floor(worldY/8))
		}
	}
	for row := 0; row < rings; row++ {
		for side := 0; side < sides; side++ {
			a := uint16(first + row*sides + side)
			b := uint16(first + row*sides + (side+1)%sides)
			c := uint16(first + (row+1)*sides + (side+1)%sides)
			d := uint16(first + (row+1)*sides + side)
			g.indices = append(g.indices, a, d, c, a, c, b)
		}
	}
	bottom := appendVertex(g, vec3{stack.x, baseY, stack.z}, .5, baseY/8-math.Floor(baseY/8))
	top := appendVertex(g, vec3{stack.x + (topY-baseY)*stack.lean, topY, stack.z}, .5, topY/8-math.Floor(topY/8))
	for side := 0; side < sides; side++ {
		next := (side + 1) % sides
		firstRing := uint16(first + side)
		secondRing := uint16(first + next)
		lastRing := uint16(first + rings*sides + side)
		nextLast := uint16(first + rings*sides + next)
		g.indices = append(g.indices, bottom, firstRing, secondRing)
		g.indices = append(g.indices, top, nextLast, lastRing)
	}
}

func addBoulder(g *geometry, boulder boulderSpec, noise, terrainNoise noiseField) {
	const sides, rings = 16, 12
	ground := terrainHeight(terrainNoise, boulder.x, boulder.z)
	center := vec3{boulder.x, ground + boulder.radius*.3, boulder.z}
	first := len(g.positions) / 3
	for row := 0; row <= rings; row++ {
		latitude := math.Pi * float64(row) / rings
		for side := 0; side < sides; side++ {
			longitude := 2 * math.Pi * float64(side) / sides
			nx := math.Sin(latitude) * math.Cos(longitude)
			ny := math.Cos(latitude)
			nz := math.Sin(latitude) * math.Sin(longitude)
			lumps := fbm3(noise, nx*1.6+boulder.x, ny*1.6, nz*1.6+boulder.z, 4)
			ridges := ridgeFbm3(noise, nx*4, ny*4, nz*4, 4)
			radius := boulder.radius * (1 + .22*lumps + .07*ridges)
			p := vec3{center.x + nx*radius, center.y + ny*radius*.72, center.z + nz*radius}
			appendVertex(g, p, longitude/(2*math.Pi), latitude/math.Pi)
		}
	}
	for row := 0; row < rings; row++ {
		for side := 0; side < sides; side++ {
			a := uint16(first + row*sides + side)
			b := uint16(first + row*sides + (side+1)%sides)
			c := uint16(first + (row+1)*sides + (side+1)%sides)
			d := uint16(first + (row+1)*sides + side)
			g.indices = append(g.indices, a, c, d, a, b, c)
		}
	}
}

func averageVertexNormals(g *geometry) {
	for i := 0; i+2 < len(g.indices); i += 3 {
		a, b, c := int(g.indices[i])*3, int(g.indices[i+1])*3, int(g.indices[i+2])*3
		pa := vec3{g.positions[a], g.positions[a+1], g.positions[a+2]}
		pb := vec3{g.positions[b], g.positions[b+1], g.positions[b+2]}
		pc := vec3{g.positions[c], g.positions[c+1], g.positions[c+2]}
		normal := cross(sub(pb, pa), sub(pc, pa))
		for _, index := range [...]int{a, b, c} {
			g.normals[index] += normal.x
			g.normals[index+1] += normal.y
			g.normals[index+2] += normal.z
		}
	}
	for i := 0; i < len(g.normals); i += 3 {
		normal := normalize(vec3{g.normals[i], g.normals[i+1], g.normals[i+2]})
		g.normals[i], g.normals[i+1], g.normals[i+2] = normal.x, normal.y, normal.z
	}
}

// monolithGeometry is a knapped obsidian shard: six jittered rings of seven
// points narrowing to an off-centre apex, flat-shaded so every facet
// catches the sky and sun at its own angle (conchoidal glass, not a box).
func monolithGeometry() (*geometry, error) {
	g := &geometry{}
	rng := newRandom(0x0B51D1A)
	const sides = 7
	ringY := []float64{0, .5, 1.25, 2.05, 2.7, 3.05}
	ringR := []float64{.72, .78, .7, .56, .38, .2}
	rings := make([][]vec3, len(ringY))
	for r := range ringY {
		twist := float64(r) * .31
		for k := 0; k < sides; k++ {
			a := 2*math.Pi*float64(k)/sides + twist + (rng.float64()-.5)*.35
			rad := ringR[r] * (0.78 + .44*rng.float64())
			y := ringY[r]
			if r > 0 {
				y += (rng.float64() - .5) * .22
			}
			// Flattened section: the shard is a blade, wider than it is deep.
			rings[r] = append(rings[r], vec3{math.Cos(a) * rad, y, math.Sin(a) * rad * .45})
		}
	}
	apex := vec3{.12, 3.42, -.04}
	tri := func(a, b, c vec3) {
		n := normalize(cross(sub(b, a), sub(c, a)))
		centre := scaleVec(vec3{a.x + b.x + c.x, 0, a.z + b.z + c.z}, 1.0/3)
		if dot(n, centre) < 0 && n.y < .9 { // keep side facets facing outward
			n = scaleVec(n, -1)
			b, c = c, b
		}
		addTriangle(g, a, b, c, n)
	}
	for r := 0; r+1 < len(rings); r++ {
		for k := 0; k < sides; k++ {
			a, b := rings[r][k], rings[r][(k+1)%sides]
			c, d := rings[r+1][(k+1)%sides], rings[r+1][k]
			tri(a, b, c)
			tri(a, c, d)
		}
	}
	top := rings[len(rings)-1]
	for k := 0; k < sides; k++ {
		tri(top[k], top[(k+1)%sides], apex)
	}
	return g, nil
}

func chamferedRect(x, z, cut float64) [8][2]float64 {
	return [8][2]float64{{-x + cut, -z}, {x - cut, -z}, {x, -z + cut}, {x, z - cut}, {x - cut, z}, {-x + cut, z}, {-x, z - cut}, {-x, -z + cut}}
}

func addQuad(g *geometry, a, b, c, d, outward vec3) {
	n := normalize(cross(sub(b, a), sub(c, a)))
	if dot(n, outward) < 0 {
		b, d = d, b
		n = scaleVec(n, -1)
	}
	addTriangle(g, a, b, c, n)
	addTriangle(g, a, c, d, n)
}

func addTriangle(g *geometry, a, b, c, n vec3) {
	for _, p := range [...]vec3{a, b, c} {
		g.positions = append(g.positions, p.x, p.y, p.z)
		g.normals = append(g.normals, n.x, n.y, n.z)
		g.uvs = append(g.uvs, p.x, p.z)
		g.indices = append(g.indices, uint16(len(g.indices)))
	}
}

func normalize(v vec3) vec3 {
	length := math.Sqrt(v.x*v.x + v.y*v.y + v.z*v.z)
	if length == 0 {
		return vec3{0, 1, 0}
	}
	return vec3{v.x / length, v.y / length, v.z / length}
}
func cross(a, b vec3) vec3                { return vec3{a.y*b.z - a.z*b.y, a.z*b.x - a.x*b.z, a.x*b.y - a.y*b.x} }
func sub(a, b vec3) vec3                  { return vec3{a.x - b.x, a.y - b.y, a.z - b.z} }
func dot(a, b vec3) float64               { return a.x*b.x + a.y*b.y + a.z*b.z }
func scaleVec(a vec3, scale float64) vec3 { return vec3{a.x * scale, a.y * scale, a.z * scale} }

// Bathymetry heightmap for scene.Ocean: the terrain height function sampled
// over a wider area than the mesh, so the far sea floor and the headlands
// beyond the mesh edges still read correctly. R = (h - BathymetryMinHeight) /
// (BathymetryMaxHeight - BathymetryMinHeight), row 0 at BathymetryMinZ.
const (
	BathymetrySize      = 512
	BathymetryMinX      = -80.0
	BathymetryMaxX      = 80.0
	BathymetryMinZ      = -60.0
	BathymetryMaxZ      = 50.0
	BathymetryMinHeight = -8.0
	BathymetryMaxHeight = 4.0
)

func makeBathymetry(n noiseField) ([]byte, error) {
	img := image.NewGray(image.Rect(0, 0, BathymetrySize, BathymetrySize))
	seed := int64(n.seed)
	stacks := stackSpecs(seed)
	boulders := boulderSpecs(seed)
	for y := 0; y < BathymetrySize; y++ {
		z := BathymetryMinZ + (float64(y)+0.5)/BathymetrySize*(BathymetryMaxZ-BathymetryMinZ)
		for x := 0; x < BathymetrySize; x++ {
			wx := BathymetryMinX + (float64(x)+0.5)/BathymetrySize*(BathymetryMaxX-BathymetryMinX)
			height := terrainHeight(n, wx, z)
			leftDistance := cliffFrontX([3]cliffPoint{{-28, -30}, {-30, 10}, {-36, 50}}, z) - wx
			rightDistance := wx - cliffFrontX([3]cliffPoint{{34, -30}, {35, 12}, {40, 50}}, z)
			if z >= -30 && (leftDistance > 0 || rightDistance > 0) {
				height = math.Max(height, 8)
			}
			for _, stack := range stacks {
				centerX := stack.x + 6*stack.lean
				// A smooth mound, not a disk: the shallow-water and foam bands
				// then ring the rock instead of drawing texel squares.
				d := math.Hypot(wx-centerX, z-stack.z)
				rise := 1 - smoothstep(stack.radius*.92, stack.radius*1.08, d)
				height = math.Max(height, lerp(height, stack.height, rise))
			}
			for _, boulder := range boulders {
				d := math.Hypot(wx-boulder.x, z-boulder.z)
				rise := 1 - smoothstep(boulder.radius*.8, boulder.radius*1.15, d)
				top := terrainHeight(n, boulder.x, boulder.z) + boulder.radius
				height = math.Max(height, lerp(height, top, rise))
			}
			v := BathymetryEncode(height)
			img.Pix[y*img.Stride+x] = uint8(math.Round(math.Max(0, math.Min(1, v)) * 255))
		}
	}
	return encodePNG(img)
}

// BathymetryEncoding is the scene.OceanBathymetry encoding of the heightmap.
const BathymetryEncoding = "signed-sqrt"

// BathymetryEncode maps a world height to the stored R value: signed-sqrt
// spends the 8-bit steps near sea level, where the shoreline needs them.
func BathymetryEncode(h float64) float64 {
	r := math.Max(math.Abs(BathymetryMinHeight), math.Abs(BathymetryMaxHeight))
	s := math.Sqrt(math.Min(1, math.Abs(h)/r))
	if h < 0 {
		s = -s
	}
	return 0.5 + 0.5*s
}

// BathymetryDecode inverts BathymetryEncode (the shader does the same).
func BathymetryDecode(v float64) float64 {
	r := math.Max(math.Abs(BathymetryMinHeight), math.Abs(BathymetryMaxHeight))
	s := 2*v - 1
	return math.Copysign(s*s*r, s)
}
