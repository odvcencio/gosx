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
	"sort"
)

const generatorSeed int64 = 0xB1AC6A55

type vec3 struct{ x, y, z float64 }
type geometry struct {
	positions []float64
	normals   []float64
	uvs       []float64
	indices   []uint16
	// seams pairs vertices duplicated only to carry a UV seam; they share
	// one averaged normal so lighting stays continuous across the seam.
	seams [][2]int
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
	rockColor, rockRoughness, err := makeRockSurface(noise)
	if err != nil {
		return nil, err
	}
	beach, err := terrainGeometry(noise)
	if err != nil {
		return nil, err
	}
	beachGLB, err := writeTerrainGLB(beach, []embeddedImage{{name: "albedo", data: albedo, mime: "image/jpeg"}, {name: "metallic-roughness", data: mr}, {name: "sand-normal", data: normal, mime: "image/jpeg"}})
	if err != nil {
		return nil, fmt.Errorf("beach GLB: %w", err)
	}
	stacks, err := stackGeometry(seed)
	if err != nil {
		return nil, err
	}
	stacksGLB, err := writeGLB(stacks, rockMaterial(), []embeddedImage{{name: "rock-normal", data: rockNormal, mime: "image/jpeg"}, {name: "rock-color", data: rockColor, mime: "image/jpeg"}, {name: "rock-wetness", data: rockRoughness}})
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
	files, err := CoastLifeAssets(seed)
	if err != nil {
		return nil, err
	}
	for name, data := range map[string][]byte{
		"beach-v2-height.png": height,
		"beach-v2.glb":        beachGLB,
		"beach-v2-albedo.jpg": albedo,
		"beach-v2-mr.png":     mr,
		"sand-normal.jpg":     normal,
		"rock-normal.jpg":     rockNormal,
		"stacks-v2.glb":       stacksGLB,
		"monolith-v2.glb":     monolithGLB,
	} {
		files[name] = data
	}
	ship, err := ShipAssets()
	if err != nil {
		return nil, err
	}
	for name, data := range ship {
		files[name] = data
	}
	return files, nil
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
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(outDir, name), files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// TerrainHeight evaluates the deterministic terrain surface at x,z.
func TerrainHeight(x, z float64, seed int64) float64 { return terrainHeight(newNoise(seed), x, z) }

// A shallow cove bends around the headlands. Fade its offset in the deep
// basin so the bathymetry keeps the same offshore depth and extent.
func shoreDistance(x, z float64) float64 {
	// Small beach cusps interrupt the cove's broad arc. Mesh, wetness,
	// walking and wave run-up all sample the same changing shoreline.
	return z - (.005*x*x+2.2*math.Sin(x/17)+.45*math.Sin(x*.37)+.18*math.Sin(x*.91))*smoothstep(-38, -12, z)
}

func headlandProfile(t float64) float64 {
	// A talus apron meets a steep eroded face. Smooth joins avoid the long,
	// perfectly horizontal steps of the old three-ramp profile.
	return .12*smoothstep(0, .32, t) + .81*smoothstep(.24, .78, t) + .07*smoothstep(.74, 1, t)
}

func leftCliffEdge(n noiseField, z float64) float64 {
	// Erosion follows unequal fault blocks, rather than a row of identical
	// vertical flutes. Broad coves and small joints have independent scales.
	return -31 + 3.6*n.value(z*.07, 17) + 2.1*n.value(z*.21, 9) + .65*n.value(z*.73, 31)
}

func terrainHeight(n noiseField, x, z float64) float64 {
	shore := shoreDistance(x, z)
	var h float64
	switch {
	case shore < 0:
		h = math.Max(-6, .12*shore)
	case shore <= 18:
		h = .055*shore + .65*smoothstep(4, 18, shore)
	default:
		h = 1.64 + 2.1*smoothstep(18, 50, shore) + n.fbm(x/12, z/12, 4)*smoothstep(18, 28, shore)
	}
	sand := n.fbm(x*.17+19, z*.17-7, 4) * .08
	h += sand
	zGate := smoothstep(-40, -30, z)
	// Spurs, coves and narrow drainage cuts change the outline at several
	// scales. The same surface drives the mesh, walking and bathymetry.
	leftEdge := leftCliffEdge(n, z)
	rightEdge := 37 + 2.6*math.Sin(z*.16+1.3) + 1.1*math.Sin(z*.47)
	depth := clamp((leftEdge+2-x)/6, 0, 1)
	// Faults drift across the face with height. Local shelves break up the
	// silhouette without making continuous horizontal terraces.
	depth = clamp(depth+.12*n.fbm(z*.17+11, depth*4.8, 3)*math.Sin(math.Pi*depth), 0, 1)
	left := headlandProfile(depth) * zGate
	right := headlandProfile(clamp((x-rightEdge+2)/6, 0, 1)) * zGate
	if left > 0 {
		base := h
		cliff := n.fbm(x/7.2+47, z/6.2-11, 4)
		ridge := (1 - 2*math.Abs(cliff)) * 3
		gully := 2.6 * math.Pow(1-math.Abs(n.value(z*.23, 39)), 8) * smoothstep(0, .7, left)
		ridge -= gully
		ridge += 1.1 * n.fbm(z*.38, depth*8.3+7, 3) * math.Sin(math.Pi*depth)
		h = lerp(base, 18+ridge-6*(1-smoothstep(-22, 8, z)), left)
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
			// Spend the same grid on the cliffs' cross-sections, where the
			// former metre-wide columns reduced each face to a few triangles.
			var x float64
			switch {
			case column < 16:
				x = lerp(-60, -39, float64(column)/16)
			case column < 48:
				x = lerp(-39, -25, float64(column-16)/32)
			case column < 96:
				x = lerp(-25, 29, float64(column-48)/48)
			case column < 120:
				x = lerp(29, 43, float64(column-96)/24)
			default:
				x = lerp(43, 60, float64(column-120)/8)
			}
			y := terrainHeight(n, x, z)
			dx := (terrainHeight(n, x+.2, z) - terrainHeight(n, x-.2, z)) / .4
			dz := (terrainHeight(n, x, z+.2) - terrainHeight(n, x, z-.2)) / .4
			normal := normalize(vec3{-dx, 1, -dz})
			px, pz := x, z
			if x > -39 && x < -25 {
				// Sculpt the inaccessible upper wall in three dimensions: broken
				// ledges protrude across the old heightfield's vertical ribs. The
				// walkable foot and waterline retain the exact sampled ground.
				face := smoothstep(.8, 2.5, math.Hypot(dx, dz)) * smoothstep(3, 5, y) * (1 - smoothstep(17, 21, y)) * smoothstep(-40, -37, z) * (1 - smoothstep(46, 50, z))
				fx, fz := cliffFracture(n, y, z)
				px += face * fx
				pz += face * fz
			}
			g.positions = append(g.positions, px, y, pz)
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
	// Lighting must follow the sculpted faces, not the analytic heightfield
	// gradients used to identify them before displacement.
	clear(g.normals)
	averageVertexNormals(g)
	return g, nil
}

func makeAlbedo(seed int64, n noiseField) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for py := 0; py < 1024; py++ {
		z := -40 + 90*float64(py)/1023
		for px := 0; px < 1024; px++ {
			x := -60 + 120*float64(px)/1023
			h := terrainHeight(n, x, z)
			shore := shoreDistance(x, z)
			dx := terrainHeight(n, x+.5, z) - terrainHeight(n, x-.5, z)
			dz := terrainHeight(n, x, z+.5) - terrainHeight(n, x, z-.5)
			rock := math.Hypot(dx, dz) > .6 || (math.Abs(x) > 34 && h > 2)
			ao := ambientOcclusion(n, x, z, h)
			factor := .7 + .3*ao
			base := [3]float64{68, 70, 71}
			foam := 0.0
			if rock {
				base = [3]float64{74, 76, 75}
				fracture := 1 - smoothstep(.025, .12, math.Abs(n.value(x*.65+h*.12, z*.28)))
				mineral := n.fbm(x*.9, z*.6+h*.45, 3)
				weather := n.fbm(x*.18+h*.14, z*.16, 3)
				wet := 1 - smoothstep(.1, 1.5, h)
				factor *= (1 + .14*mineral + .12*weather - .14*fracture) * (1 - .4*wet)
			} else {
				mottle := n.fbm(x/3+float64(seed%19), z/3-float64(seed%23), 3) * .10
				streaks := n.fbm(x/9+float64(seed%7), z*1.4, 3) * .08 // wind streaks run across the beach
				grain := n.value(x*18+float64(seed%19), z*18-float64(seed%23)) * .10
				factor *= 1 + mottle + streaks + grain
				// Broken filaments left by the retreating wash. The live ocean
				// supplies moving run-up; these narrow lines remain on sand.
				if shore > .6 && shore < 6 && h < .6 {
					contour := shore + .35*n.fbm(x*.15, z*.2, 3) + .08*math.Sin(x*.8)
					front := math.Exp(-math.Pow((contour-1.7)/.075, 2))
					back := .48 * math.Exp(-math.Pow((contour-4.2)/.055, 2))
					breakup := smoothstep(-.22, .3, n.value(x*2, z*3))
					foam = (front + back) * breakup * .72
				}
				// Wet sand is darker; its roughness band makes it mirror the sky.
				wet := smoothstep(.8, .12, h)
				if shore < 12 {
					factor *= 1 - .55*wet
				}
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(math.Round(lerp(base[0]*factor, 185, foam))),
				G: uint8(math.Round(lerp(base[1]*factor, 192, foam))),
				B: uint8(math.Round(lerp(base[2]*factor, 188, foam))), A: 255,
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
				roughness = lerp(.28, .84, smoothstep(.1, 1.5, h))
			} else if h < -.2 {
				roughness = .25
			} else if shoreDistance(x, z) < 12 {
				roughness = .065 + .735*smoothstep(.12, .8, h)
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
		"normalTexture": map[string]any{"index": 2, "scale": .24, "extensions": map[string]any{"KHR_texture_transform": transform}},
	}
}

func writeTerrainGLB(g *geometry, images []embeddedImage) ([]byte, error) {
	ground, rock := make([]uint16, 0, len(g.indices)), make([]uint16, 0, len(g.indices)/3)
	for i := 0; i < len(g.indices); i += 3 {
		var normalY, height, x float64
		for _, vertex := range g.indices[i : i+3] {
			p := int(vertex) * 3
			normalY += g.normals[p+1] / 3
			height += g.positions[p+1] / 3
			x += g.positions[p] / 3
		}
		if (normalY < .82 && height > .4) || (math.Abs(x) > 34 && height > 2) {
			rock = append(rock, g.indices[i:i+3]...)
		} else {
			ground = append(ground, g.indices[i:i+3]...)
		}
	}
	mesh := *g
	mesh.indices = append(ground, rock...)
	basalt := terrainMaterial()
	basalt["name"] = "Fractured basalt headlands"
	delete(basalt, "normalTexture") // world-space basalt detail replaces the sand ripples
	return writeGLB(&mesh, terrainMaterial(), images,
		geometrySection{start: 0, count: len(ground)},
		geometrySection{start: len(ground), count: len(rock), material: basalt})
}

func rockMaterial() map[string]any {
	return map[string]any{
		"name": "Weathered basalt",
		"pbrMetallicRoughness": map[string]any{
			"baseColorTexture":         map[string]any{"index": 1},
			"metallicRoughnessTexture": map[string]any{"index": 2},
			"metallicFactor":           0,
			"roughnessFactor":          1,
		},
		"normalTexture": map[string]any{"index": 0, "scale": .4, "extensions": map[string]any{"KHR_texture_transform": map[string]any{"scale": []float64{3, 9}}}},
	}
}

// Height is stored in the rock UV, so damp feet, mineral seams and dry
// shoulders stay attached to the actual rock instead of repeating up it.
// These small maps carry macro variation; scanned detail supplies the grain.
func makeRockSurface(n noiseField) ([]byte, []byte, error) {
	const width, height = 128, 256
	albedo := image.NewNRGBA(image.Rect(0, 0, width, height))
	palette := make(color.Palette, 16)
	for i := range palette {
		palette[i] = color.NRGBA{G: uint8(i * 17), A: 255}
	}
	roughness := image.NewPaletted(albedo.Bounds(), palette)
	for row := range height {
		y := -6 + 24*float64(row)/float64(height-1)
		for col := range width {
			a := 2 * math.Pi * float64(col) / float64(width-1)
			x, z := math.Cos(a)*3, math.Sin(a)*3
			mineral := fbm3(n, x, y*.28, z, 3)
			wet := 1 - smoothstep(.1, 1.5+.35*mineral, y)
			seam := math.Pow(1-math.Abs(noiseValue3(n, x*2.1, y*.07, z*2.1)), 10)
			factor := (1 + .17*mineral - .1*seam) * (1 - .5*wet)
			albedo.SetNRGBA(col, row, color.NRGBA{R: uint8(74 * factor), G: uint8((76 + wet*3) * factor), B: uint8((75 + wet*5) * factor), A: 255})
			roughness.SetColorIndex(col, row, uint8(math.Round(clamp((.87-.62*wet+.05*mineral)*15, 0, 15))))
		}
	}
	colorMap, err := encodeJPEG(albedo, 82)
	if err != nil {
		return nil, nil, err
	}
	roughnessMap, err := encodePNG(roughness)
	return colorMap, roughnessMap, err
}

func solidMaterial(base [4]float64, roughness, metalness float64) map[string]any {
	return map[string]any{"pbrMetallicRoughness": map[string]any{"baseColorFactor": base[:], "metallicFactor": metalness, "roughnessFactor": roughness}}
}

func stackGeometry(seed int64) (*geometry, error) {
	g := &geometry{}
	for _, stack := range stackSpecs(seed) {
		addStack(g, stack)
	}
	for _, boulder := range boulderSpecs(seed) {
		addBoulder(g, boulder, newNoise(seed+int64(boulder.id)*101), newNoise(seed))
	}
	// UNORM8 UVs cannot represent negative or repeated coordinates. Store
	// the full -6..18 m height in 0..1 and tile through KHR_texture_transform;
	// clamping worldY/8 flattened most of each stack's normal map.
	for vertex := range len(g.positions) / 3 {
		g.uvs[vertex*2+1] = (g.positions[vertex*3+1] + 6) / 24
	}
	averageVertexNormals(g)
	return g, nil
}

type stackSpec struct {
	x, z, height, radius, lean    float64
	cleftAngle, cleftDepth, taper float64
	seed                          int64
}

type boulderSpec struct {
	x, z, radius float64
	id           int
}

func stackSpecs(seed int64) []stackSpec {
	values := []struct{ x, z, height, radius, cleftAngle, cleftDepth, taper float64 }{
		{-16, -32, 18, 6.1, 1.05, .38, .44}, {-7, -27, 6, 2, -.6, .18, .3}, {-21, -36, 9, 2.6, 2.6, .3, .5},
		{8, -46, 12, 4, 1.57, .32, .36}, {26, -22, 2.5, 6, .3, .1, .65},
	}
	rng := newRandom(seed + 0x51ac)
	out := make([]stackSpec, len(values))
	for i, value := range values {
		lean := (rng.float64()*2 - 1) * math.Tan(4*math.Pi/180)
		out[i] = stackSpec{x: value.x, z: value.z, height: value.height, radius: value.radius, lean: lean,
			cleftAngle: value.cleftAngle, cleftDepth: value.cleftDepth, taper: value.taper, seed: seed + int64(i+1)*7919}
	}
	return out
}

func boulderSpecs(seed int64) []boulderSpec {
	rng := newRandom(seed + 0xB01D3)
	out := make([]boulderSpec, 0, 14)
	// Reuse part of the existing rock budget for fallen blocks at the cliff
	// toe. This breaks the wall/sea join without adding meshes or vertices.
	n := newNoise(seed)
	for _, z := range []float64{-25, -17, -9, -1, 6} {
		out = append(out, boulderSpec{leftCliffEdge(n, z) + 2.6, z, 1.4 + rng.float64()*.8, len(out)})
	}
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
	const sides, rings, corners = 48, 64, 12
	n := newNoise(stack.seed)
	var rim [corners]float64
	lo, hi := math.Inf(1), math.Inf(-1)
	for k := range corners {
		rim[k] = n.lattice(int64(k), 53)
		lo, hi = math.Min(lo, rim[k]), math.Max(hi, rim[k])
	}
	for k := range corners {
		rim[k] = .16 * stack.height * (rim[k] - lo) / (hi - lo)
		a := 2*math.Pi*float64(k)/corners + .12*n.lattice(int64(k), 9)
		rim[k] += .18 * stack.height * stackCleft(stack, a)
	}
	lo = math.Inf(1)
	for _, y := range rim {
		lo = math.Min(lo, y)
	}
	for k := range corners {
		rim[k] -= lo // keep each authored crest height, including the 18 m hero
	}
	first := len(g.positions) / 3
	var crown vec3
	for row := 0; row <= rings; row++ {
		t := float64(row) / rings
		for side := 0; side <= sides; side++ {
			// Four samples per face include both joint endpoints. The joint
			// shares a weathered normal and UV without spending more vertices
			// than the former rounded tube.
			face, u := (side%sides)/4, float64((side%sides)%4)/3
			a := stackCorner(n, stack, face, t, rim[:])
			b := stackCorner(n, stack, face+1, t, rim[:])
			p := vec3{lerp(a.x, b.x, u), lerp(a.y, b.y, u), lerp(a.z, b.z, u)}
			// Shallow chips interrupt the planes, while the broad faces and
			// deep joints remain readable from the walking camera.
			chip := .06 * stack.radius * fbm3(n, p.x*.65+7, p.y*.5, p.z*.65+11, 3) * math.Sin(math.Pi*u)
			p.x += chip * math.Cos(2*math.Pi*(float64(face)+.5)/corners)
			p.z += chip * math.Sin(2*math.Pi*(float64(face)+.5)/corners)
			texU := (float64(face) + u) / corners
			if side == sides {
				texU = 1
			}
			appendVertex(g, p, texU, 0)
			if row == rings && side < sides && side%4 == 0 {
				crown.x += p.x / corners
				crown.y += p.y / corners
				crown.z += p.z / corners
			}
			if side == sides {
				g.seams = append(g.seams, [2]int{first + row*(sides+1), first + row*(sides+1) + sides - 1})
				g.seams = append(g.seams, [2]int{first + row*(sides+1), first + row*(sides+1) + sides})
			} else if side > 0 && side%4 == 0 {
				// Weathering rounds the vertical joint edge. The radius changes
				// across beds still supply the ledges and silhouette breaks.
				g.seams = append(g.seams, [2]int{first + row*(sides+1) + side - 1, first + row*(sides+1) + side})
			}
		}
	}
	for row := 0; row < rings; row++ {
		for side := 0; side < sides; side++ {
			stride := sides + 1
			a := uint16(first + row*stride + side)
			b := a + 1
			c := uint16(first + (row+1)*stride + side + 1)
			d := c - 1
			g.indices = append(g.indices, a, d, c, a, c, b)
		}
	}
	bottom := appendVertex(g, vec3{stack.x, -6, stack.z}, .5, 0)
	top := appendVertex(g, crown, .5, 1)
	for side := 0; side < sides; side++ {
		firstRing, nextRing := uint16(first+side), uint16(first+side+1)
		lastRing, nextLast := uint16(first+rings*(sides+1)+side), uint16(first+rings*(sides+1)+side+1)
		g.indices = append(g.indices, bottom, firstRing, nextRing, top, nextLast, lastRing)
	}
}

func addBoulder(g *geometry, boulder boulderSpec, noise, terrainNoise noiseField) {
	const sides, rings = 16, 12
	ground := terrainHeight(terrainNoise, boulder.x, boulder.z)
	center := vec3{boulder.x, ground + boulder.radius*.15, boulder.z}
	first := len(g.positions) / 3
	for row := 0; row <= rings; row++ {
		latitude := math.Pi * float64(row) / rings
		for side := 0; side < sides; side++ {
			longitude := 2*math.Pi*float64(side)/sides + .7*noise.lattice(11, 17)
			nx := math.Sin(latitude) * math.Cos(longitude)
			ny := math.Cos(latitude)
			nz := math.Sin(latitude) * math.Sin(longitude)
			// Wave-worn broken blocks keep broad planar shoulders. Clamping
			// the radial envelope preserves the existing sphere colliders.
			plane := math.Max(math.Abs(nx), math.Max(math.Abs(ny), math.Abs(nz)))
			lumps := fbm3(noise, nx*2.6+boulder.x, ny*2.6, nz*2.6+boulder.z, 3)
			radius := boulder.radius * math.Min(1.02, .68/math.Pow(plane, .9)+.055*lumps)
			p := vec3{center.x + nx*radius, center.y + ny*radius*.72, center.z + nz*radius}
			appendVertex(g, p, float64(side)/sides, latitude/math.Pi)
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
	for _, pair := range g.seams {
		a, b := pair[0]*3, pair[1]*3
		for k := 0; k < 3; k++ {
			sum := g.normals[a+k] + g.normals[b+k]
			g.normals[a+k], g.normals[b+k] = sum, sum
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
	footprints := make([]rockFootprint, len(stacks))
	for i, stack := range stacks {
		footprints[i] = stackWaterline(stack)
	}
	boulders := boulderSpecs(seed)
	for y := 0; y < BathymetrySize; y++ {
		z := BathymetryMinZ + (float64(y)+0.5)/BathymetrySize*(BathymetryMaxZ-BathymetryMinZ)
		for x := 0; x < BathymetrySize; x++ {
			wx := BathymetryMinX + (float64(x)+0.5)/BathymetrySize*(BathymetryMaxX-BathymetryMinX)
			height := terrainHeight(n, wx, z)
			for i, stack := range stacks {
				// Follow the eroded mesh's actual sea-level contour, including
				// clefts and buttresses, rather than an unrelated circular halo.
				footprint := &footprints[i]
				if wx < footprint.minX-1 || wx > footprint.maxX+1 || z < footprint.minZ-1 || z > footprint.maxZ+1 {
					continue
				}
				d := footprint.distance(wx, z)
				rise := 1 - smoothstep(-.5, .65, d)
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

type rockFootprint struct {
	points                 []vec3
	minX, minZ, maxX, maxZ float64
}

func stackWaterline(stack stackSpec) rockFootprint {
	g := &geometry{}
	addStack(g, stack)
	const sides, rings = 48, 64
	outline := rockFootprint{minX: math.Inf(1), minZ: math.Inf(1), maxX: math.Inf(-1), maxZ: math.Inf(-1)}
	for side := range sides {
		for row := 0; row < rings; row++ {
			a := (row*(sides+1) + side) * 3
			b := a + (sides+1)*3
			if g.positions[a+1] > 0 || g.positions[b+1] < 0 {
				continue
			}
			t := -g.positions[a+1] / (g.positions[b+1] - g.positions[a+1])
			p := vec3{x: lerp(g.positions[a], g.positions[b], t), z: lerp(g.positions[a+2], g.positions[b+2], t)}
			outline.points = append(outline.points, p)
			outline.minX, outline.minZ = math.Min(outline.minX, p.x), math.Min(outline.minZ, p.z)
			outline.maxX, outline.maxZ = math.Max(outline.maxX, p.x), math.Max(outline.maxZ, p.z)
			break
		}
	}
	return outline
}

// Signed distance to the closed waterline polygon; negative inside the rock.
func (f *rockFootprint) distance(x, z float64) float64 {
	inside, nearest := false, math.Inf(1)
	for i, a := range f.points {
		b := f.points[(i+1)%len(f.points)]
		dx, dz := b.x-a.x, b.z-a.z
		t := clamp(((x-a.x)*dx+(z-a.z)*dz)/math.Max(dx*dx+dz*dz, 1e-12), 0, 1)
		nearest = math.Min(nearest, math.Hypot(x-a.x-t*dx, z-a.z-t*dz))
		if (a.z > z) != (b.z > z) && x < (b.x-a.x)*(z-a.z)/(b.z-a.z)+a.x {
			inside = !inside
		}
	}
	if inside {
		return -nearest
	}
	return nearest
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
