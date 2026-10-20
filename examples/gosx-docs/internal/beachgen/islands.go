package beachgen

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// OffshoreIsland is a bounded, deterministic terrain patch. The same height
// function authors the model, walker, hull floor and ocean depth field.
type OffshoreIsland struct {
	ID, Name, Find, Title          string
	X, Z, RadiusX, RadiusZ, Height float64
}

func OffshoreIslands() []OffshoreIsland {
	return []OffshoreIsland{
		{"drift-isle", "Drift Isle", "drift-letter", "The drift letter", 170, -160, 30, 24, 8},
		{"bell-rock", "Bell Rock", "tide-bell", "The tide bell", -180, -250, 28, 26, 13},
		{"starglass", "Starglass Isle", "star-dial", "The star dial", 280, -360, 36, 30, 7},
	}
}

func IslandHeight(island OffshoreIsland, x, z float64) float64 {
	dx, dz := (x-island.X)/island.RadiusX, (z-island.Z)/island.RadiusZ
	angle := math.Atan2(dz, dx)
	edge := 1 + .065*math.Sin(angle*3+.4) + .035*math.Sin(angle*7)
	r := math.Hypot(dx, dz) / edge
	shelf := -8 + 9.3*(1-smoothstep(.72, 1.4, r))
	// A low south beach climbs to a broken ridge on the north side. Its
	// approach remains gentle enough to walk from the pier to the find.
	hill := island.Height * (1 - smoothstep(.08, .85, r)) * (.48 + .52*smoothstep(.3, -.55, dz))
	fault := (.4*math.Sin(dx*12+dz*2) + .24*math.Sin(dz*19)) * smoothstep(.05, .38, r) * (1 - smoothstep(.68, .92, r))
	return shelf + hill + fault
}

func IslandGrid(island OffshoreIsland) WalkGrid {
	const size, count = 96., 41
	heights := make([]float64, 0, count*count)
	for j := 0; j < count; j++ {
		for i := 0; i < count; i++ {
			heights = append(heights, IslandHeight(island, island.X-size/2+float64(i)*size/(count-1), island.Z-size/2+float64(j)*size/(count-1)))
		}
	}
	return WalkGrid{island.X - size/2, island.Z - size/2, size, size, count, count, heights}
}

// IslandPier describes the top surface of the visible weathered timber jetty.
func IslandPier(island OffshoreIsland) [7]float64 {
	return [7]float64{island.X, 2.6, island.Z + island.RadiusZ*.8 + 10, 3.6, 24, 0, 0}
}

const (
	OffshoreDepthSize = 2048
	OffshoreMinX      = -245.
	OffshoreMaxX      = 355.
	OffshoreMinZ      = -430.
	OffshoreMaxZ      = 50.
)

// OffshoreAssets adds one terrain draw with its atlas, one timber draw and
// one depth map. All counts and extents are finite; the ocean stays unbounded.
func OffshoreAssets(seed int64) (map[string][]byte, error) {
	islands := OffshoreIslands()
	terrain, piers := &geometry{}, &geometry{}
	atlas := image.NewRGBA(image.Rect(0, 0, 256, 256*len(islands)))
	rough := image.NewRGBA(atlas.Bounds())
	const meshCount = 81
	for k, island := range islands {
		first := len(terrain.positions) / 3
		for j := 0; j < meshCount; j++ {
			for i := 0; i < meshCount; i++ {
				u, v := float64(i)/(meshCount-1), float64(j)/(meshCount-1)
				x, z := island.X-48+u*96, island.Z-48+v*96
				appendVertex(terrain, vec3{x, IslandHeight(island, x, z), z}, u, (float64(k)+v)/float64(len(islands)))
			}
		}
		for j := 0; j < meshCount-1; j++ {
			for i := 0; i < meshCount-1; i++ {
				a := uint16(first + j*meshCount + i)
				b := a + 1
				c := a + meshCount
				d := c + 1
				terrain.indices = append(terrain.indices, a, c, b, b, c, d)
			}
		}
		for j := 0; j < 256; j++ {
			for i := 0; i < 256; i++ {
				x, z := island.X-48+float64(i)*96/255, island.Z-48+float64(j)*96/255
				h := IslandHeight(island, x, z)
				dx := IslandHeight(island, x+.3, z) - IslandHeight(island, x-.3, z)
				dz := IslandHeight(island, x, z+.3) - IslandHeight(island, x, z-.3)
				slope := math.Hypot(dx, dz) / .6
				grain := .94 + .06*math.Sin(x*18+z*13)*math.Sin(z*23)
				c := color.RGBA{53, 57, 54, 255}
				if slope > .55 {
					c = color.RGBA{40, 45, 47, 255}
				} else if h > 3 && k == 2 {
					c = color.RGBA{65, 77, 56, 255}
				}
				wet := .6 + .4*smoothstep(.02, 1.2, h)
				c.R = uint8(float64(c.R) * grain * wet)
				c.G = uint8(float64(c.G) * grain * wet)
				c.B = uint8(float64(c.B) * grain * wet)
				atlas.SetRGBA(i, k*256+j, c)
				rough.SetRGBA(i, k*256+j, color.RGBA{0, uint8(85 + 150*smoothstep(0, 1.2, h)), 0, 255})
			}
		}
		pier := IslandPier(island)
		for z := pier[2] - pier[4]/2; z < pier[2]+pier[4]/2; z += .72 {
			shipBox(piers, pier[0], pier[1]-.1, z, pier[3], .2, .68)
		}
		for z := pier[2] - 9; z <= pier[2]+9; z += 6 {
			for _, side := range []float64{-1, 1} {
				shipTube(piers, vec3{pier[0] + side*1.5, -7, z}, vec3{pier[0] + side*1.5, 3.3, z}, .15, .12, 6)
			}
		}
	}
	averageVertexNormals(terrain)
	albedo, err := encodePNG(atlas)
	if err != nil {
		return nil, err
	}
	mr, err := encodePNG(rough)
	if err != nil {
		return nil, err
	}
	material := map[string]any{"pbrMetallicRoughness": map[string]any{"baseColorTexture": map[string]any{"index": 0}, "metallicRoughnessTexture": map[string]any{"index": 1}, "metallicFactor": 0., "roughnessFactor": 1.}}
	land, err := writeGLB(terrain, material, []embeddedImage{{name: "island-surface", data: albedo}, {name: "island-wetness", data: mr}})
	if err != nil {
		return nil, err
	}
	timber, err := writeGLB(piers, solidMaterial([4]float64{srgbLinear(.27), srgbLinear(.24), srgbLinear(.19), 1}, .8, 0), nil)
	if err != nil {
		return nil, err
	}
	depth, err := offshoreDepth(seed, islands)
	if err != nil {
		return nil, err
	}
	return map[string][]byte{"offshore-islands.glb": land, "offshore-piers.glb": timber, "offshore-depth.png": depth}, nil
}

func offshoreDepth(seed int64, islands []OffshoreIsland) ([]byte, error) {
	// Resample the existing high-detail coastal bathymetry, including fractured
	// stacks, rather than replacing its sea-level silhouettes with plain terrain.
	coastal, err := makeBathymetry(newNoise(seed))
	if err != nil {
		return nil, err
	}
	coast, err := png.Decode(bytes.NewReader(coastal))
	if err != nil {
		return nil, err
	}
	out := image.NewGray(image.Rect(0, 0, OffshoreDepthSize, OffshoreDepthSize))
	for j := 0; j < OffshoreDepthSize; j++ {
		for i := 0; i < OffshoreDepthSize; i++ {
			x := OffshoreMinX + (float64(i)+.5)/OffshoreDepthSize*(OffshoreMaxX-OffshoreMinX)
			z := OffshoreMinZ + (float64(j)+.5)/OffshoreDepthSize*(OffshoreMaxZ-OffshoreMinZ)
			value := uint8(0)
			if x >= BathymetryMinX && x <= BathymetryMaxX && z >= BathymetryMinZ && z <= BathymetryMaxZ {
				cx := int((x - BathymetryMinX) / (BathymetryMaxX - BathymetryMinX) * BathymetrySize)
				cz := int((z - BathymetryMinZ) / (BathymetryMaxZ - BathymetryMinZ) * BathymetrySize)
				value = color.GrayModel.Convert(coast.At(min(cx, BathymetrySize-1), min(cz, BathymetrySize-1))).(color.Gray).Y
			}
			for _, island := range islands {
				if math.Abs(x-island.X) <= 48 && math.Abs(z-island.Z) <= 48 {
					value = max(value, uint8(math.Round(BathymetryEncode(IslandHeight(island, x, z))*255)))
				}
			}
			out.Pix[j*out.Stride+i] = value
		}
	}
	return encodePNG(out)
}

// OffshoreRockPlacement reuses the coast's fractured stack mesh at an island
// ridge. The shared source is fetched once; its collider transforms match.
func OffshoreRockPlacement(island OffshoreIsland) [6]float64 {
	height := .8
	if island.ID == "bell-rock" {
		height = 1.1
	}
	if island.ID == "starglass" {
		height = .65
	}
	return [6]float64{island.X, IslandHeight(island, island.X, island.Z-8) - 1, island.Z - 8, .32, height, .32}
}

func OffshoreRockColliders(island OffshoreIsland) []WalkShape {
	pose := OffshoreRockPlacement(island)
	shapes := WalkColliders(Seed)[:len(stackSpecs(Seed))]
	for i := range shapes {
		s := &shapes[i]
		s.X = pose[0] + s.X*pose[3]
		s.Y = pose[1] + s.Y*pose[4]
		s.Z = pose[2] + s.Z*pose[5]
		s.Radius *= math.Max(pose[3], pose[5])
		s.Height *= pose[4]
	}
	return shapes
}
