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
	Name string  `json:"name"`
	X    float64 `json:"x"`
	Z    float64 `json:"z"`
	View string  `json:"view"`
}

var blackglassChartOnce sync.Once
var blackglassChartShore string

// A survey of the same Go height function that authors the coast, in world
// metres. The chart can follow a vessel beyond this surveyed coastline.
func blackglassBeachChart() string {
	blackglassChartOnce.Do(func() {
		var path strings.Builder
		const cols, rows = 48, 36
		for j := 0; j < rows; j++ {
			for i := 0; i < cols; i++ {
				x, z := -70+float64(i)*160/cols, -80+float64(j)*120/rows
				points := [4][2]float64{{x, z}, {x + 160.0/cols, z}, {x + 160.0/cols, z + 120.0/rows}, {x, z + 120.0/rows}}
				var heights [4]float64
				for k, p := range points {
					heights[k] = beachgen.TerrainHeight(p[0], p[1], beachgen.Seed)
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
		blackglassChartShore = path.String()
	})
	return blackglassChartShore
}

func blackglassBeachChartPlaces() string {
	places := []blackglassChartPlace{
		{"Tide pools", 10.8, 5.3, "pools"}, {"Black glass", beachgen.MonolithX, beachgen.MonolithZ, "glass"},
		{"Lighthouse", beaconX, beaconZ, "beacon"}, {"Sun hollow", grottoX, grottoZ, "hollow"},
		{"Jetty", beachgen.ShipX, beachgen.ShipZ, "ship"},
	}
	data, _ := json.Marshal(places)
	return string(data)
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
