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
