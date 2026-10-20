package beachgen

import (
	"image"
	"image/color"
	"math"
)

// CoastLifeAssets batches the sparse dune and tideline scatter into two
// quantized draw calls. No downloaded textures or per-object transforms.
func CoastLifeAssets(seed int64) (map[string][]byte, error) {
	files := make(map[string][]byte)
	for name, entry := range map[string]struct {
		mesh  *geometry
		color [4]float64
	}{
		"dune-grass.glb":     {duneGrass(seed), [4]float64{srgbLinear(.36), srgbLinear(.38), srgbLinear(.24), 1}},
		"tideline-wrack.glb": {tidelineWrack(seed), [4]float64{srgbLinear(.27), srgbLinear(.24), srgbLinear(.16), 1}},
	} {
		material := solidMaterial(entry.color, .82, 0)
		material["doubleSided"] = true
		data, err := writeGLB(entry.mesh, material, nil)
		if err != nil {
			return nil, err
		}
		files[name] = data
	}
	rough, err := rockRoughness(seed)
	if err != nil {
		return nil, err
	}
	files["rock-rough.jpg"] = rough
	return files, nil
}

func scatterQuad(g *geometry, a, b, c, d vec3) {
	first := appendVertex(g, a, 0, 0)
	appendVertex(g, b, 1, 0)
	appendVertex(g, c, 1, 1)
	appendVertex(g, d, 0, 1)
	g.indices = append(g.indices, first, first+1, first+2, first, first+2, first+3)
}

func duneGrass(seed int64) *geometry {
	rng := newRandom(seed + 0x6A55)
	g := &geometry{}
	for clump := 0; clump < 90; clump++ {
		x, z := -27+rng.float64()*54, 13+rng.float64()*26
		// Leave the footprint approach and the principal sight line open.
		if math.Abs(x+5) < 5 {
			continue
		}
		y := TerrainHeight(x, z, seed)
		for blade := 0; blade < 5; blade++ {
			a, height := rng.float64()*2*math.Pi, .3+rng.float64()*.55
			width := .018 + rng.float64()*.018
			dx, dz := math.Cos(a)*width, math.Sin(a)*width
			bendX, bendZ := .18*height, -.22*height
			root := vec3{x + (rng.float64()-.5)*.28, y - .02, z + (rng.float64()-.5)*.28}
			mid := vec3{root.x + bendX*.3, y + height*.6, root.z + bendZ*.3}
			tip := vec3{root.x + bendX, y + height, root.z + bendZ}
			scatterQuad(g, vec3{root.x - dx, root.y, root.z - dz}, vec3{root.x + dx, root.y, root.z + dz},
				vec3{mid.x + dx*.6, mid.y, mid.z + dz*.6}, vec3{mid.x - dx*.6, mid.y, mid.z - dz*.6})
			scatterQuad(g, vec3{mid.x - dx*.6, mid.y, mid.z - dz*.6}, vec3{mid.x + dx*.6, mid.y, mid.z + dz*.6}, tip, tip)
		}
	}
	averageVertexNormals(g)
	return g
}

func tidelineWrack(seed int64) *geometry {
	rng := newRandom(seed + 0xD21F7)
	g := &geometry{}
	for i := 0; i < 54; i++ {
		x, z := -25+rng.float64()*50, 5+rng.float64()*8
		if math.Hypot(x-MonolithX, z-MonolithZ) < 3 {
			continue
		}
		angle, length := rng.float64()*math.Pi, .5+rng.float64()*1.5
		if i < 12 {
			// Bleached, crooked driftwood with a narrower broken end.
			radius := .035 + rng.float64()*.045
			for side := 0; side < 7; side++ {
				a, b := float64(side)*2*math.Pi/7, float64(side+1)*2*math.Pi/7
				point := func(t, arc float64) vec3 {
					px := x + math.Cos(angle)*length*t + math.Sin(angle)*math.Cos(arc)*radius
					pz := z + math.Sin(angle)*length*t - math.Cos(angle)*math.Cos(arc)*radius
					return vec3{px, TerrainHeight(px, pz, seed) + radius + math.Sin(arc)*radius*(1-.4*t), pz}
				}
				scatterQuad(g, point(0, a), point(0, b), point(1, b), point(1, a))
			}
		} else {
			// Thin kelp ribbons follow the sand instead of floating above it.
			for segment := 0; segment < 5; segment++ {
				point := func(t, edge float64) vec3 {
					lateral := .1*math.Sin(t*8+angle) + edge*.035*math.Sin(math.Pi*(.05+.9*t))
					px := x + math.Cos(angle)*length*t + math.Sin(angle)*lateral
					pz := z + math.Sin(angle)*length*t - math.Cos(angle)*lateral
					return vec3{px, TerrainHeight(px, pz, seed) + .016, pz}
				}
				t0, t1 := float64(segment)/5, float64(segment+1)/5
				scatterQuad(g, point(t0, -1), point(t0, 1), point(t1, 1), point(t1, -1))
			}
		}
	}
	averageVertexNormals(g)
	return g
}

func rockRoughness(seed int64) ([]byte, error) {
	const size = 128
	img := image.NewGray(image.Rect(0, 0, size, size))
	n := newNoise(seed + 0x70C)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			h := periodicRockHeight(n, float64(x)/size, float64(y)/size)
			img.SetGray(x, y, color.Gray{Y: uint8(clamp(.73+h*.65, .48, .94) * 255)})
		}
	}
	return encodeJPEG(img, 82)
}
