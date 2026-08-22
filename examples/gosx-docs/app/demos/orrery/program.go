// Package docs provides the Lodestar Meridian showcase at /demos/orrery.
//
// The Lodestar Meridian is a clockwork star-system engine: a levitating
// lodestone heart, three armillary armatures, three planets riding keyframed
// circular orbits, and a dark transit moon whose mid-transit alignment fires
// the heart flare. Every moving part is declared with shipped Scene3D
// animation primitives (graph AnimationClip channels and Mesh.MaterialAnims),
// so the choreography is data on the wire rather than bespoke JavaScript.
package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

const (
	orreryNodeBudget = 17
	// orreryExpandedVertexBudget bounds the renderer-facing expanded geometry,
	// measured the same way as the Blackglass Coast budget (instanced meshes
	// count their geometry once).
	orreryExpandedVertexBudget = 25000
	orreryStarCount            = 220
	orreryMaxPixels            = scene.PostFXMaxPixels720p

	// The demonstration cycle. Planet orbital periods (6 s, 8 s, 12 s) divide
	// the cycle, and every keyframe track starts and ends on the same pose, so
	// the choreography loops without a pop whether the runtime loops the clip
	// or holds its final frame.
	orreryCycleSeconds = 24.0

	// Transit window. The dark moon rises into view at 12 s, aligns with the
	// lodestone heart at 13.2 s, and exits behind the armature at 14.4 s.
	// The heart-flare and halo-pulse material keys are pinned to the same
	// 13.2 s instant; the focused tests assert that alignment.
	orreryTransitRise = 12.0
	orreryTransitMid  = 13.2
	orreryTransitExit = 14.4

	// Orrery geometry anchors. The heart sits at the top of the composition;
	// planets orbit in the ecliptic plane around it.
	orreryHeartY          = 2.05
	orreryCinderRadius    = 2.10
	orreryPorcelainRadius = 2.72
	orreryVerdigrisRadius = 3.30
)

// LodestarMeridianProgram builds the bounded, deterministic, asset-free
// Scene3D program behind /demos/orrery.
func LodestarMeridianProgram() scene.Props {
	return scene.Props{
		Width: 1280, Height: 720,
		Label:      "Lodestar Meridian — clockwork star-system engine",
		AriaLabel:  "Lodestar Meridian, a clockwork orrery with a glowing heart, three spinning armatures, three orbiting planets, and a transit moon that triggers a flare",
		Background: "#0b0b0d", Controls: "orbit", AutoRotate: scene.Bool(false), Responsive: scene.Bool(true), FillHeight: scene.Bool(true),
		PreferWebGPU: scene.Bool(true), CanvasAlpha: scene.Bool(false),
		UnsupportedMessage: "Interactive 3D is unavailable in this browser. The Lodestar Meridian explanation and source links remain available beside the canvas.",
		Stats:              scene.Bool(true),
		ControlTarget:      scene.Vec3(0, 2.0, 0), ControlMinDistance: 6.5, ControlMaxDistance: 20,
		MaxFPS: 60, MaxDevicePixelRatio: 1.5, MaxPixels: orreryMaxPixels,
		AdaptiveQuality: scene.Bool(true), AdaptiveTargetFrameMS: 16.7, AdaptiveWarmupFrames: 18, AdaptivePostFX: scene.Bool(true),
		Camera: scene.PerspectiveCamera{Position: scene.Vec3(0, 3.6, 11.4), FOV: 42, Near: 0.1, Far: 90},
		Environment: scene.Environment{
			AmbientColor: "#1c1830", AmbientIntensity: 0.5,
			FogColor: "#0b0b0d", FogDensity: 0.022,
		},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels540p, Effects: []scene.PostEffect{
			scene.Bloom{Threshold: 0.72, Strength: 0.55, Radius: 8, Scale: 0.35},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.12},
			scene.Vignette{Intensity: 0.4},
			scene.FXAA{},
		}},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels512},
		Graph:   scene.NewGraph(orreryNodes()...),
	}
}

// orreryNodes declares the whole world in a fixed order. The order is part of
// the demo contract: graph animation channels address targets by node index,
// so reordering this list would silently retarget the choreography. The
// builder resolves indices by stable node ID instead of hardcoding numbers.
func orreryNodes() []scene.Node {
	iron := scene.StandardMaterial{Color: "#15151a", Roughness: 0.38, Metalness: 0.55, Clearcoat: 0.5}
	brass := scene.StandardMaterial{Color: "#b08d4f", Roughness: 0.34, Metalness: 0.92, Clearcoat: 0.35}
	glassArmature := scene.StandardMaterial{Color: "#232032", Roughness: 0.22, Metalness: 0.82, Clearcoat: 0.66}
	copper := scene.StandardMaterial{Color: "#c98a5a", Roughness: 0.36, Metalness: 0.85, Clearcoat: 0.4}
	porcelain := scene.StandardMaterial{Color: "#e8e6df", Roughness: 0.5, Metalness: 0.05, Clearcoat: 0.3}
	verdigris := scene.StandardMaterial{Color: "#5da58f", Roughness: 0.55, Metalness: 0.35, Clearcoat: 0.3}
	darkmoon := scene.StandardMaterial{Color: "#101014", Roughness: 0.8, Metalness: 0.2}

	nodes := []scene.Node{
		scene.DirectionalLight{
			ID: "orrery-key", Color: "#cdd7ff", Intensity: 1.15,
			Direction: scene.Vec3(-0.5, -1, -0.42), CastShadow: true, ShadowBias: -0.001, ShadowSize: 512,
		},
		scene.PointLight{
			ID: "orrery-heart-light", Color: "#c4b5fd", Intensity: 2.4,
			Position: scene.Vec3(0, orreryHeartY, 0), Range: 13, Decay: 2,
		},
		scene.HemisphereLight{
			ID: "orrery-horizon", SkyColor: "#2b2547", GroundColor: "#0b0b0d", Intensity: 0.4,
		},

		// Observatory dais: two stepped iron tiers carrying the engraved
		// ecliptic band. These anchor the foreground silhouette.
		scene.Mesh{
			ID: "orrery-dais-base", Geometry: scene.CylinderGeometry{RadiusTop: 3.7, RadiusBottom: 4.0, Height: 0.5, Segments: 28},
			Material: iron, Position: scene.Vec3(0, 0.25, 0), CastShadow: true, ReceiveShadow: true,
		},
		scene.Mesh{
			ID: "orrery-dais-step", Geometry: scene.CylinderGeometry{RadiusTop: 3.0, RadiusBottom: 3.15, Height: 0.45, Segments: 24},
			Material: iron, Position: scene.Vec3(0, 0.72, 0), CastShadow: true, ReceiveShadow: true,
		},
		scene.Mesh{
			ID:       "orrery-ecliptic-ring",
			Geometry: scene.TorusGeometry{Radius: 3.52, Tube: 0.055, RadialSegments: 12, TubularSegments: 96},
			Material: brass,
			Position: scene.Vec3(0, orreryHeartY, 0), Rotation: scene.Rotate(math.Pi/2, 0, 0),
			CastShadow: true, ReceiveShadow: true,
		},

		// The lodestone heart and its halo. Their emissive uniforms are the
		// ignition ramp and the mid-transit flare; see orreryHeartEmissiveAnim
		// and orreryHaloEmissiveAnim below.
		scene.Mesh{
			ID:            "orrery-heart",
			Geometry:      scene.SphereGeometry{Radius: 0.62, Segments: 22},
			Material:      scene.StandardMaterial{Color: "#2a2340", Roughness: 0.3, Metalness: 0.4, Clearcoat: 0.5, Emissive: 0.12},
			Position:      scene.Vec3(0, orreryHeartY, 0),
			CastShadow:    true,
			MaterialAnims: []scene.MaterialUniformAnim{orreryHeartEmissiveAnim()},
		},
		scene.Mesh{
			ID:       "orrery-heart-halo",
			Geometry: scene.TorusGeometry{Radius: 0.95, Tube: 0.035, RadialSegments: 8, TubularSegments: 56},
			Material: scene.StandardMaterial{Color: "#17131f", Roughness: 0.3, Metalness: 0.5, Emissive: 0.06},
			Position: scene.Vec3(0, orreryHeartY, 0), Rotation: scene.Rotate(math.Pi/2.6, 0.2, 0),
			CastShadow:    true,
			MaterialAnims: []scene.MaterialUniformAnim{orreryHaloEmissiveAnim()},
		},

		// Armillary armatures: three glass-dark rings tilted around the heart,
		// each turning slowly about its own symmetry axis. Constant procedural
		// spin reads as clockwork; the phase-specific storytelling stays in the
		// keyframe tracks.
		scene.Mesh{
			ID:       "orrery-armillary-alpha",
			Geometry: scene.TorusGeometry{Radius: 1.85, Tube: 0.05, RadialSegments: 10, TubularSegments: 64},
			Material: glassArmature,
			Position: scene.Vec3(0, orreryHeartY, 0), Rotation: scene.Rotate(math.Pi/3.1, 0.35, 0),
			Spin:       scene.Rotate(0, 0.24, 0),
			CastShadow: true,
		},
		scene.Mesh{
			ID:       "orrery-armillary-beta",
			Geometry: scene.TorusGeometry{Radius: 2.48, Tube: 0.045, RadialSegments: 10, TubularSegments: 60},
			Material: glassArmature,
			Position: scene.Vec3(0, orreryHeartY, 0), Rotation: scene.Rotate(-math.Pi/2.6, -0.5, math.Pi/5),
			Spin:       scene.Rotate(-0.02, -0.19, 0.03),
			CastShadow: true,
		},
		scene.Mesh{
			ID:       "orrery-armillary-gamma",
			Geometry: scene.TorusGeometry{Radius: 3.06, Tube: 0.04, RadialSegments: 8, TubularSegments: 56},
			Material: brass,
			Position: scene.Vec3(0, orreryHeartY, 0), Rotation: scene.Rotate(math.Pi/4.2, 1.05, -math.Pi/7),
			Spin:       scene.Rotate(0.02, 0.14, -0.02),
			CastShadow: true,
		},

		// The procession: three planets riding the ecliptic circles. Their
		// translation channels are generated from closed-form circular keys.
		scene.Mesh{
			ID: "orrery-planet-cinder", Geometry: scene.SphereGeometry{Radius: 0.17, Segments: 14},
			Material: copper, Position: scene.Vec3(orreryCinderRadius, orreryHeartY, 0), CastShadow: true,
		},
		scene.Mesh{
			ID: "orrery-planet-porcelain", Geometry: scene.SphereGeometry{Radius: 0.23, Segments: 14},
			Material: porcelain, Position: scene.Vec3(-orreryPorcelainRadius, orreryHeartY, 0), CastShadow: true,
		},
		scene.Mesh{
			ID: "orrery-planet-verdigris", Geometry: scene.SphereGeometry{Radius: 0.2, Segments: 14},
			Material: verdigris, Position: scene.Vec3(0, orreryHeartY, orreryVerdigrisRadius), CastShadow: true,
		},

		// The transit moon parks below the opaque dais outside its window, so
		// it enters and leaves the frame on declared keyframes only.
		scene.Mesh{
			ID: "orrery-transit-moon", Geometry: scene.SphereGeometry{Radius: 0.31, Segments: 14},
			Material: darkmoon, Position: orreryMoonPark(), CastShadow: true,
		},

		// Depth layer: a seeded, deterministic star dome. Fixed LCG seed, no
		// textures, no assets.
		orreryStars(),
	}

	nodes = append(nodes, orreryProcessionClip(indexOfOrreryNode(nodes)))
	if len(nodes) > orreryNodeBudget {
		panic("lodestar meridian exceeds its scene node budget")
	}
	return nodes
}

// orreryProcessionClip declares the four transform channels of the
// demonstration cycle: three planetary processions and the transit crossing.
func orreryProcessionClip(indexOf func(string) int) scene.AnimationClip {
	return scene.AnimationClip{
		Name:     "meridian-procession",
		Duration: orreryCycleSeconds,
		Channels: []scene.AnimationChannel{
			orreryPlanetChannel("orrery-planet-cinder", indexOf, orreryOrbitSpec{
				Radius: orreryCinderRadius, PeriodSeconds: 6, SamplesPerRevolution: 16, Phase: 0,
			}),
			orreryPlanetChannel("orrery-planet-porcelain", indexOf, orreryOrbitSpec{
				Radius: orreryPorcelainRadius, PeriodSeconds: 8, SamplesPerRevolution: 16, Phase: math.Pi / 3,
			}),
			orreryPlanetChannel("orrery-planet-verdigris", indexOf, orreryOrbitSpec{
				Radius: orreryVerdigrisRadius, PeriodSeconds: 12, SamplesPerRevolution: 20, Phase: 2 * math.Pi / 3,
			}),
			orreryTransitChannel(indexOf("orrery-transit-moon")),
		},
	}
}

type orreryOrbitSpec struct {
	Radius               float64
	PeriodSeconds        float64 // must divide orreryCycleSeconds exactly
	SamplesPerRevolution int
	Phase                float64 // radians at t=0
}

// orreryPlanetChannel samples one closed circular orbit across the full
// demonstration cycle. The first and last keys carry the same pose, so the
// track loops seamlessly; periods divide the cycle, so revolutions close.
func orreryPlanetChannel(nodeID string, indexOf func(string) int, spec orreryOrbitSpec) scene.AnimationChannel {
	revolutions := orreryCycleSeconds / spec.PeriodSeconds
	keys := int(revolutions) * spec.SamplesPerRevolution
	times := make([]float64, keys+1)
	values := make([]float64, 3*(keys+1))
	for i := 0; i <= keys; i++ {
		if i == keys {
			// Close the loop bit-exactly: the final key reuses the opening
			// pose instead of recomputing it through trigonometry.
			times[i] = orreryCycleSeconds
			copy(values[3*i:], values[0:3])
			continue
		}
		t := orreryCycleSeconds * float64(i) / float64(keys)
		theta := spec.Phase + 2*math.Pi*float64(i)/float64(keys)*revolutions
		times[i] = t
		values[3*i] = spec.Radius * math.Cos(theta)
		values[3*i+1] = orreryHeartY
		values[3*i+2] = spec.Radius * math.Sin(theta)
	}
	return scene.AnimationChannel{
		TargetNode: indexOf(nodeID), Property: "translation", Interpolation: "LINEAR",
		Times: times, Values: values,
	}
}

// orreryMoonPark is the hidden parking pose below the opaque dais.
func orreryMoonPark() scene.Vector3 {
	return scene.Vec3(-0.18, -3, 2.2)
}

// orreryTransitChannel crosses the moon between the viewer and the heart. The
// mid-transit key time equals the heart-flare and halo-pulse key times, so
// light answers alignment on the same deterministic beat (13.2 s).
func orreryTransitChannel(target int) scene.AnimationChannel {
	mid := scene.Vec3(-0.18, orreryHeartY+0.08, 0.52)
	rise := scene.Vec3(mid.X, mid.Y, 2.6)
	exit := scene.Vec3(mid.X, mid.Y, -1.9)
	park := orreryMoonPark()
	vecs := []scene.Vector3{park, park, rise, mid, exit, park, park}
	times := []float64{0, orreryTransitRise - 0.3, orreryTransitRise, orreryTransitMid, orreryTransitExit, orreryTransitExit + 0.3, orreryCycleSeconds}
	values := make([]float64, 0, 3*len(vecs))
	for _, v := range vecs {
		values = append(values, v.X, v.Y, v.Z)
	}
	return scene.AnimationChannel{
		TargetNode: target, Property: "translation", Interpolation: "LINEAR",
		Times: times, Values: values,
	}
}

// orreryHeartEmissiveAnim ramps the heart from banked embers to cruise
// brightness during ignition, then flares at mid-transit. First and last
// values match so the material track loops cleanly.
func orreryHeartEmissiveAnim() scene.MaterialUniformAnim {
	return scene.MaterialUniformAnim{
		Uniform: "emissive", Arity: 1, Interp: "LINEAR",
		Times:  []float64{0, 3, 12.9, orreryTransitMid, 13.9, orreryCycleSeconds},
		Values: []float64{0.12, 1.85, 1.85, 3.4, 1.85, 0.12},
		Loop:   true, Duration: orreryCycleSeconds,
	}
}

// orreryHaloEmissiveAnim pulses the halo ring on the transit beat only.
func orreryHaloEmissiveAnim() scene.MaterialUniformAnim {
	return scene.MaterialUniformAnim{
		Uniform: "emissive", Arity: 1, Interp: "LINEAR",
		Times:  []float64{0, 12.6, orreryTransitMid, 14.3, orreryCycleSeconds},
		Values: []float64{0.06, 0.06, 1.7, 0.06, 0.06},
		Loop:   true, Duration: orreryCycleSeconds,
	}
}

// orreryStars builds the seeded background dome: 220 glow points scattered on
// a shell between radius 26 and 40, kept above the observatory floor line.
// A fixed-split LCG makes the field byte-for-byte reproducible.
func orreryStars() scene.Points {
	const (
		count      = orreryStarCount
		innerR     = 26.0
		shellDepth = 14.0
	)
	state := uint64(20260822)
	next := func() float64 {
		state = state*6364136223846793005 + 1442695040888963407
		return float64(state>>33) / float64(uint64(1)<<31)
	}
	positions := make([]scene.Vector3, 0, count)
	for len(positions) < count {
		x := next()*2 - 1
		y := next()*1.12 - 0.06
		z := next()*2 - 1
		length := math.Sqrt(x*x + y*y + z*z)
		if length < 0.2 || length > 1 {
			continue
		}
		radius := innerR + shellDepth*next()
		positions = append(positions, scene.Vec3(
			x/length*radius,
			y/length*radius,
			z/length*radius,
		))
	}
	return scene.Points{
		ID: "orrery-starfield", Count: len(positions), Positions: positions,
		Color: "#dfe4f5", Style: scene.PointStyleGlow, Size: 0.9,
		Opacity: 0.9, BlendMode: scene.BlendAdditive, Attenuation: true,
		MaxPixelSize: 2,
	}
}

// indexOfOrreryNode resolves a stable mesh ID to its graph index. Animation
// channels address nodes positionally; resolving through IDs keeps the
// declaration honest if the list ever grows.
func indexOfOrreryNode(nodes []scene.Node) func(string) int {
	index := make(map[string]int, len(nodes))
	for i, node := range nodes {
		if mesh, ok := node.(scene.Mesh); ok && mesh.ID != "" {
			index[mesh.ID] = i
		}
	}
	return func(id string) int {
		if i, ok := index[id]; ok {
			return i
		}
		panic("lodestar meridian: unknown animation target " + id)
	}
}
