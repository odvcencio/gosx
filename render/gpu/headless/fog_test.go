package headless

import (
	"image/color"
	"testing"

	"m31labs.dev/gosx/engine"
)

// fogProbeScene builds a lit plane at the given world distance from the
// camera, facing the camera straight on. The plane's Width and Height scale
// with distance, so its projected footprint (and therefore the sampled centre
// pixel) stays the same across distances: only the fog term, which reads
// world-space distance, should move by distance.
//
// The framing follows litSurfaceScene: camera above, looking straight down
// (RotationX ~ pi/2), light pointing straight down too, so NdotL = 1 and the
// direct term carries no distance falloff (a directional light has none). Any
// difference the fog density makes is therefore fog, not attenuation.
func fogProbeScene(material engine.RenderMaterial, distance, fogDensity float64, fogColor string) engine.RenderBundle {
	env := darkEnvironment()
	env.FogColor = fogColor
	env.FogDensity = fogDensity
	return engine.RenderBundle{
		Background: "#000000",
		Camera:     engine.RenderCamera{Y: distance, RotationX: 1.5707963, FOV: 1, Near: 0.1, Far: distance * 4},
		Materials:  []engine.RenderMaterial{material},
		Lights: []engine.RenderLight{{
			Kind: "directional", Color: "#ffffff", Intensity: 1,
			DirectionY: -1,
		}},
		Environment: env,
		InstancedMeshes: []engine.RenderInstancedMesh{{
			ID: "floor", Kind: "plane", Width: distance, Height: distance,
			MaterialIndex: 0, InstanceCount: 1,
			Transforms: []float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1},
		}},
	}
}

// pixelDelta reports the largest single-channel move between two pixels.
func pixelDelta(a, b color.RGBA) int {
	abs := func(v int) int {
		if v < 0 {
			return -v
		}
		return v
	}
	max := abs(int(a.R) - int(b.R))
	if v := abs(int(a.G) - int(b.G)); v > max {
		max = v
	}
	if v := abs(int(a.B) - int(b.B)); v > max {
		max = v
	}
	return max
}

// TestNativeFogMovesTowardFogColorByDistance pins the native lit shader's
// exponential-squared fog end to end: RenderEnvironment.FogColor/FogDensity
// reach render/bundle/lit.go litWGSL through the Scene uniform's fogParams
// lane, and the term moves the frame.
//
// Two ordered deltas, not one, are the point. A frame that merely changes
// under fog could be catching some other regression (a stray offset shift in
// the uniform layout, for one). Requiring the far probe to move MORE than the
// near probe is the signature of exp(-density^2 * dist^2): nothing else in the
// shading model grows with camera distance for a directional light with no
// range.
func TestNativeFogMovesTowardFogColorByDistance(t *testing.T) {
	material := engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.5}
	const fogColor = "#ff2020"
	const (
		nearDistance = 6.0
		farDistance  = 40.0
		density      = 0.4
	)

	noFogNear := renderCenterPixel(t, fogProbeScene(material, nearDistance, 0, fogColor))
	if noFogNear.A != 255 || noFogNear.R+noFogNear.G+noFogNear.B == 0 {
		t.Fatalf("the near reference frame did not draw a lit surface: %+v", noFogNear)
	}
	fogNear := renderCenterPixel(t, fogProbeScene(material, nearDistance, density, fogColor))
	noFogFar := renderCenterPixel(t, fogProbeScene(material, farDistance, 0, fogColor))
	if noFogFar.A != 255 || noFogFar.R+noFogFar.G+noFogFar.B == 0 {
		t.Fatalf("the far reference frame did not draw a lit surface: %+v", noFogFar)
	}
	fogFar := renderCenterPixel(t, fogProbeScene(material, farDistance, density, fogColor))

	deltaNear := pixelDelta(noFogNear, fogNear)
	deltaFar := pixelDelta(noFogFar, fogFar)

	const minNearDelta = 10
	if deltaNear < minNearDelta {
		t.Fatalf("fog at density %v moved the near frame by at most %d, want at least %d; "+
			"the fog term is missing, or fogParams never reached the shader",
			density, deltaNear, minNearDelta)
	}
	if deltaFar <= deltaNear {
		t.Fatalf("fog moved the far frame (distance %v) by %d, want more than the near frame's "+
			"(distance %v) %d; exp(-density^2 * dist^2) must grow with distance",
			farDistance, deltaFar, nearDistance, deltaNear)
	}
}

// TestNativeFogGateStaysClosedAtZeroDensity pins the gate half: a scene with
// FogDensity 0 must render byte-identical whether or not FogColor is also
// authored. FogDensity 0 is both Go's zero value and "no fog" per
// resolveFogParams, so a mutation that flips the gate's sign, or that reads
// the colour before checking the density, would fog every scene that names a
// FogColor but never raises FogDensity above zero.
func TestNativeFogGateStaysClosedAtZeroDensity(t *testing.T) {
	material := engine.RenderMaterial{Kind: "standard", Color: "#4080c0", Roughness: 0.5}
	noColor := renderCenterPixel(t, fogProbeScene(material, 6, 0, ""))
	withColor := renderCenterPixel(t, fogProbeScene(material, 6, 0, "#ff2020"))
	if noColor != withColor {
		t.Fatalf("FogDensity 0 rendered differently depending on whether FogColor was set: %+v vs %+v",
			noColor, withColor)
	}
}
