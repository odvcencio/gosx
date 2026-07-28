package meshsimplify

import "math"

// ErrorStats reports how far a simplified mesh moved away from its source.
// The measurement is one-sided: every source vertex finds its closest point
// on the simplified surface.
type ErrorStats struct {
	MaxDistance float64 `json:"maxDistance"`
	RMSDistance float64 `json:"rmsDistance"`
	// MaxFraction and RMSFraction divide the distances by the bounding box
	// diagonal, so they compare across models.
	MaxFraction float64 `json:"maxFraction"`
	RMSFraction float64 `json:"rmsFraction"`
	SamplePoint int     `json:"samplePoints"`
}

// MeasureError returns the distance from every vertex of source to the
// closest point on the surface of simplified. A uniform grid over the
// simplified triangles keeps the search near linear, and the shell walk keeps
// the answer exact for the grid.
func MeasureError(source, simplified Mesh) ErrorStats {
	stats := ErrorStats{SamplePoint: source.VertexCount()}
	if source.VertexCount() == 0 || simplified.TriangleCount() == 0 {
		return stats
	}
	positions := make([]float64, len(simplified.Positions))
	for i, value := range simplified.Positions {
		positions[i] = float64(value)
	}
	grid := buildTriangleGrid(positions, simplified.Indices)
	diagonal := boundingBoxDiagonalFloat32(source.Positions)

	var sumSquares float64
	for i := 0; i < source.VertexCount(); i++ {
		px := float64(source.Positions[i*3])
		py := float64(source.Positions[i*3+1])
		pz := float64(source.Positions[i*3+2])
		distance := grid.nearest(px, py, pz)
		if distance > stats.MaxDistance {
			stats.MaxDistance = distance
		}
		sumSquares += distance * distance
	}
	stats.RMSDistance = math.Sqrt(sumSquares / float64(source.VertexCount()))
	if diagonal > 0 {
		stats.MaxFraction = stats.MaxDistance / diagonal
		stats.RMSFraction = stats.RMSDistance / diagonal
	}
	return stats
}

type triangleGrid struct {
	positions []float64
	indices   []uint32
	cells     map[[3]int32][]int32
	cellSize  float64
	minX      float64
	minY      float64
	minZ      float64
}

func buildTriangleGrid(positions []float64, indices []uint32) *triangleGrid {
	grid := &triangleGrid{positions: positions, indices: indices, cells: map[[3]int32][]int32{}}
	minX, minY, minZ := math.Inf(1), math.Inf(1), math.Inf(1)
	maxX, maxY, maxZ := math.Inf(-1), math.Inf(-1), math.Inf(-1)
	for i := 0; i < len(positions); i += 3 {
		minX = math.Min(minX, positions[i])
		minY = math.Min(minY, positions[i+1])
		minZ = math.Min(minZ, positions[i+2])
		maxX = math.Max(maxX, positions[i])
		maxY = math.Max(maxY, positions[i+1])
		maxZ = math.Max(maxZ, positions[i+2])
	}
	diagonal := math.Sqrt((maxX-minX)*(maxX-minX) + (maxY-minY)*(maxY-minY) + (maxZ-minZ)*(maxZ-minZ))
	triangles := len(indices) / 3
	divisions := math.Cbrt(float64(triangles))
	if divisions < 1 {
		divisions = 1
	}
	grid.cellSize = diagonal / divisions
	if grid.cellSize <= 0 {
		grid.cellSize = 1
	}
	grid.minX, grid.minY, grid.minZ = minX, minY, minZ

	for t := 0; t < triangles; t++ {
		i0, i1, i2 := indices[t*3], indices[t*3+1], indices[t*3+2]
		lowX := math.Min(positions[i0*3], math.Min(positions[i1*3], positions[i2*3]))
		highX := math.Max(positions[i0*3], math.Max(positions[i1*3], positions[i2*3]))
		lowY := math.Min(positions[i0*3+1], math.Min(positions[i1*3+1], positions[i2*3+1]))
		highY := math.Max(positions[i0*3+1], math.Max(positions[i1*3+1], positions[i2*3+1]))
		lowZ := math.Min(positions[i0*3+2], math.Min(positions[i1*3+2], positions[i2*3+2]))
		highZ := math.Max(positions[i0*3+2], math.Max(positions[i1*3+2], positions[i2*3+2]))
		for cx := grid.cellIndex(lowX, minX); cx <= grid.cellIndex(highX, minX); cx++ {
			for cy := grid.cellIndex(lowY, minY); cy <= grid.cellIndex(highY, minY); cy++ {
				for cz := grid.cellIndex(lowZ, minZ); cz <= grid.cellIndex(highZ, minZ); cz++ {
					key := [3]int32{cx, cy, cz}
					grid.cells[key] = append(grid.cells[key], int32(t))
				}
			}
		}
	}
	return grid
}

func (g *triangleGrid) cellIndex(value, origin float64) int32 {
	return int32(math.Floor((value - origin) / g.cellSize))
}

// nearest walks outward one shell at a time and stops once no closer triangle
// can exist in a further shell.
func (g *triangleGrid) nearest(px, py, pz float64) float64 {
	cx := g.cellIndex(px, g.minX)
	cy := g.cellIndex(py, g.minY)
	cz := g.cellIndex(pz, g.minZ)
	best := math.Inf(1)
	for radius := int32(0); ; radius++ {
		// A shell at this radius cannot hold a point closer than this bound.
		bound := float64(radius-1) * g.cellSize
		if radius > 0 && bound > best {
			break
		}
		found := false
		for x := cx - radius; x <= cx+radius; x++ {
			for y := cy - radius; y <= cy+radius; y++ {
				for z := cz - radius; z <= cz+radius; z++ {
					if radius > 0 && absInt32(x-cx) != radius && absInt32(y-cy) != radius && absInt32(z-cz) != radius {
						continue
					}
					cell, ok := g.cells[[3]int32{x, y, z}]
					if !ok {
						continue
					}
					found = true
					for _, triangle := range cell {
						distance := g.distanceToTriangle(px, py, pz, triangle)
						if distance < best {
							best = distance
						}
					}
				}
			}
		}
		if !found && radius > 64 && math.IsInf(best, 1) {
			break
		}
	}
	if math.IsInf(best, 1) {
		return 0
	}
	return best
}

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// distanceToTriangle returns the distance from a point to a triangle. The
// routine follows the standard closed-form solution over the seven regions of
// the triangle plane.
func (g *triangleGrid) distanceToTriangle(px, py, pz float64, triangle int32) float64 {
	i0 := g.indices[triangle*3] * 3
	i1 := g.indices[triangle*3+1] * 3
	i2 := g.indices[triangle*3+2] * 3
	ax, ay, az := g.positions[i0], g.positions[i0+1], g.positions[i0+2]
	bx, by, bz := g.positions[i1], g.positions[i1+1], g.positions[i1+2]
	cx, cy, cz := g.positions[i2], g.positions[i2+1], g.positions[i2+2]

	e0x, e0y, e0z := bx-ax, by-ay, bz-az
	e1x, e1y, e1z := cx-ax, cy-ay, cz-az
	dx, dy, dz := ax-px, ay-py, az-pz

	a := e0x*e0x + e0y*e0y + e0z*e0z
	b := e0x*e1x + e0y*e1y + e0z*e1z
	c := e1x*e1x + e1y*e1y + e1z*e1z
	d := e0x*dx + e0y*dy + e0z*dz
	e := e1x*dx + e1y*dy + e1z*dz
	f := dx*dx + dy*dy + dz*dz

	det := a*c - b*b
	s := b*e - c*d
	t := b*d - a*e

	if s+t <= det {
		if s < 0 {
			if t < 0 {
				if d < 0 {
					t = 0
					s = clamp01Scaled(-d, a)
				} else {
					s = 0
					t = clamp01Scaled(-e, c)
				}
			} else {
				s = 0
				t = clamp01Scaled(-e, c)
			}
		} else if t < 0 {
			t = 0
			s = clamp01Scaled(-d, a)
		} else {
			if det <= 0 {
				s, t = 0, 0
			} else {
				invDet := 1 / det
				s *= invDet
				t *= invDet
			}
		}
	} else {
		if s < 0 {
			tmp0 := b + d
			tmp1 := c + e
			if tmp1 > tmp0 {
				numer := tmp1 - tmp0
				denom := a - 2*b + c
				s = clamp01Scaled(numer, denom)
				t = 1 - s
			} else {
				s = 0
				t = clamp01Scaled(-e, c)
			}
		} else if t < 0 {
			tmp0 := b + e
			tmp1 := a + d
			if tmp1 > tmp0 {
				numer := tmp1 - tmp0
				denom := a - 2*b + c
				t = clamp01Scaled(numer, denom)
				s = 1 - t
			} else {
				t = 0
				s = clamp01Scaled(-d, a)
			}
		} else {
			numer := (c + e) - (b + d)
			if numer <= 0 {
				s = 0
			} else {
				denom := a - 2*b + c
				s = clamp01Scaled(numer, denom)
			}
			t = 1 - s
		}
	}

	qx := ax + s*e0x + t*e1x
	qy := ay + s*e0y + t*e1y
	qz := az + s*e0z + t*e1z
	_ = f
	return math.Sqrt((qx-px)*(qx-px) + (qy-py)*(qy-py) + (qz-pz)*(qz-pz))
}

func clamp01Scaled(numerator, denominator float64) float64 {
	if denominator <= 0 {
		return 0
	}
	value := numerator / denominator
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func boundingBoxDiagonalFloat32(positions []float32) float64 {
	converted := make([]float64, len(positions))
	for i, value := range positions {
		converted[i] = float64(value)
	}
	return boundingBoxDiagonal(converted)
}
