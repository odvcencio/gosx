package beachgen

import "math"

// faultPlane interpolates between joint intersections without rounding the
// intervening faces. Large fracture planes belong in the mesh; fine grain
// belongs in the material normal map.
func faultPlane(n noiseField, along, across float64) float64 {
	x, y := math.Floor(along), math.Floor(across)
	u, v := along-x, across-y
	return lerp(lerp(n.lattice(int64(x), int64(y)), n.lattice(int64(x)+1, int64(y)), u),
		lerp(n.lattice(int64(x), int64(y)+1), n.lattice(int64(x)+1, int64(y)+1), u), v)
}

func cliffFracture(n noiseField, y, z float64) (float64, float64) {
	// Unequal fracture blocks intersect inclined lava beds. Each cell is a
	// broad plane, with a recessed and weathered joint at its boundary.
	u, v := (z+.23*y)/5.8, (y-.12*z)/3.8
	ix, iy := int64(math.Floor(u)), int64(math.Floor(v))
	nearest, next := math.Inf(1), math.Inf(1)
	plane := 0.0
	for dz := int64(-1); dz <= 1; dz++ {
		for dy := int64(-1); dy <= 1; dy++ {
			x, h := ix+dz, iy+dy
			du := u - (float64(x) + .5 + .28*n.lattice(x+71, h+13))
			dv := v - (float64(h) + .5 + .28*n.lattice(x+23, h+59))
			d := du*du + dv*dv
			if d < nearest {
				next, nearest = nearest, d
				plane = .8*n.lattice(x+31, h+43) + .25*du*n.lattice(x+11, h+3) + .3*dv
			} else if d < next {
				next = d
			}
		}
	}
	joint := 1 - smoothstep(.015, .2, next-nearest)
	return 2.6*plane - 1.1*joint, .35 * faultPlane(n, u+37, v+19)
}

// stackCorner is an intersection of broad fracture faces. The twelve corners
// carry unequal shoulders and crown heights, rather than a circular tube
// perturbed by surface noise.
func stackCorner(n noiseField, stack stackSpec, corner int, t float64, rim []float64) vec3 {
	const corners = 12
	k := corner % corners
	a := 2*math.Pi*float64(k)/corners + .12*n.lattice(int64(k), 9)
	y := lerp(-6, stack.height, t) - smoothstep(.58, 1, t)*rim[k]
	r := stack.radius * (1.08 - stack.taper*smoothstep(.22, 1, t))
	r *= 1 + .14*n.lattice(int64(k), 13) + .15*faultPlane(n, float64(k)+23, y/(stack.radius*.65)+31)
	// A broken upper shoulder and an unequal wave-cut foot expose distinct
	// masses. Each stack retains its own seeded orientation and proportions.
	r *= 1 - .15*smoothstep(.42, .8, t)*math.Max(0, n.lattice(int64(k), 21))
	r *= 1 - stack.cleftDepth*stackCleft(stack, a)*smoothstep(.3, .6, t)
	r *= 1 - .14*smoothstep(-1, -.2, y)*(1-smoothstep(.8, 1.7, y))
	crown := smoothstep(.5, 1, t)
	shiftX := .12 * stack.radius * crown * n.lattice(7, 43)
	shiftZ := .16 * stack.radius * crown * n.lattice(9, 47)
	return vec3{stack.x + math.Cos(a)*r + (y+6)*stack.lean + shiftX, y,
		stack.z + math.Sin(a)*r*(.8+.1*n.lattice(3, 7)) + shiftZ}
}

func stackCleft(stack stackSpec, angle float64) float64 {
	d := math.Atan2(math.Sin(angle-stack.cleftAngle), math.Cos(angle-stack.cleftAngle))
	return math.Exp(-math.Pow(d/.34, 2))
}
