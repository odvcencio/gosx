//go:build !js || !wasm

package stub

import (
	"errors"
	"testing"

	"github.com/odvcencio/gosx/render/gpu"
)

// TestStubReturnsUnsupported verifies the stub device returns ErrUnsupported
// from every constructor on native Go builds. This is the contract that
// lets native code import gpu/ without linking a real backend.
func TestStubReturnsUnsupported(t *testing.T) {
	d := New()

	cases := []struct {
		name string
		err  error
	}{
		{
			"CreateBuffer",
			mustErr(func() error { _, err := d.CreateBuffer(gpu.BufferDesc{Size: 16}); return err }),
		},
		{
			"CreateTexture",
			mustErr(func() error {
				_, err := d.CreateTexture(gpu.TextureDesc{Width: 16, Height: 16})
				return err
			}),
		},
		{
			"CreateSampler",
			mustErr(func() error {
				_, err := d.CreateSampler(gpu.SamplerDesc{})
				return err
			}),
		},
		{
			"CreateShaderModule",
			mustErr(func() error { _, err := d.CreateShaderModule(gpu.ShaderDesc{SourceWGSL: ""}); return err }),
		},
		{
			"CreateRenderPipeline",
			mustErr(func() error { _, err := d.CreateRenderPipeline(gpu.RenderPipelineDesc{}); return err }),
		},
		{
			"CreateBindGroup",
			mustErr(func() error { _, err := d.CreateBindGroup(gpu.BindGroupDesc{}); return err }),
		},
		{
			"AcquireSurfaceView",
			mustErr(func() error { _, err := d.AcquireSurfaceView(nil); return err }),
		},
	}

	for _, tc := range cases {
		if !errors.Is(tc.err, gpu.ErrUnsupported) {
			t.Fatalf("%s: want gpu.ErrUnsupported, got %v", tc.name, tc.err)
		}
	}
}

func mustErr(fn func() error) error { return fn() }
