//go:build js && wasm

package jsgpu

import "m31labs.dev/gosx/render/gpu"

// encodeRenderPassDesc is the one descriptor encoder that reaches into a
// js.Value: a render pass names its attachments by live GPUTextureView. The
// rest of the mapping lives in encode.go and needs no browser.
func encodeRenderPassDesc(desc gpu.RenderPassDesc) map[string]any {
	color := make([]any, 0, len(desc.ColorAttachments))
	for _, a := range desc.ColorAttachments {
		view, _ := a.View.(*textureView)
		att := map[string]any{
			"view":    view.js,
			"loadOp":  encodeLoadOp(a.LoadOp),
			"storeOp": encodeStoreOp(a.StoreOp),
		}
		if a.LoadOp == gpu.LoadOpClear {
			att["clearValue"] = map[string]any{
				"r": a.ClearValue.R,
				"g": a.ClearValue.G,
				"b": a.ClearValue.B,
				"a": a.ClearValue.A,
			}
		}
		color = append(color, att)
	}
	out := map[string]any{"colorAttachments": color}
	if ds := desc.DepthStencilAttachment; ds != nil {
		view, _ := ds.View.(*textureView)
		out["depthStencilAttachment"] = map[string]any{
			"view":            view.js,
			"depthLoadOp":     encodeLoadOp(ds.DepthLoadOp),
			"depthStoreOp":    encodeStoreOp(ds.DepthStoreOp),
			"depthClearValue": ds.DepthClearValue,
		}
	}
	if desc.Label != "" {
		out["label"] = desc.Label
	}
	return out
}
