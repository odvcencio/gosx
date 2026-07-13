package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

// GeometryZooProgram returns a cinematic Scene3D that showcases PBR material
// variety across seven hero primitives. The goal is to show what scene3d can
// actually do: ACES tonemapping, HDR bloom, three-point lighting, a glossy
// clearcoat floor for the metals to play against, and slow individual spin
// per hero mesh so highlights and reflections visibly travel across the
// gallery — on top of the gentle AutoRotate turntable and orbit controls.
func GeometryZooProgram() scene.Props {
	return scene.Props{
		Background:    "#0b0b0d", // matches shell --demo-bg exactly
		Responsive:    scene.Bool(true),
		FillHeight:    scene.Bool(true),
		Controls:      "orbit",
		AutoRotate:    scene.Bool(true), // gentle turntable keeps the scene alive
		Camera:        cinematicCamera(),
		ControlTarget: scene.Vec3(0, 0.6, 0),
		Environment: scene.Environment{
			// Very low ambient — the three-point rig does the real work.
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.12,
		},
		PostFX: postFX(),
		Graph:  scene.NewGraph(sceneNodes()...),
	}
}

// cinematicCamera positions above and back with a tight FOV for a gallery-
// showcase feel — not the default overhead bird-eye, not the flat close zoom.
func cinematicCamera() scene.PerspectiveCamera {
	return scene.PerspectiveCamera{
		Position: scene.Vec3(0, 2.6, 9.5),
		FOV:      52, // tighter than 60° → compressed, cinematic
		Near:     0.1,
		Far:      200,
	}
}

// postFX wires the HDR pipeline: ACES tonemapping for rich colour response,
// HDR bloom that picks up the metallic highlights and rim light silhouettes,
// a gentle vignette to frame the gallery, and a subtle cool colour grade that
// pulls the midtones toward the shell's cold-blue accent (#5fb4ff).
func postFX() scene.PostFX {
	return scene.PostFX{
		Effects: []scene.PostEffect{
			// ACES gives richer shadows and naturally saturated highlights.
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.15},
			// Bloom threshold set above the diffuse range so only specular
			// highlights and rim-lit silhouettes glow.
			scene.Bloom{Threshold: 0.85, Strength: 0.45, Radius: 14},
			// Vignette frames the gallery without crushing the corners.
			scene.Vignette{Intensity: 0.55},
			// Slight cool shift toward the shell accent blue in the midtones.
			scene.ColorGrade{Exposure: 1.0, Contrast: 1.05, Saturation: 1.08},
		},
	}
}

// sceneNodes returns every light and mesh in the scene.
func sceneNodes() []scene.Node {
	nodes := []scene.Node{}
	nodes = append(nodes, lights()...)
	nodes = append(nodes, floor())
	nodes = append(nodes, heroMeshes()...)
	return nodes
}

// lights implements a three-point rig plus hemisphere ambient:
//
//   - Key: warm-white directional from above-front-right, casts shadows.
//   - Fill: cool blue-tinted point light from opposite side; picks up the
//     shell's cold-blue accent to unify the palette.
//   - Rim: hot warm point placed behind the scene so silhouettes glow.
//   - Hemisphere: sky/ground gradient that supplements the rig in occluded areas.
//
// NOTE (intensity pass): a slow hue/intensity "breathing" drift on the fill
// or rim light was investigated and intentionally skipped. scene.AnimationClip
// only carries "translation"/"rotation"/"scale" channels targeting a node
// index (see scene.AnimationChannel in scene/scene.go) — there is no
// Color/Intensity channel. DirectionalLight, PointLight, and HemisphereLight
// also don't expose a Spin/Drift-style per-frame idiom the way scene.Mesh
// does; their only time-varying hook is Transition/Live, which reacts to
// live hub-pushed prop updates, not autonomous per-frame animation, and this
// demo has no hub/ticker wired in. Animating light color or intensity here
// declaratively would require a scene-package addition (e.g. an
// AnimationChannel Property of "color"/"intensity", or a Spin/Drift-shaped
// field on the light types) rather than a change local to this demo.
func lights() []scene.Node {
	return []scene.Node{
		// Key light — warm white directional, primary shadow caster.
		scene.DirectionalLight{
			ID:         "key",
			Color:      "#fff2e0", // warm white, slight amber tint
			Intensity:  1.5,
			Direction:  scene.Vec3(0.5, -1.0, -0.7),
			CastShadow: true,
			ShadowBias: -0.001,
			ShadowSize: 2048,
		},
		// Fill light — cool blue from the left-rear; unifies with --demo-accent-scene3d.
		scene.PointLight{
			ID:        "fill",
			Color:     "#9db8ff", // cold blue, echoes shell accent
			Intensity: 0.6,
			Position:  scene.Vec3(-4, 3, 2),
			Range:     22,
			Decay:     2,
		},
		// Rim light — hot warm point behind the scene, creates silhouette pop.
		scene.PointLight{
			ID:        "rim",
			Color:     "#ffe8c0", // warm amber-white
			Intensity: 2.2,
			Position:  scene.Vec3(1, 4, -8),
			Range:     28,
			Decay:     2,
		},
		// Hemisphere ambient — sky blue-grey, ground near-black, soft fill.
		scene.HemisphereLight{
			ID:          "hemi",
			SkyColor:    "#3a4f6a",
			GroundColor: "#1a1a1e",
			Intensity:   0.4,
		},
	}
}

// floor returns a large shadowed plane under the hero arc. Dark, glossy, and
// clearcoat-finished so the key/fill/rim rig throws tight, bright specular
// highlights across it — the gold and chrome heroes get something to play
// off rather than sinking into a flat matte void. This renderer has no
// planar/screen-space reflection pass for generic PBR meshes (the only
// mirror-style reflection pipeline lives inside the water demo's dedicated
// WaterSystem), so the floor does not mirror the actual mesh silhouettes —
// the gloss comes honestly from the clearcoat + low-roughness BRDF response
// to the light rig, not from a faked reflection.
func floor() scene.Node {
	return scene.Mesh{
		ID:       "floor",
		Geometry: scene.PlaneGeometry{Width: 18, Height: 18},
		Material: scene.StandardMaterial{
			Color:     "#101014",
			Roughness: 0.24,
			Metalness: 0.06,
			Clearcoat: 0.65,
		},
		Rotation:      scene.Rotate(-math.Pi/2, 0, 0),
		ReceiveShadow: true,
	}
}

// heroSlot describes one hero object in the gallery arc.
type heroSlot struct {
	angleDeg float64        // position on the arc in degrees
	geometry scene.Geometry // what shape
	material scene.StandardMaterial
	yOffset  float64     // vertical centre offset for tall/short shapes
	spin     scene.Euler // procedural rotation, radians/second (zero = static)
	name     string
}

// heroMeshes places seven PBR primitives along a gentle arc facing the camera.
// Material variety is the point: polished gold, brushed chrome, matte dark,
// dielectric gloss, rubber, glass-like, and stone/concrete.
//
// Motion (intensity pass): each non-spherical hero gets a slow, stately Spin
// (scene.Mesh.Spin, radians/second — the same declarative idiom the
// scene3d-bench mixed scene uses) so specular highlights and clearcoat sheen
// visibly travel across the surface as it turns. Two rules shaped the axis
// choices below:
//
//   - Perfectly smooth spheres (gold-sphere, rubber-sphere, stone-sphere) are
//     left un-spun on purpose. A uniformly-shaded sphere with no normal map
//     is rotationally symmetric about every axis through its centre — no
//     amount of spin changes a single lit pixel, so animating it would just
//     burn a MotionIR track for zero visible effect. Real motion on these
//     would need a surface feature (bump/normal map) this demo doesn't carry.
//   - Bodies of revolution around Y (the cylinder, and the torus's own
//     centre axis) are spun on X/Z instead of pure Y for the same reason —
//     spinning a solid of revolution about its own symmetry axis is another
//     invisible rotation. Tumbling on the other axes is what actually makes
//     their caps/tube catch and lose highlights.
func heroMeshes() []scene.Node {
	slots := []heroSlot{
		{
			// Polished gold — high metalness, very low roughness → sharp reflections.
			// Left static: a smooth sphere has no visible rotation (see doc above).
			angleDeg: 0,
			geometry: scene.SphereGeometry{Radius: 0.62, Segments: 40},
			material: scene.StandardMaterial{Color: "#d4a844", Roughness: 0.08, Metalness: 0.97},
			name:     "gold-sphere",
		},
		{
			// Brushed chrome box — high metalness, moderate roughness → anisotropic look.
			// Slow two-axis tumble: every facet edge sweeps a highlight in turn.
			angleDeg: 30,
			geometry: scene.BoxGeometry{Width: 1.05, Height: 1.05, Depth: 1.05},
			material: scene.StandardMaterial{Color: "#dce0e8", Roughness: 0.35, Metalness: 0.95},
			spin:     scene.Rotate(0.18, 0.32, 0),
			name:     "chrome-box",
		},
		{
			// Matte dark pyramid — low metalness, high roughness → absorbs light.
			// Slow single-axis yaw; matte doesn't shimmer, but the silhouette
			// gently turns against the warm rim light behind it.
			angleDeg: 60,
			geometry: scene.PyramidGeometry{Width: 1.0, Height: 1.4, Depth: 1.0},
			material: scene.StandardMaterial{Color: "#1c1c20", Roughness: 0.78, Metalness: 0.1},
			yOffset:  0.1,
			spin:     scene.Rotate(0, 0.26, 0),
			name:     "matte-pyramid",
		},
		{
			// Dielectric gloss cylinder — low metalness, low roughness → plastic/gloss.
			// Tumbles on X/Z (not Y — see doc above) so the gloss caps and the
			// curved side both catch the rig in turn.
			angleDeg: 90,
			geometry: scene.CylinderGeometry{RadiusTop: 0.4, RadiusBottom: 0.5, Height: 1.2, Segments: 32},
			material: scene.StandardMaterial{Color: "#e8edf5", Roughness: 0.12, Metalness: 0.04},
			spin:     scene.Rotate(0.22, 0, 0.14),
			name:     "dielectric-cylinder",
		},
		{
			// Gold torus — high metalness, warm medium roughness → aged gold.
			// Tumbles on two axes (not pure Y — see doc above) so the tube
			// itself visibly rolls, not just the whole ring in place.
			angleDeg: 120,
			geometry: scene.TorusGeometry{Radius: 0.52, Tube: 0.18, RadialSegments: 32, TubularSegments: 64},
			material: scene.StandardMaterial{Color: "#c9962a", Roughness: 0.38, Metalness: 0.82},
			spin:     scene.Rotate(0.2, 0.11, 0),
			name:     "aged-torus",
		},
		{
			// Rubber dark sphere — very low metalness, high roughness → matte rubber feel.
			// Left static: a smooth sphere has no visible rotation (see doc above).
			angleDeg: 150,
			geometry: scene.SphereGeometry{Radius: 0.58, Segments: 36},
			material: scene.StandardMaterial{Color: "#27272b", Roughness: 0.88, Metalness: 0.0},
			name:     "rubber-sphere",
		},
		{
			// Stone/concrete sphere — rough, slightly off-white, non-metallic.
			// Left static: a smooth sphere has no visible rotation (see doc above).
			angleDeg: 180,
			geometry: scene.SphereGeometry{Radius: 0.55, Segments: 32},
			material: scene.StandardMaterial{Color: "#7a7878", Roughness: 0.92, Metalness: 0.02},
			name:     "stone-sphere",
		},
	}

	// Arc parameters: radius 3.2, arc spans 0–180° in XZ plane.
	// Shift X by -radius/2 and Z by -radius/2 so the arc is centred and
	// faces toward +Z (the camera).
	const arcRadius = 3.2
	nodes := make([]scene.Node, 0, len(slots))
	for _, s := range slots {
		rad := s.angleDeg * math.Pi / 180.0
		x := arcRadius*math.Cos(rad) - arcRadius*0.5
		z := arcRadius*math.Sin(rad) - arcRadius*0.5
		y := 0.65 + s.yOffset

		nodes = append(nodes, scene.Mesh{
			ID:            s.name,
			Geometry:      s.geometry,
			Material:      s.material,
			Position:      scene.Vec3(x, y, z),
			CastShadow:    true,
			ReceiveShadow: true,
			Spin:          s.spin,
		})
	}
	return nodes
}
