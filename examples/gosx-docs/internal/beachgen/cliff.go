package beachgen

import "math"

func addCliffWall(g *geometry, spec cliffSpec) {
	const columns, rows, topRows = 72, 48, 12
	const baseY, plateauDepth = -6.0, 12.0
	noise := newNoise(spec.seed)
	lengths := [2]float64{}
	totalLength := 0.0
	for i := range lengths {
		a, b := spec.front[i], spec.front[i+1]
		lengths[i] = math.Hypot(b.x-a.x, b.z-a.z)
		totalLength += lengths[i]
	}

	front := make([][]uint16, rows)
	for row := range front {
		front[row] = make([]uint16, columns)
	}
	frontPoints := make([][]vec3, rows)
	for row := range frontPoints {
		frontPoints[row] = make([]vec3, columns)
	}
	tops := make([]float64, columns)
	frontNormals := make([]vec3, columns)
	arcs := make([]float64, columns)
	for column := 0; column < columns; column++ {
		t := float64(column) / float64(columns-1)
		arc := t * totalLength
		arcs[column] = arc
		segment, local := 0, arc
		if local > lengths[0] {
			segment, local = 1, local-lengths[0]
		}
		a, b := spec.front[segment], spec.front[segment+1]
		fraction := local / lengths[segment]
		x := lerp(a.x, b.x, fraction)
		z := lerp(a.z, b.z, fraction)
		tx, tz := (b.x-a.x)/lengths[segment], (b.z-a.z)/lengths[segment]
		normal := normalize(vec3{tz * spec.face, 0, -tx * spec.face})
		frontNormals[column] = normal
		crownNoise := fbm3(noise, x/5+3, 2.7, z/5-8, 4)
		tops[column] = spec.height + (1-2*math.Abs(crownNoise))*3
		for row := 0; row < rows; row++ {
			v := float64(row) / float64(rows-1)
			y := lerp(baseY, tops[column], v)
			lumps := fbm3(noise, x/5, y/5, z/5, 4)
			ridges := ridgeFbm3(noise, x/2+5, y/2, z/2+9, 4)
			cracks := fbm3(noise, x*.25+11, y*.07, z*.25+7, 3)
			strata := .12 * math.Sin(y*1.3+2*noiseValue3(noise, x*.15, y*.15, z*.15))
			notch := smoothstep(-.5, -.15, y) * (1 - smoothstep(1.05, 1.4, y))
			talus := 1 - smoothstep(baseY, baseY+1.5, y)
			displacement := .35*lumps + .18*ridges - .2*math.Abs(cracks) + strata - .8*notch + 1.2*talus
			p := vec3{x + normal.x*displacement, y, z + normal.z*displacement}
			frontPoints[row][column] = p
			front[row][column] = appendVertex(g, p, fract(arc/8), fract(y/8))
		}
	}

	for row := 0; row < rows-1; row++ {
		for column := 0; column < columns-1; column++ {
			a := front[row][column]
			b := front[row][column+1]
			c := front[row+1][column+1]
			d := front[row+1][column]
			if spec.face > 0 {
				g.indices = append(g.indices, a, d, c, a, c, b)
			} else {
				g.indices = append(g.indices, a, b, c, a, c, d)
			}
		}
	}

	// The front's upper edge and these rows form a broad, gently uneven cap.
	cap := make([][]uint16, topRows)
	for row := range cap {
		cap[row] = make([]uint16, columns)
		depth := plateauDepth * float64(row) / float64(topRows-1)
		for column := 0; column < columns; column++ {
			if row == 0 {
				cap[row][column] = front[rows-1][column]
				continue
			}
			p := frontPoints[rows-1][column]
			p.x -= frontNormals[column].x * depth
			p.z -= frontNormals[column].z * depth
			plateauNoise := fbm3(noise, p.x/7, tops[column]/7, p.z/7, 3)
			p.y = tops[column] + .35*plateauNoise*smoothstep(0, 2, depth)
			cap[row][column] = appendVertex(g, p, fract(arcs[column]/8), fract(p.y/8))
		}
	}
	for row := 0; row < topRows-1; row++ {
		for column := 0; column < columns-1; column++ {
			a := cap[row][column]
			b := cap[row][column+1]
			c := cap[row+1][column+1]
			d := cap[row+1][column]
			// Coastline runs toward +Z; inland is opposite the cove-facing normal.
			if spec.face > 0 {
				g.indices = append(g.indices, a, d, b, b, d, c)
			} else {
				g.indices = append(g.indices, a, b, d, b, c, d)
			}
		}
	}
}

func fract(value float64) float64 { return value - math.Floor(value) }
