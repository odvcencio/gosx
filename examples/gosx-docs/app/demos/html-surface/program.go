package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

// HTMLSurfaceProgram shows texture-mode HTML on world geometry. The browser
// lays out markup in CSS pixels. Scene3D maps the raster to a quad. The quad
// rotates, occludes, and uses post-processing.
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
			AmbientIntensity: 0.38,
		},
		Graph: scene.NewGraph(
			scene.DirectionalLight{
				ID:        "key",
				Color:     "#ffffff",
				Intensity: 0.72,
				Direction: scene.Vector3{X: -0.4, Y: -0.8, Z: -0.5},
			},
			scene.Mesh{
				ID:       "floor",
				Geometry: scene.BoxGeometry{Width: 8, Height: 0.18, Depth: 5},
				Position: scene.Vector3{X: 0, Y: -1.47, Z: 0},
				Material: scene.StandardMaterial{Color: "#2a3d46", Roughness: 0.85, Metalness: 0.05},
			},
			scene.Mesh{
				ID:       "left-plinth",
				Geometry: scene.BoxGeometry{Width: 2.35, Height: 0.42, Depth: 0.75},
				Position: scene.Vector3{X: -1.5, Y: -1.17, Z: -0.25},
				Material: scene.StandardMaterial{Color: "#8e5a35", Roughness: 0.72, Metalness: 0.05},
			},
			scene.Mesh{
				ID:       "right-plinth",
				Geometry: scene.BoxGeometry{Width: 2.35, Height: 0.42, Depth: 0.75},
				Position: scene.Vector3{X: 1.5, Y: -1.17, Z: -0.45},
				Material: scene.StandardMaterial{Color: "#8e5a35", Roughness: 0.72, Metalness: 0.05},
			},
			scene.Mesh{
				ID:       "pillar",
				Geometry: scene.BoxGeometry{Width: 0.5, Height: 2.2, Depth: 0.5},
				Position: scene.Vector3{X: 2.85, Y: -0.25, Z: -1.4},
				Material: scene.StandardMaterial{Color: "#25405c", Roughness: 0.4, Metalness: 0.3},
			},
			scene.HTML{
				ID:               "panel-status",
				Mode:             scene.HTMLTexture,
				Position:         scene.Vector3{X: -1.5, Y: 0.9, Z: 0},
				Rotation:         upright,
				SurfaceWidth:     2.0,
				SurfaceHeight:    1.25,
				TextureWidth:     640,
				TextureHeight:    400,
				MaxTexturePixels: scene.HTMLTextureMaxPixels2048,
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
			scene.HTML{
				ID:               "panel-angled",
				Mode:             scene.HTMLTexture,
				Position:         scene.Vector3{X: 1.55, Y: 0.9, Z: -0.35},
				Rotation:         scene.Euler{X: -math.Pi / 2, Y: -0.62},
				SurfaceWidth:     2.0,
				SurfaceHeight:    1.25,
				TextureWidth:     640,
				TextureHeight:    400,
				MaxTexturePixels: scene.HTMLTextureMaxPixels2048,
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
			scene.HTML{
				ID:               "panel-floor",
				Mode:             scene.HTMLTexture,
				Position:         scene.Vector3{X: 0, Y: -1.24, Z: 1.35},
				Spin:             scene.Euler{Y: 0.25},
				SurfaceWidth:     1.6,
				SurfaceHeight:    0.9,
				TextureWidth:     512,
				TextureHeight:    288,
				MaxTexturePixels: scene.HTMLTextureMaxPixels2048,
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
