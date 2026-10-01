package docs

import (
	"embed"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"strings"
	"sync"

	"m31labs.dev/gosx/scene"
)

const blackglassModelRoot = "/models/blackglass/"

type blackglassPeriod struct {
	ID           string
	Name         string
	Top          string
	Horizon      string
	Bottom       string
	Sun          string
	SunStrength  float64
	Ambient      string
	AmbientPower float64
	BeaconPower  float64
	ShallowWater string
	DeepWater    string
}

func blackglassPeriodFor(raw string) blackglassPeriod {
	switch raw {
	case "daybreak":
		return blackglassPeriod{"daybreak", "Daybreak", "#557d9b", "#e7ad82", "#eed6af", "#ffe3b5", 1.05, "#b8cdd3", 0.30, 3.1, "#377f95", "#0d3545"}
	case "ember-hour":
		return blackglassPeriod{"ember-hour", "Ember hour", "#36415e", "#dc9875", "#aa7367", "#ffc18a", 0.90, "#a9abb9", 0.25, 4.2, "#326e80", "#102b3d"}
	default:
		return blackglassPeriod{"high-sun", "High sun", "#438bab", "#c6e0df", "#afd2cf", "#fff0d0", 1.20, "#c2e2ea", 0.36, 2.6, "#289ab2", "#06283e"}
	}
}

func blackglassViewID(raw string) string {
	switch raw {
	case "arrival", "beacon":
		return raw
	default:
		return "overlook"
	}
}

func blackglassViewPose(contract BlackglassCoastContract, raw string) (scene.Vector3, scene.Vector3) {
	opening, _ := contract.Marker("opening-camera")
	arrival, _ := contract.Marker("arrival")
	beacon, _ := contract.Marker("cinematic-beacon")
	switch blackglassViewID(raw) {
	case "arrival":
		// Leave room for the stele and harbor plaque in a portrait viewport.
		return contract.Local(scene.Vec3(arrival.Position.X-2.7, 3.4, arrival.Position.Z+10)), contract.Local(scene.Vec3(-1.3, 2.1, 10))
	case "beacon":
		return contract.Local(scene.Vec3(beacon.Position.X+7.8, 6.3, beacon.Position.Z+11.5)), contract.Local(scene.Vec3(8.3, 4.0, -3.6))
	default:
		// The Studio opening marker is an overlook anchor. Step back along the
		// shelf so the foreground rock frames the cove instead of filling it.
		return contract.Local(scene.Vec3(opening.Position.X-4, opening.Position.Y+1.8, opening.Position.Z+1)), contract.Local(scene.Vec3(1, 1.7, -4))
	}
}

var (
	blackglassLensOnce sync.Once
	blackglassLens     scene.CustomMaterial
	blackglassLensErr  error
	blackglassIBLOnce  sync.Once
	blackglassIBL      map[string]scene.EnvironmentIBL
)

//go:embed ibl/*.json
var blackglassIBLFiles embed.FS

func blackglassPeriodIBL(period string) scene.EnvironmentIBL {
	blackglassIBLOnce.Do(func() {
		blackglassIBL = make(map[string]scene.EnvironmentIBL, 3)
		for _, id := range []string{"daybreak", "high-sun", "ember-hour"} {
			data, err := blackglassIBLFiles.ReadFile("ibl/" + id + ".json")
			if err != nil {
				panic("read Blackglass Coast IBL: " + err.Error())
			}
			var value scene.EnvironmentIBL
			if err := json.Unmarshal(data, &value); err != nil {
				panic("decode Blackglass Coast IBL: " + err.Error())
			}
			blackglassIBL[id] = value
		}
	})
	return blackglassIBL[blackglassPeriodFor(period).ID]
}

func blackglassBeaconLensMaterial(contract BlackglassCoastContract) scene.CustomMaterial {
	blackglassLensOnce.Do(func() {
		blackglassLens, _, blackglassLensErr = scene.CompileSelenaMaterial([]byte(contract.BeaconSelenaSource), scene.SelenaMaterialOptions{
			Material: "BeaconEmber",
			Standard: scene.StandardMaterial{Color: "#ff9c4e", Roughness: 0.2, Metalness: 0.12, Emissive: 1.25},
		})
	})
	if blackglassLensErr != nil {
		panic("invalid Studio beacon Selena material: " + blackglassLensErr.Error())
	}
	return blackglassLens
}

func blackglassArtifacts(contract BlackglassCoastContract, period blackglassPeriod) []scene.Node {
	local := contract.Local
	zone := contract.Water
	arrival, _ := contract.Marker("arrival")
	engraving := html.EscapeString(contract.ArtDirection)
	fingerprint := html.EscapeString(contract.DocumentFingerprint)
	waterName := html.EscapeString(zone.Name)
	stele := fmt.Sprintf(`<article class="blackglass-artifact blackglass-artifact--stele"><p class="blackglass-artifact__kicker">Arrival stone · %s</p><h2>Blackglass Coast</h2><p>%s</p><p class="blackglass-artifact__mark" aria-hidden="true">◇ · ◇ · ◇</p><p class="blackglass-artifact__small">Arrival %.1f · %.1f · %.1f</p><p class="blackglass-artifact__small">Studio document %s · revision %d</p><code class="blackglass-artifact__fingerprint">SHA-256 %s</code></article>`, html.EscapeString(period.Name), engraving, arrival.Position.X, arrival.Position.Y, arrival.Position.Z, html.EscapeString(contract.DocumentID), contract.Revision, fingerprint)
	ledger := fmt.Sprintf(`<article class="blackglass-artifact blackglass-artifact--ledger"><p class="blackglass-artifact__kicker">Keeper's fire and tide ledger · %s</p><h2>%s</h2><dl><div><dt>Center</dt><dd>%.0f, %.0f, %.0f</dd></div><div><dt>Span</dt><dd>%.0f × %.0f × %.0f</dd></div><div><dt>Surface</dt><dd>%.1f</dd></div><div><dt>Current</dt><dd>%.2f east · %.2f north</dd></div><div><dt>Renderer</dt><dd><output data-gosx-scene3d-status="renderer">starting…</output></dd></div><div><dt>Quality</dt><dd><output data-gosx-scene3d-status="quality">measuring…</output></dd></div></dl></article>`, html.EscapeString(period.Name), waterName, zone.Center.X, zone.Center.Y, zone.Center.Z, zone.Size.X, zone.Size.Y, zone.Size.Z, zone.SurfaceY, zone.Current.X, zone.Current.Z)
	plaque := `<article class="blackglass-artifact blackglass-artifact--plaque"><p class="blackglass-artifact__kicker">Harbor notice</p><h2>Keep the light</h2><p>60 frames each second · 720p scene · 540p effects · 512px shadow</p><p>128² tide grid · 320 embers at most</p></article>`
	return []scene.Node{
		scene.HTML{ID: "arrival-stele", Mode: scene.HTMLTexture, Position: local(scene.Vec3(-3.8, 1.85, 10.6)), Rotation: scene.Euler{X: -math.Pi / 2, Y: -0.3}, SurfaceWidth: 2.15, SurfaceHeight: 2.8, TextureWidth: 384, TextureHeight: 500, MaxTexturePixels: scene.HTMLTextureMaxPixels512, ClassName: "blackglass-world-surface", Markup: stele, Fallback: "Arrival stone. Blackglass Coast. Studio document " + contract.DocumentFingerprint},
		scene.HTML{ID: "keeper-ledger", Mode: scene.HTMLTexture, Position: local(scene.Vec3(9.2, 4.2, -0.7)), Rotation: scene.Euler{X: -math.Pi / 2, Y: 0.64}, SurfaceWidth: 4.1, SurfaceHeight: 3.05, TextureWidth: 512, TextureHeight: 384, MaxTexturePixels: scene.HTMLTextureMaxPixels512, ClassName: "blackglass-world-surface", Markup: ledger, Fallback: fmt.Sprintf("Keeper's fire and tide ledger. %s. Center %.0f, %.0f, %.0f. Span %.0f by %.0f by %.0f. Surface %.1f. Current %.2f east, %.2f north.", zone.Name, zone.Center.X, zone.Center.Y, zone.Center.Z, zone.Size.X, zone.Size.Y, zone.Size.Z, zone.SurfaceY, zone.Current.X, zone.Current.Z)},
		scene.HTML{ID: "harbor-plaque", Mode: scene.HTMLTexture, Position: local(scene.Vec3(2.1, 1.45, 7.3)), Rotation: scene.Euler{X: -math.Pi / 2, Y: -0.16}, SurfaceWidth: 2.4, SurfaceHeight: 1.15, TextureWidth: 512, TextureHeight: 256, MaxTexturePixels: scene.HTMLTextureMaxPixels512, ClassName: "blackglass-world-surface", Markup: plaque, Fallback: "Harbor notice. 60 frames each second. 720p scene. 540p effects. 512 pixel shadow. 128 squared tide grid. 320 embers at most."},
	}
}

func blackglassModel(name string, contract BlackglassCoastContract, bounds float64) scene.Model {
	return scene.Model{
		ID: name, Src: blackglassModelRoot + name + "-v1.glb", Position: contract.Local(scene.Vec3(0, 0, 0)),
		Bounds: bounds, CastShadow: name != "shore", ReceiveShadow: true,
	}
}

func blackglassPageLinks(view, period string) map[string]string {
	view = blackglassViewID(view)
	period = blackglassPeriodFor(period).ID
	return map[string]string{
		"overlookHref": "?view=overlook&period=" + period,
		"arrivalHref":  "?view=arrival&period=" + period,
		"beaconHref":   "?view=beacon&period=" + period,
		"daybreakHref": "?view=" + view + "&period=daybreak",
		"highSunHref":  "?view=" + view + "&period=high-sun",
		"emberHref":    "?view=" + view + "&period=ember-hour",
	}
}

func blackglassViewName(id string) string {
	switch blackglassViewID(id) {
	case "arrival":
		return "Arrival beach"
	case "beacon":
		return "Beacon terrace"
	default:
		return "Overlook"
	}
}

func blackglassPeriodTag(period blackglassPeriod) string {
	return strings.ToUpper(period.Name)
}
