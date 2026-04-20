//go:build js && wasm

package jsgpu

import (
	"syscall/js"

	"github.com/odvcencio/gosx/render/gpu"
)

// commandEncoder wraps a GPUCommandEncoder.
type commandEncoder struct {
	js js.Value
}

func (c *commandEncoder) BeginRenderPass(desc gpu.RenderPassDesc) gpu.RenderPassEncoder {
	return &renderPassEncoder{js: c.js.Call("beginRenderPass", encodeRenderPassDesc(desc))}
}

func (c *commandEncoder) Finish() gpu.CommandBuffer {
	return &commandBuffer{js: c.js.Call("finish")}
}

// commandBuffer wraps a GPUCommandBuffer.
type commandBuffer struct {
	js js.Value
}

// renderPassEncoder wraps a GPURenderPassEncoder.
type renderPassEncoder struct {
	js js.Value
}

func (e *renderPassEncoder) SetPipeline(p gpu.RenderPipeline) {
	rp, ok := p.(*renderPipeline)
	if !ok || rp == nil {
		return
	}
	e.js.Call("setPipeline", rp.js)
}

func (e *renderPassEncoder) SetBindGroup(group int, bg gpu.BindGroup) {
	bgv, ok := bg.(*bindGroup)
	if !ok || bgv == nil {
		return
	}
	e.js.Call("setBindGroup", group, bgv.js)
}

func (e *renderPassEncoder) SetVertexBuffer(slot int, buf gpu.Buffer) {
	b, ok := buf.(*buffer)
	if !ok || b == nil {
		return
	}
	e.js.Call("setVertexBuffer", slot, b.js)
}

func (e *renderPassEncoder) SetIndexBuffer(buf gpu.Buffer, format gpu.IndexFormat) {
	b, ok := buf.(*buffer)
	if !ok || b == nil {
		return
	}
	e.js.Call("setIndexBuffer", b.js, encodeIndexFormat(format))
}

func (e *renderPassEncoder) Draw(vertexCount, instanceCount, firstVertex, firstInstance int) {
	if instanceCount == 0 {
		instanceCount = 1
	}
	e.js.Call("draw", vertexCount, instanceCount, firstVertex, firstInstance)
}

func (e *renderPassEncoder) DrawIndexed(indexCount, instanceCount, firstIndex, baseVertex, firstInstance int) {
	if instanceCount == 0 {
		instanceCount = 1
	}
	e.js.Call("drawIndexed", indexCount, instanceCount, firstIndex, baseVertex, firstInstance)
}

func (e *renderPassEncoder) End() { e.js.Call("end") }
