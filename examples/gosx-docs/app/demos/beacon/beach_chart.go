package docs

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

type blackglassChartPlace struct {
	Name   string       `json:"name"`
	X      float64      `json:"x"`
	Z      float64      `json:"z"`
	View   string       `json:"view"`
	Island bool         `json:"island,omitempty"`
	Route  [][2]float64 `json:"route,omitempty"`
}

var blackglassChartOnce sync.Once
var blackglassChartShore string

// A survey of the same Go height function that authors the coast, in world
// metres. The chart can follow a vessel beyond this surveyed coastline.
func blackglassBeachChart() string {
	blackglassChartOnce.Do(func() {
		var path strings.Builder
		survey := func(minX, minZ, sizeX, sizeZ float64, sample func(float64, float64) float64) {
			const cols, rows = 48, 36
			for j := 0; j < rows; j++ {
				for i := 0; i < cols; i++ {
					x, z := minX+float64(i)*sizeX/cols, minZ+float64(j)*sizeZ/rows
					points := [4][2]float64{{x, z}, {x + sizeX/cols, z}, {x + sizeX/cols, z + sizeZ/rows}, {x, z + sizeZ/rows}}
					var heights [4]float64
					for k, p := range points {
						heights[k] = sample(p[0], p[1])
					}
					var crossings [][2]float64
					for k, a := range points {
						b, ha, hb := points[(k+1)%4], heights[k], heights[(k+1)%4]
						if (ha > 0) == (hb > 0) {
							continue
						}
						t := ha / (ha - hb)
						crossings = append(crossings, [2]float64{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t})
					}
					for k := 0; k+1 < len(crossings); k += 2 {
						fmt.Fprintf(&path, "M%.1f %.1fL%.1f %.1f", crossings[k][0], crossings[k][1], crossings[k+1][0], crossings[k+1][1])
					}
				}
			}
		}
		survey(-70, -80, 160, 120, func(x, z float64) float64 { return beachgen.TerrainHeight(x, z, beachgen.Seed) })
		for _, island := range beachgen.OffshoreIslands() {
			survey(island.X-48, island.Z-48, 96, 96, func(x, z float64) float64 { return beachgen.IslandHeight(island, x, z) })
		}
		blackglassChartShore = path.String()
	})
	return blackglassChartShore
}

func blackglassBeachPlaces() []blackglassChartPlace {
	places := []blackglassChartPlace{
		{"Tide pools", 10.8, 5.3, "pools", false, nil}, {"Black glass", beachgen.MonolithX, beachgen.MonolithZ, "glass", false, nil},
		{"Lighthouse", beaconX, beaconZ, "beacon", false, nil}, {"Sun hollow", grottoX, grottoZ, "hollow", false, nil},
		{"Jetty", beachgen.ShipX, beachgen.ShipZ, "ship", false, nil},
	}
	for _, island := range beachgen.OffshoreIslands() {
		p := beachgen.IslandPier(island)
		var passage [][2]float64
		if island.ID == "starglass" {
			passage = [][2]float64{{230, -100}, {410, -240}}
		}
		places = append(places, blackglassChartPlace{island.Name, p[0], p[2] + p[4]/2 - 1, island.ID, true, passage})
	}
	return places
}

func blackglassBeachChartPlaces() string {
	data, _ := json.Marshal(blackglassBeachPlaces())
	return string(data)
}

// Keep the view-model keys used by CoastChart independent of the public
// navigation IDs (drift-isle and bell-rock are not Go field names).
func blackglassBeachChartIslandCoordinates() map[string]float64 {
	coordinates := make(map[string]float64, 6)
	for _, place := range blackglassBeachPlaces() {
		key := ""
		switch place.View {
		case "drift-isle":
			key = "drift"
		case "bell-rock":
			key = "bell"
		case "starglass":
			key = "starglass"
		}
		if key != "" && place.Island {
			coordinates[key+"X"] = place.X
			coordinates[key+"Z"] = place.Z
		}
	}
	return coordinates
}

func blackglassBeachOptics() *scene.Optics {
	optics := &scene.Optics{Magnification: 3, InputSignal: "$blackglass.spyglass", OutputSignal: "$blackglass.sight"}
	for _, place := range blackglassBeachPlaces() {
		optics.Landmarks = append(optics.Landmarks, scene.Landmark{ID: place.View, Name: place.Name, Position: scene.Vec3(place.X, 0, place.Z), Landing: place.Island})
	}
	return optics
}

func blackglassBeachPoolRipple() scene.Mesh {
	return scene.Mesh{ID: "pool-ripple", Geometry: scene.TorusGeometry{Radius: 1, Tube: .009, RadialSegments: 4, TubularSegments: 32},
		Rotation: scene.Euler{X: 1.5707963267948966}, Scale: scene.Vec3(.001, .001, .001),
		Material: scene.StandardMaterial{Color: "#9ac8bf", Roughness: .2, Opacity: scene.Float(.3)}, DepthWrite: scene.Bool(false)}
}

func blackglassBeachPoolRippleSites() string {
	var sites [][4]float64
	for _, site := range blackglassBeachPoolSites() {
		sites = append(sites, [4]float64{site[0], blackglassBeachPoolLevel(site[0], site[1], site[2]) + .018, site[1], site[2]})
	}
	data, _ := json.Marshal(sites)
	return string(data)
}

func blackglassHarborExit() string { return `[80,-80]` }

// BlackglassBeachChartSVG keeps the immutable survey on the server rather than
// duplicating its path in island props and the initial document.
func BlackglassBeachChartSVG() string {
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="-245 -430 600 480"><path d="` + blackglassBeachChart() + `" fill="none" stroke="#91aaa0" stroke-width="0.55" vector-effect="non-scaling-stroke"/></svg>`
}
