package bundle

import (
	"fmt"
	"math"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/render/gpu"
)

func (r *Renderer) prepareObjectMeshResources(b engine.RenderBundle) error {
	if len(b.Objects) == 0 {
		for key, res := range r.objectMeshCache {
			destroyObjectMeshResources(res)
			delete(r.objectMeshCache, key)
		}
		return nil
	}
	live := make(map[string]struct{})
	for i, object := range b.Objects {
		if !nativeObjectDrawable(b, object) {
			continue
		}
		key := objectMeshKey(i, object)
		if _, err := r.ensureObjectMeshResources(key, i, b, object); err != nil {
			return err
		}
		live[key] = struct{}{}
	}
	for key, res := range r.objectMeshCache {
		if _, ok := live[key]; ok {
			continue
		}
		destroyObjectMeshResources(res)
		delete(r.objectMeshCache, key)
	}
	return nil
}

func (r *Renderer) drawObjectMeshes(pass gpu.RenderPassEncoder, b engine.RenderBundle) error {
	if len(b.Objects) == 0 {
		return nil
	}
	pass.SetPipeline(r.litPipeline)
	pass.SetBindGroup(0, r.litBindGrp)
	for i, object := range b.Objects {
		if !nativeObjectDrawable(b, object) {
			continue
		}
		res := r.objectMeshCache[objectMeshKey(i, object)]
		if res == nil || res.vertexCount == 0 || res.instance == nil {
			continue
		}
		fp := resolveObjectMaterialFingerprint(b, object)
		mat, err := r.ensureMaterial(fp)
		if err != nil {
			return fmt.Errorf("bundle.objectMesh: ensure material: %w", err)
		}
		pass.SetBindGroup(1, mat.bindGroup)
		pass.SetVertexBuffer(0, res.positions)
		pass.SetVertexBuffer(1, res.colors)
		pass.SetVertexBuffer(2, res.normals)
		pass.SetVertexBuffer(3, res.uvs)
		pass.SetVertexBuffer(4, res.instance)
		pass.Draw(res.vertexCount, 1, 0, 0)
	}
	return nil
}

func resolveObjectMaterialFingerprint(b engine.RenderBundle, object engine.RenderObject) materialFingerprint {
	if object.MaterialIndex < 0 || object.MaterialIndex >= len(b.Materials) {
		return defaultVertexColorMaterial()
	}
	return materialFromRender(b.Materials[object.MaterialIndex])
}

func objectMeshKey(index int, object engine.RenderObject) string {
	return fmt.Sprintf("%d:%s:%d:%d:%d", index, object.ID, object.MaterialIndex, object.VertexOffset, object.VertexCount)
}

func nativeObjectDrawable(b engine.RenderBundle, object engine.RenderObject) bool {
	if object.ViewCulled || object.VertexCount < 3 || object.VertexCount%3 != 0 {
		return false
	}
	if !objectComponentRangeOK(b.WorldPositions, object, 3) {
		return false
	}
	// Legacy engine-VM objects use WorldPositions as line-list data and do not
	// carry normals or UVs. Require at least one mesh attribute stream so the
	// native path does not reinterpret helper lines as triangles.
	return objectComponentRangeOK(b.WorldNormals, object, 3) || objectComponentRangeOK(b.WorldUVs, object, 2)
}

func objectComponentRangeOK(values []float64, object engine.RenderObject, components int) bool {
	if components <= 0 || object.VertexOffset < 0 || object.VertexCount <= 0 {
		return false
	}
	start := object.VertexOffset * components
	end := start + object.VertexCount*components
	return start >= 0 && start <= end && end <= len(values)
}

func (r *Renderer) ensureObjectMeshResources(key string, index int, b engine.RenderBundle, object engine.RenderObject) (*objectMeshResources, error) {
	positions := objectPositionBytes(b, object)
	colors := objectColorBytes(b, object)
	normals := objectNormalBytes(b, object)
	uvs := objectUVBytes(b, object)
	instance := objectInstanceBytes(r.pickBaseForObject(index))
	if len(positions) == 0 || len(colors) == 0 || len(normals) == 0 || len(uvs) == 0 || len(instance) == 0 {
		return nil, nil
	}
	if cached := r.objectMeshCache[key]; cached != nil &&
		cached.positionLen == len(positions) &&
		cached.colorLen == len(colors) &&
		cached.normalLen == len(normals) &&
		cached.uvLen == len(uvs) &&
		cached.instanceLen == len(instance) {
		r.device.Queue().WriteBuffer(cached.positions, 0, positions)
		r.device.Queue().WriteBuffer(cached.colors, 0, colors)
		r.device.Queue().WriteBuffer(cached.normals, 0, normals)
		r.device.Queue().WriteBuffer(cached.uvs, 0, uvs)
		r.device.Queue().WriteBuffer(cached.instance, 0, instance)
		cached.vertexCount = object.VertexCount
		return cached, nil
	}
	if old := r.objectMeshCache[key]; old != nil {
		destroyObjectMeshResources(old)
	}
	posBuf, err := r.uploadVertexBytes(positions, "bundle.object.positions:"+key)
	if err != nil {
		return nil, err
	}
	colorBuf, err := r.uploadVertexBytes(colors, "bundle.object.colors:"+key)
	if err != nil {
		posBuf.Destroy()
		return nil, err
	}
	normalBuf, err := r.uploadVertexBytes(normals, "bundle.object.normals:"+key)
	if err != nil {
		posBuf.Destroy()
		colorBuf.Destroy()
		return nil, err
	}
	uvBuf, err := r.uploadVertexBytes(uvs, "bundle.object.uvs:"+key)
	if err != nil {
		posBuf.Destroy()
		colorBuf.Destroy()
		normalBuf.Destroy()
		return nil, err
	}
	instanceBuf, err := r.uploadVertexBytes(instance, "bundle.object.instance:"+key)
	if err != nil {
		posBuf.Destroy()
		colorBuf.Destroy()
		normalBuf.Destroy()
		uvBuf.Destroy()
		return nil, err
	}
	res := &objectMeshResources{
		positions:   posBuf,
		colors:      colorBuf,
		normals:     normalBuf,
		uvs:         uvBuf,
		instance:    instanceBuf,
		positionLen: len(positions),
		colorLen:    len(colors),
		normalLen:   len(normals),
		uvLen:       len(uvs),
		instanceLen: len(instance),
		vertexCount: object.VertexCount,
	}
	r.objectMeshCache[key] = res
	return res, nil
}

func objectPositionBytes(b engine.RenderBundle, object engine.RenderObject) []byte {
	start := object.VertexOffset * 3
	end := start + object.VertexCount*3
	if start < 0 || end > len(b.WorldPositions) || start > end {
		return nil
	}
	return float64sToFloat32Bytes(b.WorldPositions[start:end])
}

func objectColorBytes(b engine.RenderBundle, object engine.RenderObject) []byte {
	out := make([]float32, object.VertexCount*3)
	if objectComponentRangeOK(b.WorldColors, object, 4) {
		start := object.VertexOffset * 4
		for i := 0; i < object.VertexCount; i++ {
			src := start + i*4
			dst := i * 3
			out[dst+0] = float32(b.WorldColors[src+0])
			out[dst+1] = float32(b.WorldColors[src+1])
			out[dst+2] = float32(b.WorldColors[src+2])
		}
		return float32sToBytes(out)
	}
	if objectComponentRangeOK(b.WorldColors, object, 3) {
		start := object.VertexOffset * 3
		for i := 0; i < object.VertexCount; i++ {
			src := start + i*3
			dst := i * 3
			out[dst+0] = float32(b.WorldColors[src+0])
			out[dst+1] = float32(b.WorldColors[src+1])
			out[dst+2] = float32(b.WorldColors[src+2])
		}
		return float32sToBytes(out)
	}
	color := [3]float32{0.8, 0.8, 0.8}
	if object.MaterialIndex >= 0 && object.MaterialIndex < len(b.Materials) {
		color = parseCSSColor(b.Materials[object.MaterialIndex].Color, color)
	}
	for i := 0; i < object.VertexCount; i++ {
		dst := i * 3
		out[dst+0] = color[0]
		out[dst+1] = color[1]
		out[dst+2] = color[2]
	}
	return float32sToBytes(out)
}

func objectNormalBytes(b engine.RenderBundle, object engine.RenderObject) []byte {
	if objectComponentRangeOK(b.WorldNormals, object, 3) {
		start := object.VertexOffset * 3
		end := start + object.VertexCount*3
		return float64sToFloat32Bytes(b.WorldNormals[start:end])
	}
	return flatObjectNormalBytes(b, object)
}

func flatObjectNormalBytes(b engine.RenderBundle, object engine.RenderObject) []byte {
	start := object.VertexOffset * 3
	end := start + object.VertexCount*3
	if start < 0 || end > len(b.WorldPositions) || start > end {
		return nil
	}
	out := make([]float32, object.VertexCount*3)
	for tri := 0; tri+2 < object.VertexCount; tri += 3 {
		p0 := objectPositionAt(b, object, tri)
		p1 := objectPositionAt(b, object, tri+1)
		p2 := objectPositionAt(b, object, tri+2)
		n := triangleNormal(p0, p1, p2)
		for j := 0; j < 3; j++ {
			dst := (tri + j) * 3
			out[dst+0] = n[0]
			out[dst+1] = n[1]
			out[dst+2] = n[2]
		}
	}
	return float32sToBytes(out)
}

func objectPositionAt(b engine.RenderBundle, object engine.RenderObject, vertex int) [3]float32 {
	idx := (object.VertexOffset + vertex) * 3
	if idx < 0 || idx+2 >= len(b.WorldPositions) {
		return [3]float32{}
	}
	return [3]float32{
		float32(b.WorldPositions[idx+0]),
		float32(b.WorldPositions[idx+1]),
		float32(b.WorldPositions[idx+2]),
	}
}

func objectUVBytes(b engine.RenderBundle, object engine.RenderObject) []byte {
	if objectComponentRangeOK(b.WorldUVs, object, 2) {
		start := object.VertexOffset * 2
		end := start + object.VertexCount*2
		return float64sToFloat32Bytes(b.WorldUVs[start:end])
	}
	return make([]byte, object.VertexCount*2*4)
}

func objectUVAt(b engine.RenderBundle, object engine.RenderObject, vertex int) [2]float32 {
	idx := (object.VertexOffset + vertex) * 2
	if idx < 0 || idx+1 >= len(b.WorldUVs) {
		return [2]float32{}
	}
	return [2]float32{float32(b.WorldUVs[idx]), float32(b.WorldUVs[idx+1])}
}

func raycastWorldObject(ray pickRay, b engine.RenderBundle, object engine.RenderObject) (primitiveHit, bool) {
	var best primitiveHit
	best.depth = float32(math.Inf(1))
	found := false
	for tri := 0; tri+2 < object.VertexCount; tri += 3 {
		p0 := objectPositionAt(b, object, tri)
		p1 := objectPositionAt(b, object, tri+1)
		p2 := objectPositionAt(b, object, tri+2)
		dist, u, v, ok := rayIntersectsTriangle(ray.origin, ray.dir, p0, p1, p2)
		if !ok || dist >= best.depth {
			continue
		}
		w := 1 - u - v
		best = primitiveHit{
			triangleIndex: tri / 3,
			localPosition: barycentric3(p0, p1, p2, w, u, v),
			worldPosition: [3]float32{
				ray.origin[0] + ray.dir[0]*dist,
				ray.origin[1] + ray.dir[1]*dist,
				ray.origin[2] + ray.dir[2]*dist,
			},
			uv:    barycentric2(objectUVAt(b, object, tri), objectUVAt(b, object, tri+1), objectUVAt(b, object, tri+2), w, u, v),
			depth: dist,
		}
		found = true
	}
	return best, found
}

func objectInstanceBytes(pickBase uint32) []byte {
	return instanceRecordBytes([]float64{
		1, 0, 0, 0,
		0, 1, 0, 0,
		0, 0, 1, 0,
		0, 0, 0, 1,
	}, 1, pickBase)
}

func destroyObjectMeshResources(res *objectMeshResources) {
	if res == nil {
		return
	}
	if res.positions != nil {
		res.positions.Destroy()
	}
	if res.colors != nil {
		res.colors.Destroy()
	}
	if res.normals != nil {
		res.normals.Destroy()
	}
	if res.uvs != nil {
		res.uvs.Destroy()
	}
	if res.instance != nil {
		res.instance.Destroy()
	}
}
