//go:build js && wasm

package jsgpu

import (
	"errors"
	"fmt"
	"syscall/js"

	"github.com/odvcencio/gosx/client/jsutil"
	"github.com/odvcencio/gosx/render/gpu"
)

// Device is the WebGPU-backed gpu.Device. Construct via Open.
type Device struct {
	gpuNS  js.Value // navigator.gpu
	dev    js.Value // GPUDevice
	queue  *queue
	format gpu.TextureFormat
}

// Open finds the canvas by ID, acquires an adapter + device, and returns a
// ready-to-use gpu.Device plus the Surface for that canvas. The same
// canvasID can be used across multiple Open calls (each produces a fresh
// device) but typical apps keep one Device for the lifetime of the canvas.
func Open(canvasID string) (*Device, gpu.Surface, error) {
	canvas := js.Global().Get("document").Call("getElementById", canvasID)
	if canvas.IsNull() || canvas.IsUndefined() {
		return nil, nil, fmt.Errorf("jsgpu: canvas #%s not found", canvasID)
	}
	gpuNS := js.Global().Get("navigator").Get("gpu")
	if gpuNS.IsUndefined() {
		return nil, nil, errors.New("jsgpu: navigator.gpu unavailable")
	}

	adapter, err := jsutil.AwaitPromise(gpuNS.Call("requestAdapter"))
	if err != nil {
		return nil, nil, fmt.Errorf("jsgpu: requestAdapter: %w", err)
	}
	if adapter.IsNull() || adapter.IsUndefined() {
		return nil, nil, errors.New("jsgpu: no adapter available")
	}

	devVal, err := jsutil.AwaitPromise(adapter.Call("requestDevice"))
	if err != nil {
		return nil, nil, fmt.Errorf("jsgpu: requestDevice: %w", err)
	}

	format := parseCanvasFormat(gpuNS.Call("getPreferredCanvasFormat").String())

	ctx := canvas.Call("getContext", "webgpu")
	if ctx.IsNull() || ctx.IsUndefined() {
		return nil, nil, errors.New("jsgpu: canvas.getContext('webgpu') returned null")
	}
	ctx.Call("configure", map[string]any{
		"device":    devVal,
		"format":    encodeCanvasFormat(format),
		"alphaMode": "premultiplied",
	})

	d := &Device{
		gpuNS:  gpuNS,
		dev:    devVal,
		format: format,
	}
	d.queue = &queue{js: devVal.Get("queue")}

	return d, &surface{canvas: canvas, ctx: ctx}, nil
}

// Queue returns the default submission queue.
func (d *Device) Queue() gpu.Queue { return d.queue }

// PreferredSurfaceFormat returns the format the swap chain was configured with.
func (d *Device) PreferredSurfaceFormat() gpu.TextureFormat { return d.format }

// CreateBuffer allocates a GPU buffer.
func (d *Device) CreateBuffer(desc gpu.BufferDesc) (gpu.Buffer, error) {
	if desc.Size <= 0 {
		return nil, fmt.Errorf("jsgpu: %w: buffer size must be > 0", gpu.ErrInvalidDesc)
	}
	dict := map[string]any{
		"size":  desc.Size,
		"usage": encodeBufferUsage(desc.Usage),
	}
	if desc.Label != "" {
		dict["label"] = desc.Label
	}
	js := d.dev.Call("createBuffer", dict)
	return &buffer{js: js, size: desc.Size, usage: desc.Usage}, nil
}

// CreateTexture allocates a GPU texture.
func (d *Device) CreateTexture(desc gpu.TextureDesc) (gpu.Texture, error) {
	if desc.Width <= 0 || desc.Height <= 0 {
		return nil, fmt.Errorf("jsgpu: %w: texture dimensions must be > 0", gpu.ErrInvalidDesc)
	}
	layers := desc.DepthOrArrayLayers
	if layers == 0 {
		layers = 1
	}
	sampleCount := desc.SampleCount
	if sampleCount == 0 {
		sampleCount = 1
	}
	dict := map[string]any{
		"size": map[string]any{
			"width":              desc.Width,
			"height":             desc.Height,
			"depthOrArrayLayers": layers,
		},
		"format":      encodeTextureFormat(desc.Format),
		"usage":       encodeTextureUsage(desc.Usage),
		"sampleCount": sampleCount,
	}
	if desc.Label != "" {
		dict["label"] = desc.Label
	}
	js := d.dev.Call("createTexture", dict)
	return &texture{
		js:     js,
		width:  desc.Width,
		height: desc.Height,
		format: desc.Format,
	}, nil
}

// CreateShaderModule compiles a WGSL source module.
func (d *Device) CreateShaderModule(desc gpu.ShaderDesc) (gpu.ShaderModule, error) {
	if desc.SourceWGSL == "" {
		return nil, fmt.Errorf("jsgpu: %w: empty shader source", gpu.ErrInvalidDesc)
	}
	dict := map[string]any{"code": desc.SourceWGSL}
	if desc.Label != "" {
		dict["label"] = desc.Label
	}
	js := d.dev.Call("createShaderModule", dict)
	return &shaderModule{js: js}, nil
}

// CreateRenderPipeline compiles a render pipeline. Only AutoLayout is
// supported in R1; explicit pipeline layouts land in R2 alongside textures.
func (d *Device) CreateRenderPipeline(desc gpu.RenderPipelineDesc) (gpu.RenderPipeline, error) {
	if desc.Vertex.Module == nil {
		return nil, fmt.Errorf("jsgpu: %w: nil vertex shader module", gpu.ErrInvalidDesc)
	}
	if desc.Fragment.Module == nil {
		return nil, fmt.Errorf("jsgpu: %w: nil fragment shader module", gpu.ErrInvalidDesc)
	}
	if !desc.AutoLayout {
		return nil, fmt.Errorf("jsgpu: %w: only AutoLayout pipelines are supported in R1", gpu.ErrUnsupported)
	}

	dict := map[string]any{
		"layout": "auto",
		"vertex": map[string]any{
			"module":     desc.Vertex.Module.(*shaderModule).js,
			"entryPoint": desc.Vertex.EntryPoint,
			"buffers":    encodeVertexBuffers(desc.Vertex.Buffers),
		},
		"fragment": map[string]any{
			"module":     desc.Fragment.Module.(*shaderModule).js,
			"entryPoint": desc.Fragment.EntryPoint,
			"targets":    encodeColorTargets(desc.Fragment.Targets),
		},
		"primitive": encodePrimitive(desc.Primitive),
	}
	if desc.DepthStencil != nil {
		dict["depthStencil"] = encodeDepthStencil(*desc.DepthStencil)
	}
	if desc.Label != "" {
		dict["label"] = desc.Label
	}

	js := d.dev.Call("createRenderPipeline", dict)
	return &renderPipeline{js: js}, nil
}

// CreateBindGroup creates a bind group. Layout must come from a pipeline's
// GetBindGroupLayout for AutoLayout pipelines.
func (d *Device) CreateBindGroup(desc gpu.BindGroupDesc) (gpu.BindGroup, error) {
	layout, ok := desc.Layout.(*bindGroupLayout)
	if !ok || layout == nil {
		return nil, fmt.Errorf("jsgpu: %w: nil bind-group layout", gpu.ErrInvalidDesc)
	}
	entries := make([]any, 0, len(desc.Entries))
	for _, e := range desc.Entries {
		if e.Buffer == nil {
			return nil, fmt.Errorf("jsgpu: %w: bind-group entry missing buffer", gpu.ErrInvalidDesc)
		}
		b := e.Buffer.(*buffer)
		res := map[string]any{"buffer": b.js}
		if e.Offset > 0 {
			res["offset"] = e.Offset
		}
		if e.Size > 0 {
			res["size"] = e.Size
		}
		entries = append(entries, map[string]any{
			"binding":  e.Binding,
			"resource": res,
		})
	}
	dict := map[string]any{
		"layout":  layout.js,
		"entries": entries,
	}
	if desc.Label != "" {
		dict["label"] = desc.Label
	}
	js := d.dev.Call("createBindGroup", dict)
	return &bindGroup{js: js}, nil
}

// CreateCommandEncoder returns a fresh command encoder.
func (d *Device) CreateCommandEncoder() gpu.CommandEncoder {
	return &commandEncoder{js: d.dev.Call("createCommandEncoder")}
}

// AcquireSurfaceView returns a texture view for the current swap-chain image.
func (d *Device) AcquireSurfaceView(s gpu.Surface) (gpu.TextureView, error) {
	sf, ok := s.(*surface)
	if !ok || sf == nil {
		return nil, fmt.Errorf("jsgpu: %w: expected *surface", gpu.ErrInvalidDesc)
	}
	tex := sf.ctx.Call("getCurrentTexture")
	if tex.IsNull() || tex.IsUndefined() {
		return nil, errors.New("jsgpu: getCurrentTexture returned null")
	}
	return &textureView{js: tex.Call("createView")}, nil
}

// Destroy releases the device. Resources created through it become invalid.
func (d *Device) Destroy() {
	if !d.dev.IsUndefined() {
		d.dev.Call("destroy")
	}
}
