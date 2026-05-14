package bundle

import (
	"fmt"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

const worldLineWGSL = `
struct Scene {
  viewProj : mat4x4<f32>,
};

@group(0) @binding(0) var<uniform> scene : Scene;

struct VSOut {
  @builtin(position) pos : vec4<f32>,
  @location(0) color : vec4<f32>,
};

struct FSOut {
  @location(0) color  : vec4<f32>,
  @location(1) pickId : u32,
};

@vertex
fn vs_main(
  @location(0) pos : vec3<f32>,
  @location(1) color : vec4<f32>,
) -> VSOut {
  var out : VSOut;
  out.pos = scene.viewProj * vec4<f32>(pos, 1.0);
  out.color = color;
  return out;
}

@fragment
fn fs_main(in : VSOut) -> FSOut {
  var out : FSOut;
  out.color = in.color;
  out.pickId = 0u;
  return out;
}
`

func (r *Renderer) buildWorldLinePipeline() error {
	shader, err := r.device.CreateShaderModule(gpu.ShaderDesc{
		SourceWGSL: worldLineWGSL,
		Label:      "bundle.worldLine",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildWorldLinePipeline: %w", err)
	}
	pipeline, err := r.device.CreateRenderPipeline(gpu.RenderPipelineDesc{
		Vertex: gpu.VertexStageDesc{
			Module:     shader,
			EntryPoint: "vs_main",
			Buffers: []gpu.VertexBufferLayout{
				{ArrayStride: 12, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 0, Offset: 0, Format: gpu.VertexFormatFloat32x3},
				}},
				{ArrayStride: 16, StepMode: gpu.StepVertex, Attributes: []gpu.VertexAttribute{
					{ShaderLocation: 1, Offset: 0, Format: gpu.VertexFormatFloat32x4},
				}},
			},
		},
		Fragment: gpu.FragmentStageDesc{
			Module:     shader,
			EntryPoint: "fs_main",
			Targets: []gpu.ColorTargetState{
				{Format: r.hdrFormat, Blend: alphaBlendState(), WriteMask: gpu.ColorWriteAll},
				{Format: gpu.FormatR32Uint, WriteMask: gpu.ColorWriteAll},
			},
		},
		Primitive: gpu.PrimitiveState{
			Topology:  gpu.TopologyLineList,
			CullMode:  gpu.CullNone,
			FrontFace: gpu.FrontFaceCCW,
		},
		DepthStencil: &gpu.DepthStencilState{
			Format:            r.depthFormat,
			DepthWriteEnabled: false,
			DepthCompare:      gpu.CompareLessEqual,
		},
		AutoLayout: true,
		Label:      "bundle.worldLine",
	})
	if err != nil {
		return fmt.Errorf("bundle.buildWorldLinePipeline: %w", err)
	}
	r.worldLinePipeline = pipeline
	r.worldLineBGLayout = pipeline.GetBindGroupLayout(0)
	return nil
}

func (r *Renderer) drawWorldLines(pass gpu.RenderPassEncoder, b engine.RenderBundle) error {
	positions, colors, vertexCount := worldLineBufferBytes(b)
	if vertexCount <= 1 {
		destroyWorldLineResources(r.worldLineCache)
		r.worldLineCache = nil
		return nil
	}
	res, err := r.ensureWorldLineBuffers(positions, colors, vertexCount)
	if err != nil {
		return err
	}
	if res == nil || res.vertexCount == 0 {
		return nil
	}
	pass.SetPipeline(r.worldLinePipeline)
	pass.SetBindGroup(0, r.worldLineBindGrp)
	pass.SetVertexBuffer(0, res.positions)
	pass.SetVertexBuffer(1, res.colors)
	pass.Draw(res.vertexCount, 1, 0, 0)
	return nil
}

func worldLineBufferBytes(b engine.RenderBundle) ([]byte, []byte, int) {
	if len(b.Objects) == 0 {
		vertexCount := worldLineVertexCount(b)
		if vertexCount <= 0 {
			return nil, nil, 0
		}
		return float64sToFloat32Bytes(b.WorldPositions[:vertexCount*3]), worldLineColorBytes(b.WorldColors, vertexCount), vertexCount
	}
	positions := make([]float64, 0, len(b.WorldPositions))
	colors := make([]float64, 0, len(b.WorldColors))
	for _, object := range b.Objects {
		if nativeObjectDrawable(b, object) || object.ViewCulled || object.VertexCount <= 0 {
			continue
		}
		positionStart := object.VertexOffset * 3
		positionEnd := positionStart + object.VertexCount*3
		if positionStart < 0 || positionEnd > len(b.WorldPositions) || positionStart > positionEnd {
			continue
		}
		positions = append(positions, b.WorldPositions[positionStart:positionEnd]...)
		if objectComponentRangeOK(b.WorldColors, object, 4) {
			colorStart := object.VertexOffset * 4
			colorEnd := colorStart + object.VertexCount*4
			colors = append(colors, b.WorldColors[colorStart:colorEnd]...)
		}
	}
	vertexCount := len(positions) / 3
	if vertexCount%2 != 0 {
		vertexCount--
		positions = positions[:vertexCount*3]
		if len(colors) >= (vertexCount+1)*4 {
			colors = colors[:vertexCount*4]
		}
	}
	if vertexCount <= 0 {
		return nil, nil, 0
	}
	return float64sToFloat32Bytes(positions), worldLineColorBytes(colors, vertexCount), vertexCount
}

func worldLineVertexCount(b engine.RenderBundle) int {
	vertexCount := b.WorldVertexCount
	if vertexCount <= 0 {
		vertexCount = len(b.WorldPositions) / 3
	}
	vertexCount = min(vertexCount, len(b.WorldPositions)/3)
	if len(b.WorldColors) > 0 {
		vertexCount = min(vertexCount, len(b.WorldColors)/4)
	}
	if vertexCount%2 != 0 {
		vertexCount--
	}
	if vertexCount < 0 {
		return 0
	}
	return vertexCount
}

func (r *Renderer) ensureWorldLineBuffers(positions, colors []byte, vertexCount int) (*worldLineResources, error) {
	if cached := r.worldLineCache; cached != nil && cached.positionLen == len(positions) && cached.colorLen == len(colors) {
		r.device.Queue().WriteBuffer(cached.positions, 0, positions)
		r.device.Queue().WriteBuffer(cached.colors, 0, colors)
		cached.vertexCount = vertexCount
		return cached, nil
	}
	destroyWorldLineResources(r.worldLineCache)
	posBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(positions),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.worldLine.positions",
	})
	if err != nil {
		r.worldLineCache = nil
		return nil, fmt.Errorf("bundle.worldLine: create positions buffer: %w", err)
	}
	colorBuf, err := r.device.CreateBuffer(gpu.BufferDesc{
		Size:  len(colors),
		Usage: gpu.BufferUsageVertex | gpu.BufferUsageCopyDst,
		Label: "bundle.worldLine.colors",
	})
	if err != nil {
		posBuf.Destroy()
		r.worldLineCache = nil
		return nil, fmt.Errorf("bundle.worldLine: create colors buffer: %w", err)
	}
	res := &worldLineResources{
		positions:   posBuf,
		colors:      colorBuf,
		positionLen: len(positions),
		colorLen:    len(colors),
		vertexCount: vertexCount,
	}
	r.worldLineCache = res
	r.device.Queue().WriteBuffer(posBuf, 0, positions)
	r.device.Queue().WriteBuffer(colorBuf, 0, colors)
	return res, nil
}

func worldLineColorBytes(colors []float64, vertexCount int) []byte {
	if len(colors) >= vertexCount*4 {
		return float64sToFloat32Bytes(colors[:vertexCount*4])
	}
	values := make([]float32, vertexCount*4)
	for i := 0; i < vertexCount; i++ {
		values[i*4+0] = 1
		values[i*4+1] = 1
		values[i*4+2] = 1
		values[i*4+3] = 1
	}
	return float32sToBytes(values)
}

func destroyWorldLineResources(res *worldLineResources) {
	if res == nil {
		return
	}
	if res.positions != nil {
		res.positions.Destroy()
	}
	if res.colors != nil {
		res.colors.Destroy()
	}
}
