package beachgen

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
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

// Generate builds all six Blackglass Beach files using only deterministic code
// and the supplied seed. The returned map is independent of the output path.
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
	beach, err := terrainGeometry(noise)
	if err != nil {
		return nil, err
	}
	beachGLB, err := writeGLB(beach, terrainMaterial(), []embeddedImage{{name: "albedo", data: albedo}, {name: "metallic-roughness", data: mr}, {name: "sand-normal", data: normal}})
	if err != nil {
		return nil, fmt.Errorf("beach GLB: %w", err)
	}
	stacks, err := stackGeometry(seed)
	if err != nil {
		return nil, err
	}
	stacksGLB, err := writeGLB(stacks, solidMaterial([4]float64{srgbLinear(30.0 / 255), srgbLinear(32.0 / 255), srgbLinear(34.0 / 255), 1}, .55, 0), nil)
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
	return map[string][]byte{
		"beach-v2.glb":        beachGLB,
		"beach-v2-albedo.png": albedo,
		"beach-v2-mr.png":     mr,
		"sand-normal.png":     normal,
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
	for _, name := range []string{"beach-v2.glb", "beach-v2-albedo.png", "beach-v2-mr.png", "sand-normal.png", "stacks-v2.glb", "monolith-v2.glb"} {
		if err := os.WriteFile(filepath.Join(outDir, name), files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// TerrainHeight evaluates the deterministic terrain surface at x,z.
func TerrainHeight(x, z float64, seed int64) float64 { return terrainHeight(newNoise(seed), x, z) }

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
	left := (1 - smoothstep(-34, -28, x)) * zGate
	right := smoothstep(34, 40, x) * zGate
	if left > 0 {
		base := h
		cliff := n.fbm(x/4.8+47, z/7.2-11, 5)
		ridge := (1 - 2*math.Abs(cliff)) * 3
		h = lerp(base, 18+ridge, left)
	}
	if right > 0 {
		base := h
		cliff := n.fbm(x/4.8-31, z/7.2+23, 5)
		ridge := (1 - 2*math.Abs(cliff)) * 3
		h = lerp(base, 12+ridge, right)
	}
	return h
}

func terrainGeometry(n noiseField) (*geometry, error) {
	const columns, rows = 161, 161
	g := &geometry{positions: make([]float64, 0, columns*rows*3), normals: make([]float64, 0, columns*rows*3), uvs: make([]float64, 0, columns*rows*2), indices: make([]uint16, 0, (columns-1)*(rows-1)*6)}
	zs := make([]float64, rows)
	for row := 0; row < rows; row++ {
		switch {
		case row < 40:
			zs[row] = lerp(-40, -12, float64(row)/39)
		case row <= 120:
			zs[row] = lerp(-12, 22, float64(row-40)/80)
		default:
			zs[row] = lerp(22, 50, float64(row-120)/40)
		}
	}
	for _, z := range zs {
		for column := 0; column < columns; column++ {
			x := -60 + 120*float64(column)/160
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
	palette := make(color.Palette, 16)
	pigments := [4][3]int{{27, 26, 28}, {28, 27, 29}, {26, 25, 27}, {37, 39, 42}}
	for pigment, base := range pigments {
		for level := 0; level < 4; level++ {
			factor := .45 + .55*float64(level)/3
			palette[pigment*4+level] = color.RGBA{R: uint8(math.Round(float64(base[0]) * factor)), G: uint8(math.Round(float64(base[1]) * factor)), B: uint8(math.Round(float64(base[2]) * factor)), A: 255}
		}
	}
	bayer := [4][4]float64{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}
	img := image.NewPaletted(image.Rect(0, 0, 512, 512), palette)
	for py := 0; py < 512; py++ {
		z := -40 + 90*float64(py)/511
		for px := 0; px < 512; px++ {
			x := -60 + 120*float64(px)/511
			h := terrainHeight(n, x, z)
			dx := (terrainHeight(n, x+.5, z) - terrainHeight(n, x-.5, z))
			dz := (terrainHeight(n, x, z+.5) - terrainHeight(n, x, z-.5))
			slope := math.Hypot(dx, dz)
			rock := slope > .6 || (math.Abs(x) > 34 && h > 2)
			pigment := 0
			if rock {
				pigment = 3
			} else {
				variation := n.value(x*.45+float64(seed%19), z*.45-float64(seed%23))
				if variation > .35 {
					pigment = 1
				} else if variation < -.35 {
					pigment = 2
				}
			}
			ao := ambientOcclusion(n, x, z, h)
			level := int(math.Round((ao-.45)/.55*3 + (bayer[py%4][px%4]-7.5)/16))
			level = int(clamp(float64(level), 0, 3))
			img.SetColorIndex(px, py, uint8(pigment*4+level))
		}
	}
	return encodePNG(img)
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
			} else if z < 4 && h < .45 {
				roughness = .1 + .7*smoothstep(-.2, .45, h)
			}
			img.SetColorIndex(px, py, uint8(math.Round(clamp(roughness, 0, 1)*15)))
		}
	}
	return encodePNG(img)
}

func makeSandNormal() ([]byte, error) {
	palette := make(color.Palette, 64)
	for ix := 0; ix < 8; ix++ {
		for iy := 0; iy < 8; iy++ {
			nx := (float64(ix)/7*2 - 1) * .16
			ny := (float64(iy)/7*2 - 1) * .28
			nz := math.Sqrt(math.Max(.01, 1-nx*nx-ny*ny))
			palette[ix*8+iy] = color.RGBA{R: uint8(math.Round((nx*.5 + .5) * 255)), G: uint8(math.Round((ny*.5 + .5) * 255)), B: uint8(math.Round((nz*.5 + .5) * 255)), A: 255}
		}
	}
	img := image.NewPaletted(image.Rect(0, 0, 512, 512), palette)
	const tileMeters = 2.5
	for y := 0; y < 512; y++ {
		v := float64(y) / 512
		for x := 0; x < 512; x++ {
			u := float64(x) / 512
			du := 1.0 / 512
			dhdu := (rippleHeight(u+du, v) - rippleHeight(u-du, v)) / (2 * du * tileMeters)
			dhdv := (rippleHeight(u, v+du) - rippleHeight(u, v-du)) / (2 * du * tileMeters)
			normal := normalize(vec3{-dhdu, -dhdv, 1})
			ix := int(math.Round(clamp(normal.x/.32*.5+.5, 0, 1) * 7))
			iy := int(math.Round(clamp(normal.y/.56*.5+.5, 0, 1) * 7))
			img.SetColorIndex(x, y, uint8(ix*8+iy))
		}
	}
	return encodePNG(img)
}

func rippleHeight(u, v float64) float64 {
	warp := .045*math.Sin(2*math.Pi*3*u)*math.Sin(2*math.Pi*2*v) + .025*math.Sin(2*math.Pi*(5*u+4*v))
	phase := 2 * math.Pi * 9 * (v + warp)
	grain := .00018*math.Sin(2*math.Pi*(37*u+19*v)) + .00012*math.Sin(2*math.Pi*(23*u-41*v))
	return .0028*math.Sin(phase) + grain
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
	transform := map[string]any{"scale": []float64{48, 36}}
	return map[string]any{
		"name": "Black volcanic sand",
		"pbrMetallicRoughness": map[string]any{
			"baseColorTexture":         map[string]any{"index": 0},
			"metallicRoughnessTexture": map[string]any{"index": 1},
			"metallicFactor":           1.0,
			"roughnessFactor":          1.0,
		},
		"normalTexture": map[string]any{"index": 2, "scale": .6, "extensions": map[string]any{"KHR_texture_transform": transform}},
	}
}

func solidMaterial(base [4]float64, roughness, metalness float64) map[string]any {
	return map[string]any{"pbrMetallicRoughness": map[string]any{"baseColorFactor": base[:], "metallicFactor": metalness, "roughnessFactor": roughness}}
}

func stackGeometry(seed int64) (*geometry, error) {
	g := &geometry{}
	clusters := []struct {
		center vec3
		count  int
		radius float64
		maxTop float64
		step   float64
		seed   int64
	}{{vec3{-14, 0, -30}, 30, 4.2, 16, 2.2, seed + 17}, {vec3{9, 0, -44}, 20, 3.45, 11, 1.7, seed + 31}, {vec3{26, 0, -22}, 12, 2.7, 6, 1.1, seed + 59}}
	for _, cluster := range clusters {
		if err := addCluster(g, cluster.center, cluster.count, cluster.radius, cluster.maxTop, cluster.step, cluster.seed); err != nil {
			return nil, err
		}
	}
	return g, nil
}

type candidate struct{ x, z, distance float64 }

func addCluster(g *geometry, center vec3, count int, spread, maxTop, step float64, seed int64) error {
	spacing := 1.28
	candidates := make([]candidate, 0, count+12)
	for row := -8; row <= 8; row++ {
		for column := -8; column <= 8; column++ {
			x := (float64(column) + .5*float64(row&1)) * spacing
			z := float64(row) * spacing * .8660254037844386
			distance := math.Hypot(x, z)
			if distance <= spread {
				candidates = append(candidates, candidate{x: x, z: z, distance: distance})
			}
		}
	}
	if len(candidates) < count {
		return fmt.Errorf("cluster at (%.1f, %.1f) has only %d column sites, need %d", center.x, center.z, len(candidates), count)
	}
	rng := newRandom(seed)
	for i := len(candidates) - 1; i > 0; i-- {
		j := rng.intn(i + 1)
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}
	noise := newNoise(seed)
	for i := 0; i < count; i++ {
		point := candidates[i]
		radius := .45 + .25*rng.float64()
		distanceTier := int(point.distance / (spread / 4))
		jitter := (noise.value(point.x*1.7+3, point.z*1.7-9) + 1) * .22
		top := maxTop - float64(distanceTier)*step - jitter
		if top < -0.1 {
			top = -.1
		}
		angle := rng.float64() * 2 * math.Pi
		tilt := .10471975511965977 * rng.float64()
		slopeX, slopeZ := math.Cos(angle)*tilt, math.Sin(angle)*tilt
		addColumn(g, vec3{center.x + point.x, 0, center.z + point.z}, radius, top, slopeX, slopeZ)
	}
	return nil
}

func addColumn(g *geometry, center vec3, radius, top float64, slopeX, slopeZ float64) {
	const sides = 6
	bottom, upper, inset := make([]vec3, sides), make([]vec3, sides), make([]vec3, sides)
	for i := 0; i < sides; i++ {
		angle := 2*math.Pi*float64(i)/sides + math.Pi/6
		dx, dz := math.Cos(angle), math.Sin(angle)
		bottom[i] = vec3{center.x + dx*radius, -6, center.z + dz*radius}
		y := top + slopeX*dx*radius + slopeZ*dz*radius
		upper[i] = vec3{center.x + dx*radius, y - .04, center.z + dz*radius}
		inset[i] = vec3{center.x + dx*(radius-.04), y, center.z + dz*(radius-.04)}
	}
	for i := 0; i < sides; i++ {
		next := (i + 1) % sides
		outward := vec3{math.Cos(2*math.Pi*(float64(i)+.5)/sides + math.Pi/6), 0, math.Sin(2*math.Pi*(float64(i)+.5)/sides + math.Pi/6)}
		addQuad(g, bottom[i], upper[i], upper[next], bottom[next], outward)
		addQuad(g, upper[i], inset[i], inset[next], upper[next], outward)
		centerTop := vec3{center.x, top, center.z}
		topNormal := normalize(vec3{-slopeX, 1, -slopeZ})
		addTriangle(g, centerTop, inset[next], inset[i], topNormal)
	}
}

func monolithGeometry() (*geometry, error) {
	g := &geometry{}
	const halfWidth, halfDepth, chamfer = .8, .25, .05
	bottomInner := chamferedRect(halfWidth-chamfer, halfDepth-chamfer, chamfer)
	outer := chamferedRect(halfWidth, halfDepth, chamfer)
	bottomInnerRing := make([]vec3, 8)
	bottomOuterRing := make([]vec3, 8)
	upperOuterRing := make([]vec3, 8)
	topInnerRing := make([]vec3, 8)
	for i := 0; i < 8; i++ {
		bottomInnerRing[i] = vec3{bottomInner[i][0], 0, bottomInner[i][1]}
		bottomOuterRing[i] = vec3{outer[i][0], .05, outer[i][1]}
		topY := 3.4 + outer[i][0]*math.Tan(12*math.Pi/180)
		upperOuterRing[i] = vec3{outer[i][0], topY - .05, outer[i][1]}
		innerX := bottomInner[i][0]
		innerZ := bottomInner[i][1]
		topInnerRing[i] = vec3{innerX, 3.4 + innerX*math.Tan(12*math.Pi/180), innerZ}
	}
	for i := 0; i < 8; i++ {
		next := (i + 1) % 8
		midX := (outer[i][0] + outer[next][0]) * .5
		midZ := (outer[i][1] + outer[next][1]) * .5
		outward := vec3{midX, 0, midZ}
		addQuad(g, bottomInnerRing[i], bottomOuterRing[i], bottomOuterRing[next], bottomInnerRing[next], outward)
		addQuad(g, bottomOuterRing[i], upperOuterRing[i], upperOuterRing[next], bottomOuterRing[next], outward)
		addQuad(g, upperOuterRing[i], topInnerRing[i], topInnerRing[next], upperOuterRing[next], outward)
		topNormal := normalize(vec3{-math.Tan(12 * math.Pi / 180), 1, 0})
		addTriangle(g, vec3{0, 3.4, 0}, topInnerRing[next], topInnerRing[i], topNormal)
		addTriangle(g, vec3{0, 0, 0}, bottomInnerRing[next], bottomInnerRing[i], vec3{0, -1, 0})
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
