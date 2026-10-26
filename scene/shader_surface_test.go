package scene

import (
	"encoding/json"
	"math"
	"testing"
)

func TestShaderSurfaceBothBackendsAndShapes(t *testing.T) {
	material, _, err := CompileSelenaMaterial([]byte(selenaDefaultsSource), SelenaMaterialOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, shape := range []ShaderShape{ShaderPlane, ShaderSphere, ShaderBox, ShaderTorus} {
		props, err := (ShaderSurface{Material: material, Shape: shape, Interactive: true}).Scene()
		if err != nil {
			t.Fatal(err)
		}
		cfg := props.EngineConfig()
		var wire map[string]any
		if err := json.Unmarshal(cfg.Props, &wire); err != nil {
			t.Fatal(err)
		}
		if props.MaxPixels != 2000000 || props.MaxFrameRate != 30 || props.MaxDevicePixelRatio != 1.5 {
			t.Fatalf("unbounded surface: %+v", props)
		}
		if (props.OrthographicCamera != nil) != (shape == ShaderPlane) || *props.DragToRotate != (shape != ShaderPlane) {
			t.Fatalf("camera/control policy for %s", shape)
		}
		mesh := props.Graph.Nodes[0].(Mesh)
		custom := mesh.Material.(CustomMaterial)
		if custom.VertexWGSL == "" || custom.FragmentWGSL == "" || custom.VertexGLSL == "" || custom.FragmentGLSL == "" {
			t.Fatalf("surface lost compiled browser programs for %s", shape)
		}
	}
}

func TestShaderSurfaceRejectsInvalidShapeAndBudgets(t *testing.T) {
	for _, surface := range []ShaderSurface{{Shape: "unknown"}, {MaxPixels: -1}, {MaxFrameRate: math.NaN()}, {MaxDevicePixelRatio: math.Inf(1)}} {
		if _, err := surface.EngineConfig(); err == nil {
			t.Fatalf("accepted invalid surface %+v", surface)
		}
	}
}
