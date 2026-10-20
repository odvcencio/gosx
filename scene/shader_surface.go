package scene

import (
	"fmt"
	"math"

	"m31labs.dev/gosx/engine"
)

// ShaderShape places the same Selena material on a flat surface or a 3D form.
type ShaderShape string

const (
	ShaderPlane  ShaderShape = "plane"
	ShaderSphere ShaderShape = "sphere"
	ShaderBox    ShaderShape = "box"
	ShaderTorus  ShaderShape = "torus"
)

// ShaderSurface is a bounded Scene3D surface for a compiled Selena material.
// Render its EngineConfig with the normal engine mount and semantic HTML
// fallback. Text and controls remain ordinary GSX alongside the surface.
// It uses the existing WebGPU/WebGL renderer, not a second canvas runtime.
type ShaderSurface struct {
	Material CustomMaterial
	Shape    ShaderShape // empty selects the flat plane
	MeshID   string      // default "shader-shape", usable by motion bindings
	Label    string
	Width    int
	Height   int
	// Interactive enables orbit and automatic rotation for 3D forms only.
	Interactive bool
	// Zero values select modest defaults: 30 FPS, DPR 1.5, two million pixels.
	MaxFrameRate        float64
	MaxDevicePixelRatio float64
	MaxPixels           int
}

// Scene returns ordinary Props so callers can compose cameras, post effects,
// capability policy and other Scene3D features before mounting the surface.
func (s ShaderSurface) Scene() (Props, error) {
	if s.Width < 0 || s.Height < 0 || s.MaxPixels < 0 || !finiteNonnegative(s.MaxFrameRate) || !finiteNonnegative(s.MaxDevicePixelRatio) {
		return Props{}, fmt.Errorf("scene: shader surface dimensions and budgets must be finite and nonnegative")
	}
	var geometry Geometry
	rotation := Euler{X: math.Pi / 2}
	shape := s.Shape
	if shape == "" {
		shape = ShaderPlane
	}
	switch shape {
	case ShaderPlane:
		geometry = PlaneGeometry{Width: 18, Height: 12}
	case ShaderSphere:
		geometry = SphereGeometry{Radius: 2.5, Segments: 48}
	case ShaderBox:
		geometry = BoxGeometry{Width: 4, Height: 4, Depth: 4}
	case ShaderTorus:
		geometry = TorusGeometry{Radius: 2.2, Tube: .7, RadialSegments: 24, TubularSegments: 64}
	default:
		return Props{}, fmt.Errorf("scene: unknown shader shape %q", shape)
	}
	if shape != ShaderPlane {
		rotation = Euler{X: .9, Y: .25}
	}
	id := s.MeshID
	if id == "" {
		id = "shader-shape"
	}
	fps, dpr, pixels := s.MaxFrameRate, s.MaxDevicePixelRatio, s.MaxPixels
	if fps == 0 {
		fps = 30
	}
	if dpr == 0 {
		dpr = 1.5
	}
	if pixels == 0 {
		pixels = 2_000_000
	}
	props := Props{
		Width: s.Width, Height: s.Height, Label: s.Label,
		Background: "transparent", CanvasAlpha: Bool(true), Responsive: Bool(true), FillHeight: Bool(true),
		MaxFrameRate: fps, MaxDevicePixelRatio: dpr, MaxPixels: pixels, AdaptiveQuality: Bool(true),
		AutoRotate: Bool(s.Interactive && shape != ShaderPlane), DragToRotate: Bool(s.Interactive && shape != ShaderPlane),
		Camera: PerspectiveCamera{Position: Vec3(0, 0, 10), FOV: 50, Near: .1, Far: 100},
		Graph:  NewGraph(Mesh{ID: id, Geometry: geometry, Material: s.Material, Rotation: rotation}),
	}
	if shape == ShaderPlane {
		// Explicit bounds keep the plane edge-to-edge at every viewport aspect.
		// The shader's UV domain stays 0..1, including ultrawide and portrait.
		props.OrthographicCamera = &OrthographicCamera{Position: Vec3(0, 0, 10), Left: -9, Right: 9, Top: 6, Bottom: -6, Near: .1, Far: 100}
	}
	return props, nil
}

// EngineConfig lowers the surface through the regular Scene3D engine.
func (s ShaderSurface) EngineConfig() (engine.Config, error) {
	props, err := s.Scene()
	if err != nil {
		return engine.Config{}, err
	}
	return props.EngineConfig(), nil
}

func finiteNonnegative(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}
