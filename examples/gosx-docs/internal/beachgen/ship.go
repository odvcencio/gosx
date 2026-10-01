package beachgen

import "fmt"

const (
	ShipX       = 18.0
	ShipZ       = -42.0
	ShipHeading = 8.0 * 3.141592653589793 / 180
	JettyX      = 25.0
	JettyEndZ   = -35.2
	JettyDeck   = 2.5
)

func shipMaterials() []map[string]any {
	materials := []map[string]any{
		solidMaterial([4]float64{.013, .019, .023, 1}, .68, 0), // tar and dark blue paint
		solidMaterial([4]float64{.19, .095, .035, 1}, .82, 0),  // timber deck and spars
		solidMaterial([4]float64{.78, .72, .58, 1}, .88, 0),    // warm off-white canvas
		solidMaterial([4]float64{.16, .12, .075, 1}, 1, 0),     // rope
		solidMaterial([4]float64{.52, .43, .28, 1}, .6, 0),     // cream wale and brass wheel
		solidMaterial([4]float64{.45, .035, .025, 1}, .9, 0),   // red pennant
	}
	materials[2]["doubleSided"] = true
	materials[5]["doubleSided"] = true
	return materials
}

func clipperParts(lod int) []shipPart {
	stations, profile, sides, cols, rows := 20, 12, 7, 8, 6
	if lod == 1 {
		stations, profile, sides, cols, rows = 14, 8, 5, 4, 3
	}
	if lod == 2 {
		stations, profile, sides, cols, rows = 10, 6, 4, 2, 2
	}
	hull, deck, trim := shipHull(stations, profile)
	timber, rope := &geometry{}, &geometry{}
	parts := []shipPart{{"tarred-hull", hull, 0}, {"timber-deck", deck, 1}, {"painted-wale", trim, 4}}
	// Three masts, each with lower, top and topgallant square sails.
	for mast, spec := range [][3]float64{{-6.3, 18.5, 5.9}, {0, 21, 6.5}, {6, 17, 5.1}} {
		z, top, width := spec[0], spec[1], spec[2]
		mastMesh := &geometry{}
		shipTube(mastMesh, vec3{0, 2.5, z}, vec3{0, top, z}, .18, .065, sides)
		parts = append(parts, shipPart{fmt.Sprintf("mast-%d", mast), mastMesh, 1})
		for tier := 0; tier < 3; tier++ {
			yard := top - 1 - float64(tier)*4.1
			w := width * (.53 + float64(tier)*.23)
			height := 2.7 + float64(tier)*.3
			shipTube(timber, vec3{-w * .56, yard, z}, vec3{w * .56, yard, z}, .065, .065, sides)
			if lod < 2 || tier > 0 {
				parts = append(parts, shipPart{fmt.Sprintf("canvas-%d-%d", mast, tier), shipSail(z, yard, w, height, cols, rows), 2})
			}
			for _, side := range []float64{-1, 1} {
				shipTube(rope, vec3{w * .55 * side, yard, z}, vec3{0, top, z}, .016, .016, 3)
			}
		}
		for _, side := range []float64{-1, 1} {
			lines := 4
			if lod > 0 {
				lines = 2
			}
			if lod > 1 {
				lines = 1
			}
			for i := 0; i < lines; i++ {
				dz := float64(i) - float64(lines)/2
				shipTube(rope, vec3{side * 2, 2.7, z + dz}, vec3{0, top * .73, z}, .022, .016, 3)
			}
			if lod == 0 {
				for rung := 0; rung < 10; rung++ {
					t := float64(rung) / 12
					y := 2.7 + (top*.73-2.7)*t
					x := side * 2 * (1 - t)
					shipTube(rope, vec3{x, y, z - 2*(1-t)}, vec3{x, y, z + 1*(1-t)}, .012, .012, 3)
				}
			}
		}
		end := vec3{0, 3.2, -15}
		if mast > 0 {
			end = vec3{0, 10, spec[0] - 6}
		}
		shipTube(rope, vec3{0, top, z}, end, .025, .025, 3)
	}
	shipTube(timber, vec3{0, 3, -9}, vec3{0, 4.2, -16}, .2, .055, sides) // bowsprit
	shipTube(rope, vec3{0, 4.2, -16}, vec3{0, -.7, -10.8}, .025, .025, 3)
	// Poop deckhouse, hatches, plank seams, bulwark stanchions and a helm wheel.
	deckhouse := &geometry{}
	shipBox(deckhouse, 0, 2.9, 8.8, 1.5, .8, 1.4)
	parts = append(parts, shipPart{"deckhouse", deckhouse, 1})
	shipBox(timber, 0, 2.64, -3, 1.6, .28, 1.8)
	shipBox(timber, 0, 2.64, 3, 1.6, .28, 1.8)
	if lod < 2 {
		for z := -8.; z < 9; z += 1.2 {
			for _, side := range []float64{-1, 1} {
				shipTube(timber, vec3{side * hullWidth(z) * .84, 2.5, z}, vec3{side * hullWidth(z) * .84, 3.15 + hullSheer(z), z}, .04, .04, 4)
			}
		}
	}
	// Wheel is near the stern, above the clear central deck lane.
	shipTube(timber, vec3{0, 2.5, 6.5}, vec3{0, 3.5, 6.5}, .09, .09, sides)
	for spoke := 0; spoke < 8; spoke++ {
		a := float64(spoke) * 3.141592653589793 / 4
		shipWheelSpoke(trim, a)
	}
	flag := &geometry{}
	addQuad(flag, vec3{0, 20.8, 0}, vec3{1.6, 20.7, 0}, vec3{1.2, 20.05, 0}, vec3{0, 20.1, 0}, vec3{0, 0, 1})
	parts = append(parts, shipPart{"timber-spars", timber, 1}, shipPart{"rope-rigging", rope, 3}, shipPart{"wind-flag", flag, 5})
	return parts
}

// ShipAssets builds three clipper LODs plus the jetty with no image textures.
func ShipAssets() (map[string][]byte, error) {
	out := map[string][]byte{}
	for lod, name := range []string{"clipper-high.glb", "clipper-mid.glb", "clipper-low.glb"} {
		data, err := writeShipGLB(clipperParts(lod), shipMaterials())
		if err != nil {
			return nil, err
		}
		out[name] = data
	}
	data, err := writeGLB(jettyGeometry(), solidMaterial([4]float64{.14, .078, .032, 1}, .9, 0), nil)
	if err != nil {
		return nil, err
	}
	out["jetty.glb"] = data
	foam, err := wakeFoam()
	if err != nil {
		return nil, err
	}
	out["wake-foam.png"] = foam
	return out, nil
}
