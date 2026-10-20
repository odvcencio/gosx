package beachgen

import "math"

func shipWheelSpoke(g *geometry, a float64) {
	centre := vec3{0, 3.45, 6.5}
	r := .43
	edge := vec3{r * math.Sin(a), centre.y + r*math.Cos(a), centre.z}
	next := vec3{r * math.Sin(a+math.Pi/4), centre.y + r*math.Cos(a+math.Pi/4), centre.z}
	shipTube(g, centre, edge, .022, .022, 4)
	shipTube(g, edge, next, .035, .035, 4)
}

func jettyGeometry() *geometry {
	g := &geometry{}
	for z := -35.; z < 8; z += .72 {
		y := JettyDeck
		if z > -12 {
			y = JettyDeck * (8 - z) / 20
		}
		shipBox(g, JettyX, y-.09, z, 2.5, .18, .68)
		if z < -12 {
			for _, side := range []float64{-1, 1} {
				shipBox(g, JettyX+side*1.12, y-.24, z, .13, .24, .7)
			}
		}
	}
	// The gangway meets the stern; a cantilever avoids a pile at the helm.
	for x := 19.; x < JettyX+.9; x += .72 {
		shipBox(g, x, JettyDeck-.09, JettyEndZ, .68, .18, 2)
	}
	for z := -30.; z < 4; z += 6 {
		y := JettyDeck
		if z > -12 {
			y = JettyDeck * (8 - z) / 20
		}
		for _, side := range []float64{-1, 1} {
			x := JettyX + side*1.02
			shipTube(g, vec3{x, -6, z}, vec3{x, y - .15, z}, .18, .15, 6)
			shipTube(g, vec3{x, y - .15, z}, vec3{x, y + .85, z}, .06, .06, 4)
		}
	}
	for _, side := range []float64{-1, 1} {
		shipTube(g, vec3{JettyX + side*1.02, JettyDeck + .65, -30}, vec3{JettyX + side*1.02, JettyDeck + .65, -12}, .025, .025, 4)
	}
	return g
}

// JettySurfaces are separate walk planes: a shore ramp, the pier and gangway.
func JettySurfaces() [][7]float64 {
	return [][7]float64{
		{JettyX, JettyDeck / 2, -2, 2.5, 20, 0, -JettyDeck / 20},
		{JettyX, JettyDeck, -23.6, 2.5, 23.2, 0, 0},
		{22, JettyDeck, JettyEndZ, 7, 2, 0, 0},
	}
}

func JettyColliders() []WalkShape {
	var out []WalkShape
	for z := -30.; z < 4; z += 6 {
		y := JettyDeck
		if z > -12 {
			y = JettyDeck * (8 - z) / 20
		}
		for _, side := range []float64{-1, 1} {
			out = append(out, WalkShape{Kind: "cylinder", X: JettyX + side*1.02, Y: -6, Z: z, Radius: .19, Height: y + 6 - .15})
		}
	}
	return out
}
