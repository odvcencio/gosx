package beachgen

import "math"

func shipBox(g *geometry, x, y, z, sx, sy, sz float64) {
	a, b, c, d := vec3{x - sx/2, y - sy/2, z - sz/2}, vec3{x + sx/2, y - sy/2, z - sz/2}, vec3{x + sx/2, y + sy/2, z - sz/2}, vec3{x - sx/2, y + sy/2, z - sz/2}
	e, f, h, j := vec3{x - sx/2, y - sy/2, z + sz/2}, vec3{x + sx/2, y - sy/2, z + sz/2}, vec3{x + sx/2, y + sy/2, z + sz/2}, vec3{x - sx/2, y + sy/2, z + sz/2}
	addQuad(g, a, d, c, b, vec3{0, 0, -1})
	addQuad(g, e, f, h, j, vec3{0, 0, 1})
	addQuad(g, d, j, h, c, vec3{0, 1, 0})
	addQuad(g, a, b, f, e, vec3{0, -1, 0})
	addQuad(g, a, e, j, d, vec3{-1, 0, 0})
	addQuad(g, b, c, h, f, vec3{1, 0, 0})
}

func shipTube(g *geometry, a, b vec3, r0, r1 float64, sides int) {
	axis := normalize(vec3{b.x - a.x, b.y - a.y, b.z - a.z})
	side := normalize(vec3{axis.z, 0, -axis.x})
	if math.Hypot(axis.x, axis.z) < .001 {
		side = vec3{1, 0, 0}
	}
	up := vec3{axis.y*side.z - axis.z*side.y, axis.z*side.x - axis.x*side.z, axis.x*side.y - axis.y*side.x}
	for i := 0; i < sides; i++ {
		angle, next := 2*math.Pi*float64(i)/float64(sides), 2*math.Pi*float64(i+1)/float64(sides)
		n := vec3{side.x*math.Cos(angle) + up.x*math.Sin(angle), side.y*math.Cos(angle) + up.y*math.Sin(angle), side.z*math.Cos(angle) + up.z*math.Sin(angle)}
		m := vec3{side.x*math.Cos(next) + up.x*math.Sin(next), side.y*math.Cos(next) + up.y*math.Sin(next), side.z*math.Cos(next) + up.z*math.Sin(next)}
		addQuad(g, vec3{a.x + n.x*r0, a.y + n.y*r0, a.z + n.z*r0}, vec3{b.x + n.x*r1, b.y + n.y*r1, b.z + n.z*r1}, vec3{b.x + m.x*r1, b.y + m.y*r1, b.z + m.z*r1}, vec3{a.x + m.x*r0, a.y + m.y*r0, a.z + m.z*r0}, normalize(vec3{n.x + m.x, n.y + m.y, n.z + m.z}))
	}
}

func hullWidth(z float64) float64 {
	// Fine entrance, broad shoulders, narrowing stern and a high clipper bow.
	t := (z + 11) / 22
	return 2.5*math.Pow(math.Max(0, math.Sin(t*math.Pi)), .6)*(1-.12*t) + .04
}
func hullSheer(z float64) float64 { return .55*math.Pow(math.Abs(z)/11, 3) + .15*math.Max(0, -z/11) }

func shipHull(stations, profile int) (hull, deck, trim *geometry) {
	hull, deck, trim = &geometry{}, &geometry{}, &geometry{}
	for station := 0; station <= stations; station++ {
		z := -11 + 22*float64(station)/float64(stations)
		w := hullWidth(z)
		sheer := hullSheer(z)
		for j := 0; j <= profile; j++ {
			a := -math.Pi/2 + math.Pi*float64(j)/float64(profile)
			y := -1.5 + 4*math.Pow(math.Abs(math.Sin(a)), .8) + sheer
			x := w * math.Sin(a) * (1 - .16*math.Pow(math.Abs(math.Sin(a)), 4))
			appendVertex(hull, vec3{x, y, z}, float64(j)/float64(profile), float64(station)/float64(stations))
		}
	}
	for i := 0; i < stations; i++ {
		for j := 0; j < profile; j++ {
			a := uint16(i*(profile+1) + j)
			b := a + 1
			c := a + uint16(profile+1)
			d := c + 1
			hull.indices = append(hull.indices, a, b, c, b, d, c)
		}
	}
	averageVertexNormals(hull)
	for i := 0; i < stations; i++ {
		z := -11 + 22*float64(i)/float64(stations)
		next := -11 + 22*float64(i+1)/float64(stations)
		w, nw := hullWidth(z)*.84, hullWidth(next)*.84
		// The walkable main deck is level; forecastle and poop rise along the sheer.
		addQuad(deck, vec3{-w, 2.5, z}, vec3{-nw, 2.5, next}, vec3{nw, 2.5, next}, vec3{w, 2.5, z}, vec3{0, 1, 0})
		for _, side := range []float64{-1, 1} {
			a, b := vec3{w * side, 2.5 + hullSheer(z), z}, vec3{nw * side, 2.5 + hullSheer(next), next}
			shipTube(trim, a, b, .07, .07, 4)
			// Cream-painted wale follows the waterline and emphasizes the sheer.
			a.y -= .55
			b.y -= .55
			shipTube(trim, a, b, .045, .045, 4)
		}
	}
	return
}

func shipSail(z, top, width, height float64, cols, rows int) *geometry {
	g := &geometry{}
	for row := 0; row <= rows; row++ {
		for col := 0; col <= cols; col++ {
			u, v := float64(col)/float64(cols), float64(row)/float64(rows)
			x := (u - .5) * width * (1 - .12*v)
			billow := .6*math.Sin(u*math.Pi)*math.Sin(v*math.Pi) + .045*math.Sin(u*math.Pi*8)*math.Sin(v*math.Pi)
			id := appendVertex(g, vec3{x, top - height*v + .12*math.Sin(u*math.Pi), z + billow}, u, v)
			g.normals[int(id)*3+2] = 1
		}
	}
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			a := uint16(row*(cols+1) + col)
			b := a + 1
			c := a + uint16(cols+1)
			d := c + 1
			g.indices = append(g.indices, a, c, b, b, c, d)
		}
	}
	averageVertexNormals(g)
	return g
}
