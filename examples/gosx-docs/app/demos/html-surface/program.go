package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

// HTMLSurfaceProgram builds the diegetic-panel scene.
//
// A texture-mode scene.HTML lowers to a real quad in the scene graph with the
// rasterized markup as its texture. That is not a DOM overlay positioned over
// the canvas: the panel has a world transform, sits behind and in front of
// other geometry by depth, and can be viewed from any angle.
//
// Three details in this program are the whole lesson:
//
//  1. Rotation.X = -math.Pi/2 stands the panel up. Surfaces are authored in
//     the XZ plane, so an unrotated panel lies flat on the floor and is
//     edge-on (invisible) to a camera that looks along -Z. Every panel here
//     sets it.
//  2. TextureWidth and TextureHeight are CSS pixels — the box the markup is
//     laid out in. SurfaceWidth and SurfaceHeight are world units. Keeping
//     the two ratios equal is what stops the text from stretching.
//  3. The angled panel is deliberate. A wall-mounted readout is rarely viewed
//     head-on, and an oblique panel is the honest test of whether the raster
//     is legible.
func HTMLSurfaceProgram() scene.Props {
	upright := scene.Euler{X: -math.Pi / 2}
	return scene.Props{
		Label:      "HTML surfaces rendered as textures on 3D geometry",
		AriaLabel:  "HTML surfaces rendered as textures on 3D geometry",
		Background: "#080d14",
		Responsive: scene.Bool(true),
		FillHeight: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vector3{X: 0, Y: 0.75, Z: 4.6},
			FOV:      52,
			Near:     0.1,
			Far:      120,
		},
		Environment: scene.Environment{
			AmbientColor:     "#1a2635",
			AmbientIntensity: 0.6,
		},
		Graph: scene.NewGraph(
			scene.DirectionalLight{
				ID:        "key",
				Color:     "#ffffff",
				Intensity: 1.1,
				Direction: scene.Vector3{X: -0.4, Y: -0.8, Z: -0.5},
			},
			// A floor gives the panels somewhere to stand and proves the
			// surface is in the scene rather than over it.
			scene.Mesh{
				ID:       "floor",
				Geometry: scene.BoxGeometry{Width: 14, Height: 0.08, Depth: 14},
				Position: scene.Vector3{X: 0, Y: -1.35, Z: 0},
				Material: scene.StandardMaterial{Color: "#141d2a", Roughness: 0.85, Metalness: 0.05},
			},
			// A solid block behind the centre panel. When the panel is
			// dragged behind it the panel is occluded, which a DOM overlay
			// can never do.
			scene.Mesh{
				ID:       "pillar",
				Geometry: scene.BoxGeometry{Width: 0.5, Height: 2.2, Depth: 0.5},
				Position: scene.Vector3{X: 2.85, Y: -0.25, Z: -1.4},
				Material: scene.StandardMaterial{Color: "#25405c", Roughness: 0.4, Metalness: 0.3},
			},

			// Panel 1: head-on. The legibility baseline.
			scene.HTML{
				ID:               "panel-status",
				Mode:             scene.HTMLTexture,
				Position:         scene.Vector3{X: -1.5, Y: 0.9, Z: 0},
				Rotation:         upright,
				SurfaceWidth:     2.0,
				SurfaceHeight:    1.25,
				TextureWidth:     640,
				TextureHeight:    400,
				MaxTexturePixels: 2048 * 2048,
				ClassName:        "diegetic-panel diegetic-panel--status",
				Markup: `<div class="diegetic-panel__inner">
  <p class="diegetic-panel__eyebrow">REACTOR / SECTOR 7</p>
  <h2 class="diegetic-panel__title">Coolant Loop</h2>
  <dl class="diegetic-panel__grid">
    <div><dt>Flow</dt><dd>412 L/min</dd></div>
    <div><dt>Pressure</dt><dd>2.14 MPa</dd></div>
    <div><dt>Delta T</dt><dd>18.6 K</dd></div>
    <div><dt>Status</dt><dd class="is-ok">NOMINAL</dd></div>
  </dl>
  <p class="diegetic-panel__foot">Real CSS grid. Real webfont. One texture.</p>
</div>`,
			},

			// Panel 2: rotated away from the camera. This is the diegetic
			// case and the one that exposes oblique-angle legibility.
			scene.HTML{
				ID:               "panel-angled",
				Mode:             scene.HTMLTexture,
				Position:         scene.Vector3{X: 1.55, Y: 0.9, Z: -0.35},
				Rotation:         scene.Euler{X: -math.Pi / 2, Y: -0.62},
				SurfaceWidth:     2.0,
				SurfaceHeight:    1.25,
				TextureWidth:     640,
				TextureHeight:    400,
				MaxTexturePixels: 2048 * 2048,
				ClassName:        "diegetic-panel diegetic-panel--angled",
				Markup: `<div class="diegetic-panel__inner">
  <p class="diegetic-panel__eyebrow">WALL TERMINAL</p>
  <h2 class="diegetic-panel__title">Viewed at 36&#176;</h2>
  <ul class="diegetic-panel__list">
    <li>Flexbox and grid lay out normally.</li>
    <li>Page tokens reach the surface.</li>
    <li>The raster follows device pixel ratio.</li>
  </ul>
  <p class="diegetic-panel__foot">Rotation.Y = -0.62 rad</p>
</div>`,
			},

			// Panel 3: a floor-mounted readout, slowly turning. Spin proves
			// the surface transform is live, not baked at load.
			scene.HTML{
				ID:               "panel-floor",
				Mode:             scene.HTMLTexture,
				Position:         scene.Vector3{X: 0, Y: -1.24, Z: 1.35},
				Rotation:         scene.Euler{},
				Spin:             scene.Euler{Y: 0.25},
				SurfaceWidth:     1.6,
				SurfaceHeight:    0.9,
				TextureWidth:     512,
				TextureHeight:    288,
				MaxTexturePixels: 2048 * 2048,
				ClassName:        "diegetic-panel diegetic-panel--floor",
				Markup: `<div class="diegetic-panel__inner diegetic-panel__inner--compact">
  <p class="diegetic-panel__eyebrow">FLOOR PLATE</p>
  <h2 class="diegetic-panel__title">Lying flat</h2>
  <p class="diegetic-panel__foot">No rotation, constant Spin.Y</p>
</div>`,
			},
		),
	}
}
