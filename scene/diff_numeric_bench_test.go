package scene

import (
	"encoding/json"
	"fmt"
	"runtime"
	"testing"
)

// This file holds the unchanged-scene diff benchmark. It uses only DiffScene,
// benchObjectIR and the helpers below, so the same file runs against any revision
// of diff.go. The before and after numbers in the changelog come from running it
// on both.

// benchMeshIndexCounts lists the triangle-index count of each mesh in the
// benchmark scene: 120 meshes and 164,082 indices. A few large tile bodies carry
// most of the indices, a middle group carries decorations, and many small meshes
// carry dividers, outlines and shadows.
func benchMeshIndexCounts() []int {
	counts := make([]int, 0, 120)
	add := func(meshes, indices int) {
		for range meshes {
			counts = append(counts, indices)
		}
	}
	add(4, 8505)
	add(8, 6561)
	add(12, 2682)
	add(20, 1152)
	add(75, 294)
	add(1, 300)
	return counts
}

// benchFloats returns n finite floats that depend on the seed and the position.
func benchFloats(seed, n int) []float64 {
	values := make([]float64, n)
	for index := range values {
		values[index] = float64((index*31+seed*17)%1000)/1000 - 0.5
	}
	return values
}

// benchMeshVertices builds one indexed mesh the way lowering builds it: unique
// vertex streams plus a triangle index list over them.
func benchMeshVertices(seed, indexCount int, tangents bool) *MeshVertices {
	count := max(indexCount/4, 3)
	revision := uint64(seed + 1)
	mesh := &MeshVertices{
		Count:     count,
		Positions: benchFloats(seed, count*3),
		Normals:   benchFloats(seed+1, count*3),
		UVs:       benchFloats(seed+2, count*2),
		Indices:   make([]uint32, indexCount),
		Immutable: true,
		Revision:  &revision,
	}
	if tangents {
		mesh.Tangents = benchFloats(seed+3, count*4)
	}
	for index := range mesh.Indices {
		mesh.Indices[index] = uint32((index*7 + seed) % count)
	}
	return mesh
}

// benchRequirements stands in for a struct that a decoded shader layout holds
// next to its JSON-shaped values.
type benchRequirements struct {
	GLExtensions []string
	Features     []string
}

// benchShaderLayoutJSON is a shader layout of the size a custom material carries:
// about 140 maps, slices and scalars.
const benchShaderLayoutJSON = `{"attributes":[{"location":0,"name":"position","type":"vec3"},{"location":1,"name":"normal","type":"vec3"},{"location":2,"name":"uv","type":"vec2"}],"context":["environmentInfo","cameraPos","sunDir","sunColor","ambient"],"entryPoints":{"fragment":"fragmentMain","fragmentSpecular":"fragmentMainSpecular","vertex":"vertexMain"},"kind":"mesh","languageVersion":"selena.lang.v1","material":"BoneBody","metal":{"buffer":0},"schemaVersion":"selena.descriptor.v1","specularOutput":{"authored":false,"colorSpace":"linear","glExtensions":["WEBGL_draw_buffers"],"location":1,"semantic":"specular","type":"vec4"},"textures":[{"dimension":"cube","gl":{"uniform":"environmentRadiance","unit":0},"metal":{"sampler":0,"texture":0},"name":"environmentRadiance","wgsl":{"group":0,"samplerBinding":2,"textureBinding":1}}],"uniformBlock":{"defaults":[{"name":"variant","type":"float","values":[0]},{"name":"bodyColor","type":"vec3","values":[0.888,0.831,0.716]},{"name":"toneColor","type":"vec3","values":[0.888,0.831,0.716]},{"name":"roughness","type":"float","values":[0.46]},{"name":"coat","type":"float","values":[0.18]},{"name":"specular","type":"float","values":[0.38]}],"fields":[{"name":"mvp","offset":0,"size":64,"type":"mat4"},{"name":"normalMatrix","offset":64,"size":48,"type":"mat3"},{"name":"variant","offset":112,"size":4,"type":"float"},{"name":"bodyColor","offset":128,"size":12,"type":"vec3"},{"name":"toneColor","offset":144,"size":12,"type":"vec3"},{"name":"roughness","offset":156,"size":4,"type":"float"},{"name":"coat","offset":160,"size":4,"type":"float"},{"name":"specular","offset":164,"size":4,"type":"float"},{"class":"context","name":"environmentInfo","offset":176,"size":16,"type":"vec4"},{"class":"context","name":"cameraPos","offset":192,"size":12,"type":"vec3"},{"class":"context","name":"sunDir","offset":208,"size":12,"type":"vec3"},{"class":"context","name":"sunColor","offset":224,"size":12,"type":"vec3"},{"class":"context","name":"ambient","offset":240,"size":12,"type":"vec3"}],"size":256},"webglTarget":"gles","wgsl":{"binding":0,"group":0}}`

// benchShaderSource is one shader-sized string. Every object shares it, the way
// a material cache hands the same source string to every object that uses it.
var benchShaderSource = func() string {
	source := make([]byte, 0, 32<<10)
	for len(source) < 32<<10 {
		source = append(source, "vec4 shade(vec3 normal, vec3 light) { return vec4(max(dot(normal, light), 0.0)); }\n"...)
	}
	return string(source)
}()

// benchShaderLayout decodes a fresh layout tree. Each call returns maps and
// slices that no other call shares.
func benchShaderLayout(tb testing.TB) map[string]any {
	tb.Helper()
	var layout map[string]any
	if err := json.Unmarshal([]byte(benchShaderLayoutJSON), &layout); err != nil {
		tb.Fatal(err)
	}
	layout["targetRequires"] = map[string]any{
		"wgsl":  benchRequirements{},
		"glsl":  benchRequirements{GLExtensions: []string{"OES_standard_derivatives", "EXT_shader_texture_lod"}},
		"metal": benchRequirements{},
		"gles":  benchRequirements{},
	}
	return layout
}

// benchMeshObject builds one mesh record of the benchmark scene. Two calls with
// the same arguments return equal records that share no slice, map or pointer,
// except the shader source strings.
func benchMeshObject(tb testing.TB, index, indexCount int) ObjectIR {
	tb.Helper()
	record := benchObjectIR(index)
	record.ID = fmt.Sprintf("tile/%03d", index)
	record.Kind = "gltf-mesh"
	record.MaterialKind = "custom"
	record.Vertices = benchMeshVertices(index, indexCount, index < 44)
	if index%8 != 7 {
		record.ShaderBackend = "selena"
		record.CustomVertex = benchShaderSource
		record.CustomFragment = benchShaderSource
		record.CustomVertexWGSL = benchShaderSource
		record.CustomFragmentWGSL = benchShaderSource
		record.ShaderLayout = benchShaderLayout(tb)
		record.CustomUniforms = map[string]any{
			"sunColor":            []float64{1, 0.96, 0.9},
			"coat":                0.18,
			"environmentRadiance": "gosx:environment:radiance",
			"variant":             float64(index % 4),
			"toneColor":           []float64{0.888, 0.831, 0.716},
			"specular":            0.38,
			"ambient":             []float64{0.2, 0.22, 0.25},
			"cameraPos":           []float64{0, 6, 7},
			"sunDir":              []float64{-0.4, -1, -0.3},
			"bodyColor":           []float64{0.888, 0.831, 0.716},
			"roughness":           0.46,
		}
	}
	return record
}

// benchMeshScene builds a lowered scene shaped like a board-game table: 120 mesh
// objects holding 164,082 indices and 469,488 vertex floats, a shader layout and
// a uniform table on seven of every eight, three lights and seven labels.
func benchMeshScene(tb testing.TB) SceneIR {
	tb.Helper()
	counts := benchMeshIndexCounts()
	ir := SceneIR{Schema: SceneIRSchema, Objects: make([]ObjectIR, len(counts))}
	for index, indexCount := range counts {
		ir.Objects[index] = benchMeshObject(tb, index, indexCount)
	}
	for index := range 3 {
		ir.Lights = append(ir.Lights, LightIR{
			ID: fmt.Sprintf("light/%d", index), Kind: "directional", Color: "#fff1d6", Intensity: 1.1,
			DirectionX: 0.3, DirectionY: -1, DirectionZ: -0.5, CastShadow: index == 0,
		})
	}
	for index := range 7 {
		ir.Labels = append(ir.Labels, LabelIR{
			ID: fmt.Sprintf("label/%d", index), Text: fmt.Sprintf("seat %d", index), X: float64(index), Y: 1.5,
		})
	}
	return ir
}

// shareStreams returns a scene whose records hold the very slices, maps and
// pointers of the original. It is what a caller gets from caching lowered
// geometry between frames.
func shareStreams(ir SceneIR) SceneIR {
	shared := ir
	shared.Objects = append([]ObjectIR(nil), ir.Objects...)
	return shared
}

// BenchmarkDiffSceneUnchangedMeshes measures DiffScene on a scene that did not
// change, with the options the table client passes. This is the case every room
// update that moves nothing in the scene still pays for.
//
// independent-copies builds the two scenes separately, so they share no slice or
// map: lowering deep-copies the vertex streams, and the diff has to compare every
// element. shared-streams hands both scenes the same slices and maps.
func BenchmarkDiffSceneUnchangedMeshes(b *testing.B) {
	options := DiffOptions{PatchObjectState: true}
	run := func(b *testing.B, previous, next SceneIR) {
		b.Helper()
		if diff := DiffScene(previous, next, options); len(diff.Commands) != 0 || len(diff.RemountFields) != 0 {
			b.Fatalf("unchanged scene produced %d commands and remount fields %v", len(diff.Commands), diff.RemountFields)
		}
		// Collect the garbage the setup made, so a collection it would have
		// started does not run inside the timed loop.
		runtime.GC()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			DiffScene(previous, next, options)
		}
	}
	b.Run("independent-copies", func(b *testing.B) {
		run(b, benchMeshScene(b), benchMeshScene(b))
	})
	b.Run("shared-streams", func(b *testing.B) {
		previous := benchMeshScene(b)
		run(b, previous, shareStreams(previous))
	})
}
