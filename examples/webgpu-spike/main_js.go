//go:build js && wasm

// Spike: prove that a Go/WASM module can drive navigator.gpu through syscall/js
// end-to-end — adapter → device → swapchain configuration → pipeline → draw.
// Deliberately NOT using render/ or engine/ abstractions; the point is to find
// out what the syscall/js WebGPU path actually looks like before we design an
// abstraction around it.
package main

import (
	"errors"
	"fmt"
	"math"
	"syscall/js"
)

const wgslShader = `
struct Uniforms {
  mvp : mat4x4<f32>,
};
@group(0) @binding(0) var<uniform> u : Uniforms;

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) color : vec3<f32>,
};

@vertex
fn vs_main(
  @location(0) pos : vec3<f32>,
  @location(1) color : vec3<f32>,
) -> VSOut {
  var out : VSOut;
  out.pos = u.mvp * vec4<f32>(pos, 1.0);
  out.color = color;
  return out;
}

@fragment
fn fs_main(in : VSOut) -> @location(0) vec4<f32> {
  return vec4<f32>(in.color, 1.0);
}
`

// Cube with per-face colors so the rotation is actually visible.
// 6 faces * 2 triangles * 3 verts = 36 verts; interleaved pos(3) + color(3).
func cubeVertices() []float32 {
	faces := []struct {
		corners [4][3]float32
		color   [3]float32
	}{
		{[4][3]float32{{-1, -1, 1}, {1, -1, 1}, {1, 1, 1}, {-1, 1, 1}}, [3]float32{1, 0.3, 0.2}},       // +Z
		{[4][3]float32{{1, -1, -1}, {-1, -1, -1}, {-1, 1, -1}, {1, 1, -1}}, [3]float32{0.2, 0.8, 0.3}}, // -Z
		{[4][3]float32{{-1, 1, 1}, {1, 1, 1}, {1, 1, -1}, {-1, 1, -1}}, [3]float32{0.3, 0.5, 1}},       // +Y
		{[4][3]float32{{-1, -1, -1}, {1, -1, -1}, {1, -1, 1}, {-1, -1, 1}}, [3]float32{1, 0.9, 0.2}},   // -Y
		{[4][3]float32{{1, -1, 1}, {1, -1, -1}, {1, 1, -1}, {1, 1, 1}}, [3]float32{0.9, 0.2, 0.8}},     // +X
		{[4][3]float32{{-1, -1, -1}, {-1, -1, 1}, {-1, 1, 1}, {-1, 1, -1}}, [3]float32{0.2, 0.9, 0.9}}, // -X
	}

	out := make([]float32, 0, 6*6*6)
	for _, face := range faces {
		tris := [][3]int{{0, 1, 2}, {0, 2, 3}}
		for _, tri := range tris {
			for _, idx := range tri {
				c := face.corners[idx]
				out = append(out, c[0], c[1], c[2], face.color[0], face.color[1], face.color[2])
			}
		}
	}
	return out
}

// Tiny column-major 4x4 matrix helpers. No generics, no external deps.
type mat4 [16]float32

func mat4Identity() mat4 {
	var m mat4
	m[0] = 1
	m[5] = 1
	m[10] = 1
	m[15] = 1
	return m
}

func mat4Mul(a, b mat4) mat4 {
	var r mat4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			sum := float32(0)
			for k := 0; k < 4; k++ {
				sum += a[k*4+row] * b[col*4+k]
			}
			r[col*4+row] = sum
		}
	}
	return r
}

func mat4Perspective(fovRad, aspect, near, far float32) mat4 {
	f := float32(1.0) / float32(math.Tan(float64(fovRad/2)))
	nf := 1 / (near - far)
	var m mat4
	m[0] = f / aspect
	m[5] = f
	m[10] = (far + near) * nf
	m[11] = -1
	m[14] = (2 * far * near) * nf
	return m
}

func mat4Translate(x, y, z float32) mat4 {
	m := mat4Identity()
	m[12] = x
	m[13] = y
	m[14] = z
	return m
}

func mat4RotateY(angleRad float32) mat4 {
	c := float32(math.Cos(float64(angleRad)))
	s := float32(math.Sin(float64(angleRad)))
	m := mat4Identity()
	m[0] = c
	m[2] = -s
	m[8] = s
	m[10] = c
	return m
}

func mat4RotateX(angleRad float32) mat4 {
	c := float32(math.Cos(float64(angleRad)))
	s := float32(math.Sin(float64(angleRad)))
	m := mat4Identity()
	m[5] = c
	m[6] = s
	m[9] = -s
	m[10] = c
	return m
}

// awaitPromise blocks the calling goroutine on a JS Promise and returns the
// resolved value or an error. This is the foundational primitive for driving
// the WebGPU API (adapter, device, pipeline-async all return Promises).
func awaitPromise(p js.Value) (js.Value, error) {
	if p.IsNull() || p.IsUndefined() {
		return js.Undefined(), errors.New("awaitPromise: null promise")
	}
	done := make(chan js.Value, 1)
	errc := make(chan js.Value, 1)
	then := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			done <- args[0]
		} else {
			done <- js.Undefined()
		}
		return nil
	})
	catch := js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 {
			errc <- args[0]
		} else {
			errc <- js.Undefined()
		}
		return nil
	})
	defer then.Release()
	defer catch.Release()
	p.Call("then", then).Call("catch", catch)
	select {
	case v := <-done:
		return v, nil
	case e := <-errc:
		return js.Undefined(), fmt.Errorf("promise rejected: %v", jsDescribe(e))
	}
}

func jsDescribe(v js.Value) string {
	if v.IsNull() {
		return "<null>"
	}
	if v.IsUndefined() {
		return "<undefined>"
	}
	if v.Type() == js.TypeString {
		return v.String()
	}
	if v.Get("message").Type() == js.TypeString {
		return v.Get("message").String()
	}
	return v.Call("toString").String()
}

// float32SliceToUint8 reinterprets a []float32 as the []byte view needed to
// write into a js.TypedArray without copying the bytes twice.
func float32ToBytes(src []float32) []byte {
	out := make([]byte, len(src)*4)
	for i, f := range src {
		bits := math.Float32bits(f)
		out[i*4+0] = byte(bits)
		out[i*4+1] = byte(bits >> 8)
		out[i*4+2] = byte(bits >> 16)
		out[i*4+3] = byte(bits >> 24)
	}
	return out
}

type renderer struct {
	canvas   js.Value
	ctx      js.Value
	device   js.Value
	queue    js.Value
	format   string
	pipeline js.Value
	vbuf     js.Value
	ubuf     js.Value
	bindGrp  js.Value
	vcount   int
}

func initRenderer(canvasID string) (*renderer, error) {
	doc := js.Global().Get("document")
	canvas := doc.Call("getElementById", canvasID)
	if canvas.IsNull() || canvas.IsUndefined() {
		return nil, fmt.Errorf("canvas #%s not found", canvasID)
	}

	gpu := js.Global().Get("navigator").Get("gpu")
	if gpu.IsUndefined() {
		return nil, errors.New("navigator.gpu unavailable — WebGPU not supported")
	}

	adapter, err := awaitPromise(gpu.Call("requestAdapter"))
	if err != nil {
		return nil, fmt.Errorf("requestAdapter: %w", err)
	}
	if adapter.IsNull() || adapter.IsUndefined() {
		return nil, errors.New("no WebGPU adapter available")
	}

	device, err := awaitPromise(adapter.Call("requestDevice"))
	if err != nil {
		return nil, fmt.Errorf("requestDevice: %w", err)
	}

	ctx := canvas.Call("getContext", "webgpu")
	if ctx.IsNull() || ctx.IsUndefined() {
		return nil, errors.New("canvas.getContext('webgpu') returned null")
	}
	format := gpu.Call("getPreferredCanvasFormat").String()
	ctx.Call("configure", map[string]any{
		"device":    device,
		"format":    format,
		"alphaMode": "premultiplied",
	})

	shaderModule := device.Call("createShaderModule", map[string]any{"code": wgslShader})

	vertexBufferLayout := map[string]any{
		"arrayStride": 6 * 4, // 6 floats * 4 bytes
		"attributes": []any{
			map[string]any{"shaderLocation": 0, "offset": 0, "format": "float32x3"},
			map[string]any{"shaderLocation": 1, "offset": 3 * 4, "format": "float32x3"},
		},
	}

	pipeline := device.Call("createRenderPipeline", map[string]any{
		"layout": "auto",
		"vertex": map[string]any{
			"module":     shaderModule,
			"entryPoint": "vs_main",
			"buffers":    []any{vertexBufferLayout},
		},
		"fragment": map[string]any{
			"module":     shaderModule,
			"entryPoint": "fs_main",
			"targets":    []any{map[string]any{"format": format}},
		},
		"primitive": map[string]any{
			"topology": "triangle-list",
			"cullMode": "back",
		},
	})

	verts := cubeVertices()
	vbytes := float32ToBytes(verts)
	vbuf := device.Call("createBuffer", map[string]any{
		"size":  len(vbytes),
		"usage": 0x20 | 0x08, // GPUBufferUsage.VERTEX | COPY_DST
	})
	writeBufferBytes(device.Get("queue"), vbuf, vbytes)

	// Uniform buffer: one mat4 (64 bytes), writable each frame.
	ubuf := device.Call("createBuffer", map[string]any{
		"size":  64,
		"usage": 0x40 | 0x08, // UNIFORM | COPY_DST
	})
	bindGrp := device.Call("createBindGroup", map[string]any{
		"layout": pipeline.Call("getBindGroupLayout", 0),
		"entries": []any{
			map[string]any{"binding": 0, "resource": map[string]any{"buffer": ubuf}},
		},
	})

	return &renderer{
		canvas:   canvas,
		ctx:      ctx,
		device:   device,
		queue:    device.Get("queue"),
		format:   format,
		pipeline: pipeline,
		vbuf:     vbuf,
		ubuf:     ubuf,
		bindGrp:  bindGrp,
		vcount:   len(verts) / 6,
	}, nil
}

// writeBufferBytes copies Go bytes into a GPU buffer via a Uint8Array view.
func writeBufferBytes(queue, buffer js.Value, data []byte) {
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	queue.Call("writeBuffer", buffer, 0, u8)
}

func (r *renderer) frame(tSeconds float64) {
	w := r.canvas.Get("width").Int()
	h := r.canvas.Get("height").Int()
	if w <= 0 || h <= 0 {
		return
	}
	aspect := float32(w) / float32(h)

	proj := mat4Perspective(math.Pi/3, aspect, 0.1, 100)
	view := mat4Translate(0, 0, -4)
	model := mat4Mul(mat4RotateY(float32(tSeconds)*0.9), mat4RotateX(float32(tSeconds)*0.5))
	mvp := mat4Mul(mat4Mul(proj, view), model)

	writeBufferBytes(r.queue, r.ubuf, float32ToBytes(mvp[:]))

	view0 := r.ctx.Call("getCurrentTexture").Call("createView")

	encoder := r.device.Call("createCommandEncoder")
	pass := encoder.Call("beginRenderPass", map[string]any{
		"colorAttachments": []any{map[string]any{
			"view":       view0,
			"clearValue": map[string]any{"r": 0.05, "g": 0.06, "b": 0.08, "a": 1},
			"loadOp":     "clear",
			"storeOp":    "store",
		}},
	})
	pass.Call("setPipeline", r.pipeline)
	pass.Call("setBindGroup", 0, r.bindGrp)
	pass.Call("setVertexBuffer", 0, r.vbuf)
	pass.Call("draw", r.vcount)
	pass.Call("end")

	cmdBuf := encoder.Call("finish")
	r.queue.Call("submit", []any{cmdBuf})
}

func main() {
	js.Global().Set("gosxWebGPUSpikeStart", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.ValueOf(map[string]any{"ok": false, "error": "need canvasID"})
		}
		canvasID := args[0].String()

		promiseCtor := js.Global().Get("Promise")
		return promiseCtor.New(js.FuncOf(func(this js.Value, args []js.Value) any {
			resolve := args[0]
			reject := args[1]
			go func() {
				r, err := initRenderer(canvasID)
				if err != nil {
					reject.Invoke(js.ValueOf(err.Error()))
					return
				}
				start := js.Global().Get("performance").Call("now").Float()
				var raf js.Func
				raf = js.FuncOf(func(this js.Value, args []js.Value) any {
					var tMs float64
					if len(args) > 0 && args[0].Type() == js.TypeNumber {
						tMs = args[0].Float()
					} else {
						tMs = js.Global().Get("performance").Call("now").Float()
					}
					r.frame((tMs - start) / 1000)
					js.Global().Call("requestAnimationFrame", raf)
					return nil
				})
				js.Global().Call("requestAnimationFrame", raf)
				resolve.Invoke(js.ValueOf(map[string]any{
					"ok":     true,
					"format": r.format,
				}))
			}()
			return nil
		}))
	}))

	select {}
}
