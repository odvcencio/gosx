//go:build js && wasm

package jsgpu

import (
	"syscall/js"

	"github.com/odvcencio/gosx/client/jsutil"
	"github.com/odvcencio/gosx/render/gpu"
)

// queue wraps a GPUQueue.
type queue struct {
	js js.Value
}

func (q *queue) WriteBuffer(buf gpu.Buffer, offset int, data []byte) {
	b, ok := buf.(*buffer)
	if !ok || b == nil || len(data) == 0 {
		return
	}
	u8 := jsutil.NewUint8ArrayFromBytes(data)
	q.js.Call("writeBuffer", b.js, offset, u8)
}

func (q *queue) WriteTexture(dst gpu.Texture, data []byte, bytesPerRow, width, height int) {
	t, ok := dst.(*texture)
	if !ok || t == nil || len(data) == 0 {
		return
	}
	u8 := jsutil.NewUint8ArrayFromBytes(data)
	destination := map[string]any{
		"texture":  t.js,
		"mipLevel": 0,
		"origin":   map[string]any{"x": 0, "y": 0, "z": 0},
	}
	dataLayout := map[string]any{
		"offset":       0,
		"bytesPerRow":  bytesPerRow,
		"rowsPerImage": height,
	}
	size := map[string]any{
		"width":              width,
		"height":             height,
		"depthOrArrayLayers": 1,
	}
	q.js.Call("writeTexture", destination, u8, dataLayout, size)
}

func (q *queue) Submit(cmds ...gpu.CommandBuffer) {
	if len(cmds) == 0 {
		return
	}
	arr := make([]any, 0, len(cmds))
	for _, c := range cmds {
		cb, ok := c.(*commandBuffer)
		if !ok || cb == nil {
			continue
		}
		arr = append(arr, cb.js)
	}
	q.js.Call("submit", arr)
}

// buffer wraps a GPUBuffer.
type buffer struct {
	js    js.Value
	size  int
	usage gpu.BufferUsage
}

func (b *buffer) Size() int              { return b.size }
func (b *buffer) Usage() gpu.BufferUsage { return b.usage }
func (b *buffer) Destroy()               { b.js.Call("destroy") }

// ReadAsync maps the buffer for reading on the JS side, copies the mapped
// range out as Go bytes, and unmaps. The caller goroutine blocks on
// mapAsync — fine for the one-shot pick readback path; long-running
// streaming readback is a future optimization using persistent mapping.
func (b *buffer) ReadAsync(size int) ([]byte, error) {
	const modeRead = 0x0001 // GPUMapMode.READ (WebGPU spec constant)
	if _, err := jsutil.AwaitPromise(b.js.Call("mapAsync", modeRead)); err != nil {
		return nil, err
	}
	mapped := b.js.Call("getMappedRange")
	u8 := js.Global().Get("Uint8Array").New(mapped)
	out := make([]byte, size)
	js.CopyBytesToGo(out, u8)
	b.js.Call("unmap")
	return out, nil
}

// shaderModule wraps a GPUShaderModule.
type shaderModule struct {
	js js.Value
}

func (s *shaderModule) Destroy() {
	// WebGPU has no explicit shader-module destroy; GC handles it.
}

// renderPipeline wraps a GPURenderPipeline.
type renderPipeline struct {
	js js.Value
}

func (p *renderPipeline) GetBindGroupLayout(group int) gpu.BindGroupLayout {
	return &bindGroupLayout{js: p.js.Call("getBindGroupLayout", group)}
}

func (p *renderPipeline) Destroy() {
	// WebGPU has no explicit pipeline destroy; GC handles it.
}

// computePipeline wraps a GPUComputePipeline.
type computePipeline struct {
	js js.Value
}

func (p *computePipeline) GetBindGroupLayout(group int) gpu.BindGroupLayout {
	return &bindGroupLayout{js: p.js.Call("getBindGroupLayout", group)}
}

func (p *computePipeline) Destroy() {
	// WebGPU has no explicit pipeline destroy; GC handles it.
}

// bindGroupLayout wraps a GPUBindGroupLayout.
type bindGroupLayout struct {
	js js.Value
}

// bindGroup wraps a GPUBindGroup.
type bindGroup struct {
	js js.Value
}

func (b *bindGroup) Destroy() {
	// WebGPU has no explicit bind-group destroy; GC handles it.
}

// surface wraps a canvas + its GPUCanvasContext.
type surface struct {
	canvas js.Value
	ctx    js.Value
}

// textureView wraps a GPUTextureView.
type textureView struct {
	js js.Value
}

// sampler wraps a GPUSampler.
type sampler struct {
	js js.Value
}

func (*sampler) Destroy() {
	// WebGPU has no explicit sampler destroy; GC handles it.
}

// texture wraps a GPUTexture.
type texture struct {
	js     js.Value
	width  int
	height int
	format gpu.TextureFormat
}

func (t *texture) Width() int                { return t.width }
func (t *texture) Height() int               { return t.height }
func (t *texture) Format() gpu.TextureFormat { return t.format }

func (t *texture) CreateView() gpu.TextureView {
	return &textureView{js: t.js.Call("createView")}
}

func (t *texture) CreateLayerView(layer int) gpu.TextureView {
	return &textureView{js: t.js.Call("createView", map[string]any{
		"baseArrayLayer":  layer,
		"arrayLayerCount": 1,
		"dimension":       "2d",
	})}
}

func (t *texture) Destroy() {
	if !t.js.IsUndefined() {
		t.js.Call("destroy")
	}
}
