//go:build !js || !wasm

// Package stub provides a non-WASM implementation of gpu.Device that returns
// ErrUnsupported from every constructor. It exists so that packages depending
// on render/gpu (render/bundle, server-side bundle validators, etc.) compile
// and link on native Go builds without pulling in syscall/js.
package stub

import (
	"github.com/odvcencio/gosx/render/gpu"
)

// Device is a no-op Device that satisfies the gpu.Device interface for
// non-WASM builds. All constructors return gpu.ErrUnsupported.
type Device struct{}

// New returns a stub device. Useful for code that needs to accept a gpu.Device
// in test harnesses or pure-Go validators.
func New() *Device { return &Device{} }

func (*Device) Queue() gpu.Queue                                          { return stubQueue{} }
func (*Device) PreferredSurfaceFormat() gpu.TextureFormat                 { return gpu.FormatUndefined }
func (*Device) CreateBuffer(gpu.BufferDesc) (gpu.Buffer, error)           { return nil, gpu.ErrUnsupported }
func (*Device) CreateTexture(gpu.TextureDesc) (gpu.Texture, error) { return nil, gpu.ErrUnsupported }
func (*Device) CreateSampler(gpu.SamplerDesc) (gpu.Sampler, error) { return nil, gpu.ErrUnsupported }
func (*Device) CreateShaderModule(gpu.ShaderDesc) (gpu.ShaderModule, error) {
	return nil, gpu.ErrUnsupported
}
func (*Device) CreateRenderPipeline(gpu.RenderPipelineDesc) (gpu.RenderPipeline, error) {
	return nil, gpu.ErrUnsupported
}
func (*Device) CreateComputePipeline(gpu.ComputePipelineDesc) (gpu.ComputePipeline, error) {
	return nil, gpu.ErrUnsupported
}
func (*Device) CreateBindGroup(gpu.BindGroupDesc) (gpu.BindGroup, error) {
	return nil, gpu.ErrUnsupported
}
func (*Device) CreateCommandEncoder() gpu.CommandEncoder                   { return nil }
func (*Device) AcquireSurfaceView(gpu.Surface) (gpu.TextureView, error) {
	return nil, gpu.ErrUnsupported
}
func (*Device) OnLost(func(string, string)) {}
func (*Device) Destroy()                    {}

type stubQueue struct{}

func (stubQueue) WriteBuffer(gpu.Buffer, int, []byte)             {}
func (stubQueue) WriteTexture(gpu.Texture, []byte, int, int, int) {}
func (stubQueue) Submit(...gpu.CommandBuffer)                     {}
