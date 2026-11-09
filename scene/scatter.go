package scene

import (
	"math"
	"strings"

	"m31labs.dev/gosx/det"
)

// Scatter places vegetation and props over a Terrain with a seeded,
// deterministic layout, and draws them through the ordinary InstancedMesh
// path, so GPU-driven culling (Props.GPUDriven), shadows and the instance
// stream all apply.
//
// Placement uses a jittered grid per species: the region is cut into squares
// of 1/sqrt(Density) meters and each square gets one candidate at a hashed
// offset. A candidate survives when the ground passes the species' height and
// slope masks, it is above WaterLevel, it is off every Path, and a hashed draw
// falls under the product of the scatter and species Mask values. Every random
// number is an integer hash of (Seed, species, square), and every float product
// is rounded explicitly, so the same Scatter reproduces the same placement bit
// for bit on amd64, arm64 and js/wasm. Placements returns that layout for
// gameplay (collision, harvesting, AI).
//
// For level of detail the region is also cut into CellSize squares. Each cell
// lowers to one InstancedMesh batch per species level, tagged with a distance
// band; the browser draws the batch whose band holds the camera's distance to
// the cell centre, and nothing beyond the last band. WebGL2 draws the same
// batches.
type Scatter struct {
	// ID prefixes every lowered batch ID. Empty means "scatter".
	ID string
	// Terrain is the ground to place on. Required.
	Terrain *Terrain
	// Seed selects the layout. The same Seed and fields give the same layout.
	Seed uint64
	// RegionMin and RegionMax bound placement in world XZ (Y is ignored).
	// Both zero means the whole terrain.
	RegionMin Vector3
	RegionMax Vector3
	// WaterLevel, when set, rejects ground lower than WaterLevel + WaterMargin.
	WaterLevel  *float64
	WaterMargin float64
	// Mask scales every species' density at (x, z): 0 removes, 1 keeps. It must
	// be a pure function of its arguments. Nil is 1 everywhere.
	Mask func(x, z float64) float64
	// Paths clear lanes through the scatter.
	Paths []ScatterPath
	// CellSize is the side, in meters, of one LOD batch cell. Zero means 64.
	CellSize float64
	// Species are placed independently, in order.
	Species []ScatterSpecies
}

// ScatterPath clears every instance within Width/2 of the polyline through
// Points (world XZ; Y is ignored).
type ScatterPath struct {
	Points []Vector3
	Width  float64
}

// ScatterSpecies is one kind of instance: grass, a shrub, a tree, a rock.
type ScatterSpecies struct {
	// Name labels the species in batch IDs. Empty uses its index.
	Name string
	// Density is candidates per square meter before masks. Zero places none.
	Density float64
	// MinHeight and MaxHeight bound the ground's world Y. Nil leaves a side open.
	MinHeight *float64
	MaxHeight *float64
	// MinSlope and MaxSlope bound the ground slope in degrees. Zero MaxSlope
	// means 90.
	MinSlope float64
	MaxSlope float64
	// Mask scales this species' density at (x, z), as Scatter.Mask does.
	Mask func(x, z float64) float64
	// MinScale and MaxScale bound the uniform random scale. Zero means 1.
	MinScale float64
	MaxScale float64
	// Align tilts instances toward the ground normal: 0 stands them upright,
	// 1 follows the slope.
	Align float64
	// Sink lowers each instance into the ground, in meters, so roots hide on
	// slopes.
	Sink float64
	// LODs are the drawn levels, nearest first. LODs[i].Distance is the
	// farthest camera distance for that level; the last level with Distance 0
	// draws at every distance. At least one level is required.
	LODs []ScatterLOD
	// Billboard optionally adds a far level of two crossed cards past the last
	// LOD distance.
	Billboard *ScatterBillboard
	// CastShadow and ReceiveShadow apply to every level of the species.
	CastShadow    bool
	ReceiveShadow bool
	// Wind is the sway amount, 0 (rigid) to 1 (grass), that Environment.Wind
	// applies in the vertex stage. See wind.go.
	Wind float64
	// WindHeight is the instance-space height, in meters, at which sway reaches
	// full strength; vertices below the instance origin never move. Zero means 1.
	WindHeight float64
}

// ScatterLOD is one level of a species. Geometry and Material are the same
// values an InstancedMesh takes. The instance origin sits on the ground, so
// lift a centred primitive with OffsetY.
type ScatterLOD struct {
	Distance float64
	Geometry Geometry
	Material Material
	// OffsetY lifts the geometry along the instance up axis, in meters before
	// scale. Use Height/2 for a centred primitive such as a box or a cone.
	OffsetY float64
	// Keep thins the level to this share of the species' instances, 0..1.
	// Zero keeps every instance.
	Keep float64
}

// ScatterBillboard is the far level: two crossed, upright cards of
// Width x Height, drawn from the last LOD distance out to Distance.
type ScatterBillboard struct {
	Distance float64
	Width    float64
	Height   float64
	Material Material
}

// ScatterInstance is one placed instance.
type ScatterInstance struct {
	Species  int
	Position Vector3
	// Yaw is the rotation about the instance up axis, in radians.
	Yaw   float64
	Scale float64
	// Up is the instance's up axis: straight up blended toward the ground
	// normal by Species.Align.
	Up Vector3
}

const defaultScatterCellSize = 64.0

func (Scatter) sceneNode() {}

// scatterHash returns a uniform float in [0, 1) from the scatter seed and
// three integer coordinates.
func scatterHash(seed uint64, species int, x, z int64, salt uint64) float64 {
	h := mix64(seed ^ mix64(uint64(species)*0x9e3779b97f4a7c15+salt) ^ mix64(uint64(x)*0xbf58476d1ce4e5b9) ^ mix64(uint64(z)*0x94d049bb133111eb+0x632be59bd9b4e019))
	return float64(h>>11) / float64(uint64(1)<<53)
}

// scatterRegion resolves the placement rectangle.
func (s Scatter) scatterRegion(f terrainFrame) (minX, minZ, maxX, maxZ float64) {
	minX, minZ, maxX, maxZ = f.minX, f.minZ, f.minX+f.sizeX, f.minZ+f.sizeZ
	if s.RegionMin != (Vector3{}) || s.RegionMax != (Vector3{}) {
		minX, maxX = math.Max(minX, math.Min(s.RegionMin.X, s.RegionMax.X)), math.Min(maxX, math.Max(s.RegionMin.X, s.RegionMax.X))
		minZ, maxZ = math.Max(minZ, math.Min(s.RegionMin.Z, s.RegionMax.Z)), math.Min(maxZ, math.Max(s.RegionMin.Z, s.RegionMax.Z))
	}
	return minX, minZ, maxX, maxZ
}

func scatterOnPath(paths []ScatterPath, x, z float64) bool {
	for _, path := range paths {
		half := path.Width / 2
		if half <= 0 || len(path.Points) == 0 {
			continue
		}
		limit := float64(half * half)
		for i := range path.Points {
			a := path.Points[i]
			b := a
			if i+1 < len(path.Points) {
				b = path.Points[i+1]
			} else if len(path.Points) > 1 {
				continue
			}
			dx, dz := b.X-a.X, b.Z-a.Z
			t := 0.0
			if span := float64(dx*dx) + float64(dz*dz); span > 0 {
				t = clampFloat((float64((x-a.X)*dx)+float64((z-a.Z)*dz))/span, 0, 1)
			}
			px := a.X + float64(t*dx) - x
			pz := a.Z + float64(t*dz) - z
			if float64(px*px)+float64(pz*pz) <= limit {
				return true
			}
		}
	}
	return false
}

func scatterMask(mask func(x, z float64) float64, x, z float64) float64 {
	if mask == nil {
		return 1
	}
	value := mask(x, z)
	if math.IsNaN(value) {
		return 0
	}
	return clampFloat(value, 0, 1)
}

// Placements returns every instance, species by species and, within a
// species, row by row over the jitter grid. A scatter without a valid Terrain
// places nothing.
func (s Scatter) Placements() []ScatterInstance {
	f, ok := s.Terrain.frame()
	if !ok {
		return nil
	}
	minX, minZ, maxX, maxZ := s.scatterRegion(f)
	if !(maxX > minX) || !(maxZ > minZ) {
		return nil
	}
	var out []ScatterInstance
	for index, species := range s.Species {
		if !(species.Density > 0) || len(species.LODs) == 0 {
			continue
		}
		step := 1 / math.Sqrt(species.Density)
		columns := int64(math.Ceil((maxX - minX) / step))
		rows := int64(math.Ceil((maxZ - minZ) / step))
		if columns*rows > 4_000_000 {
			continue // A density this high would stall the server; reject it.
		}
		minSlope := clampFloat(species.MinSlope, 0, 90)
		maxSlope := species.MaxSlope
		if maxSlope <= 0 || maxSlope > 90 {
			maxSlope = 90
		}
		// Compare normal.Y against cosines so no arc cosine runs per candidate.
		cosMin := det.Cos(minSlope * math.Pi / 180)
		cosMax := det.Cos(maxSlope * math.Pi / 180)
		minScale, maxScale := species.MinScale, species.MaxScale
		if minScale <= 0 {
			minScale = 1
		}
		if maxScale <= 0 {
			maxScale = minScale
		}
		align := clampFloat(species.Align, 0, 1)
		for row := int64(0); row < rows; row++ {
			for column := int64(0); column < columns; column++ {
				x := minX + float64((float64(column)+scatterHash(s.Seed, index, column, row, 1))*step)
				z := minZ + float64((float64(row)+scatterHash(s.Seed, index, column, row, 2))*step)
				if x >= maxX || z >= maxZ {
					continue
				}
				keep := float64(scatterMask(s.Mask, x, z) * scatterMask(species.Mask, x, z))
				if keep <= 0 || scatterHash(s.Seed, index, column, row, 3) >= keep {
					continue
				}
				if scatterOnPath(s.Paths, x, z) {
					continue
				}
				height := f.height(x, z)
				if s.WaterLevel != nil && height < *s.WaterLevel+s.WaterMargin {
					continue
				}
				if (species.MinHeight != nil && height < *species.MinHeight) || (species.MaxHeight != nil && height > *species.MaxHeight) {
					continue
				}
				normal := f.normal(x, z)
				if normal.Y > cosMin || normal.Y < cosMax {
					continue
				}
				up := terrainNormalize(Vector3{
					X: float64(align * normal.X),
					Y: 1 + float64(align*(normal.Y-1)),
					Z: float64(align * normal.Z),
				})
				scale := minScale + float64(scatterHash(s.Seed, index, column, row, 4)*(maxScale-minScale))
				out = append(out, ScatterInstance{
					Species:  index,
					Position: Vector3{X: x, Y: height - species.Sink, Z: z},
					Yaw:      float64(scatterHash(s.Seed, index, column, row, 5) * (2 * math.Pi)),
					Scale:    scale,
					Up:       up,
				})
			}
		}
	}
	return out
}

// scatterMatrix is the column-major instance transform: the yaw about up,
// scaled, lifted by offsetY along up.
func scatterMatrix(instance ScatterInstance, offsetY float64, upright bool, extraYaw float64) [16]float64 {
	up := instance.Up
	if upright {
		up = Vector3{Y: 1}
	}
	sin, cos := det.Sin(instance.Yaw+extraYaw), det.Cos(instance.Yaw+extraYaw)
	// Forward on the ground plane, then made perpendicular to up.
	fx, fz := sin, cos
	dot := float64(fx*up.X) + float64(fz*up.Z)
	forward := terrainNormalize(Vector3{X: fx - float64(dot*up.X), Y: -float64(dot * up.Y), Z: fz - float64(dot*up.Z)})
	right := Vector3{
		X: float64(up.Y*forward.Z) - float64(up.Z*forward.Y),
		Y: float64(up.Z*forward.X) - float64(up.X*forward.Z),
		Z: float64(up.X*forward.Y) - float64(up.Y*forward.X),
	}
	s := instance.Scale
	lift := float64(offsetY * s)
	return [16]float64{
		float64(right.X * s), float64(right.Y * s), float64(right.Z * s), 0,
		float64(up.X * s), float64(up.Y * s), float64(up.Z * s), 0,
		float64(forward.X * s), float64(forward.Y * s), float64(forward.Z * s), 0,
		instance.Position.X + float64(up.X*lift), instance.Position.Y + float64(up.Y*lift), instance.Position.Z + float64(up.Z*lift), 1,
	}
}

// scatterBand is one drawn level of a species in one cell.
type scatterBand struct {
	level     int
	min, max  float64
	geometry  Geometry
	material  Material
	offsetY   float64
	keep      float64
	billboard bool
}

func scatterBands(species ScatterSpecies) []scatterBand {
	var bands []scatterBand
	previous := 0.0
	for i, lod := range species.LODs {
		if lod.Geometry == nil {
			continue
		}
		far := lod.Distance
		if far <= previous && i < len(species.LODs)-1 {
			continue
		}
		if far <= previous {
			far = 0
		}
		bands = append(bands, scatterBand{level: len(bands), min: previous, max: far, geometry: lod.Geometry, material: lod.Material, offsetY: lod.OffsetY, keep: lod.Keep})
		if far == 0 {
			return bands
		}
		previous = far
	}
	if b := species.Billboard; b != nil && b.Distance > previous && b.Width > 0 && b.Height > 0 {
		bands = append(bands, scatterBand{level: len(bands), min: previous, max: b.Distance, geometry: BoxGeometry{Width: b.Width, Height: b.Height, Depth: 0.02},
			material: b.Material, offsetY: b.Height / 2, billboard: true})
	}
	return bands
}

// lowerScatter places the scatter and appends one compressed InstancedMesh
// batch per (species, cell, level).
func (l *graphLowerer) lowerScatter(s Scatter) {
	f, ok := s.Terrain.frame()
	if !ok {
		return
	}
	placements := s.Placements()
	if len(placements) == 0 {
		return
	}
	id := strings.TrimSpace(s.ID)
	if id == "" {
		id = "scatter"
	}
	cellSize := s.CellSize
	if cellSize <= 0 {
		cellSize = defaultScatterCellSize
	}
	minX, minZ, _, _ := s.scatterRegion(f)
	type cellKey struct{ species, cx, cz int }
	cells := map[cellKey][]int{}
	var order []cellKey
	unbanded := make([]bool, len(s.Species))
	for i, species := range s.Species {
		bands := scatterBands(species)
		unbanded[i] = len(bands) == 1 && bands[0].max == 0
	}
	for i, instance := range placements {
		key := cellKey{instance.Species, int(math.Floor((instance.Position.X - minX) / cellSize)), int(math.Floor((instance.Position.Z - minZ) / cellSize))}
		if unbanded[instance.Species] {
			// One unbounded level draws at every distance: one batch, no cells.
			key.cx, key.cz = 0, 0
		}
		if _, seen := cells[key]; !seen {
			order = append(order, key)
		}
		cells[key] = append(cells[key], i)
	}
	for _, key := range order {
		species := s.Species[key.species]
		name := strings.TrimSpace(species.Name)
		if name == "" {
			name = intString(key.species)
		}
		bands := scatterBands(species)
		members := cells[key]
		// The cell centre the browser measures LOD distance to.
		centerX := minX + (float64(key.cx)+0.5)*cellSize
		centerZ := minZ + (float64(key.cz)+0.5)*cellSize
		centerY := f.height(centerX, centerZ)
		staticSpecies := unbanded[key.species]
		group := id + "/" + name + "/" + intString(key.cx) + "," + intString(key.cz)
		for _, band := range bands {
			copies := 1
			if band.billboard {
				copies = 2
			}
			transforms := make([]float64, 0, len(members)*16*copies)
			for n, index := range members {
				if band.keep > 0 && band.keep < 1 && scatterHash(s.Seed, key.species, int64(n), int64(band.level), 9) >= band.keep {
					continue
				}
				for c := 0; c < copies; c++ {
					m := scatterMatrix(placements[index], band.offsetY, band.billboard, float64(c)*math.Pi/2)
					transforms = append(transforms, m[:]...)
				}
			}
			count := len(transforms) / 16
			if count == 0 {
				continue
			}
			start := len(l.instancedMeshes)
			l.lowerInstancedMesh(InstancedMesh{
				ID:            group + "/" + intString(band.level),
				Count:         0,
				Geometry:      band.geometry,
				Material:      band.material,
				CastShadow:    species.CastShadow,
				ReceiveShadow: species.ReceiveShadow,
			}, identityTransform())
			if len(l.instancedMeshes) == start {
				continue
			}
			record := &l.instancedMeshes[start]
			record.Count = count
			record.Transforms = nil
			if count >= 2 {
				record.CompressedTransforms = compressDeinterleavedTransforms(transforms, 8)
				record.TransformStride = 16
			} else {
				record.Transforms = transforms
			}
			if !staticSpecies {
				record.LODGroup = group
				record.LODLevel = band.level
				record.LODMinDistance = band.min
				record.LODMaxDistance = band.max
				record.X, record.Y, record.Z = centerX, centerY, centerZ
			}
			if species.Wind > 0 {
				height := species.WindHeight
				if height <= 0 {
					height = 1
				}
				record.Wind = &InstancedWindIR{Sway: clampFloat(species.Wind, 0, 1), Height: height}
			}
		}
	}
}
