package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/internal/scene3drenderersource"
)

func readBrowserSceneSource(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "client", "js", "bootstrap-src", name)
	switch name {
	case "16-scene-webgl.js":
		return scene3drenderersource.ReadBackend(t, "webgl")
	case "16a-scene-webgpu.js":
		return scene3drenderersource.ReadBackend(t, "webgpu")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read browser Scene3D source %s: %v", path, err)
	}
	if len(data) == 0 {
		t.Fatalf("browser Scene3D source %s is empty", path)
	}
	return string(data)
}

// TestBrowserWorldBakeTransformContract pins the shared fallback because all
// browser backends consume its world-space arrays. Runtime tests exercise the
// numeric result; this guard prevents a backend from silently bypassing those
// corrected positions, normals, tangents, or triangle order later.
func TestBrowserWorldBakeTransformContract(t *testing.T) {
	core := readBrowserSceneSource(t, "10-runtime-scene-core.ts")
	for _, needle := range []string{
		"const determinant = a * c00 + b * c01 + c * c02;",
		"const orientation = determinant < -1e-12 ? -1 : 1;",
		"normal, normalTransform, normal.x, normal.y, normal.z, 3, false,",
		"sceneMatrixTransformInto(tangent, modelMatrix, tangent.x, tangent.y, tangent.z, 4, false);",
		"w: tangent.w * orientation,",
		"const base1 = authoredIndices ? authoredIndices[tri + 1] : tri + 1;",
		"const base2 = authoredIndices ? authoredIndices[tri + 2] : tri + 2;",
		"const source1 = reverseWinding ? base2 : base1;",
		"const source2 = reverseWinding ? base1 : base2;",
	} {
		if !strings.Contains(core, needle) {
			t.Errorf("shared browser world-bake contract lost %q", needle)
		}
	}

	consumers := []struct {
		name    string
		file    string
		needles []string
	}{
		{
			name: "WebGL2 PBR",
			file: "16-scene-webgl.js",
			needles: []string{
				"bundle.worldMeshPositions",
				"bundle.worldMeshNormals",
				"bundle.worldMeshTangents",
			},
		},
		{
			name: "WebGPU PBR",
			file: "16a-scene-webgpu.js",
			needles: []string{
				"bundle.worldMeshPositions",
				"bundle.worldMeshNormals",
				`webGPUSceneMeshAttributeData(bundle, "worldMeshTangents"`,
			},
		},
		{
			name: "legacy WebGL",
			file: "16e-scene-webgl-legacy.ts",
			needles: []string{
				"sceneSliceFloatArray(bundle.worldMeshPositions",
				"gl.drawArrays(resources.trianglesMode",
			},
		},
		{
			name: "CPU picking",
			file: "17-scene-input.ts",
			needles: []string{
				"sceneRaycastPickGroup(ray, bundle.meshObjects, bundle.worldMeshPositions",
				"sceneRayIntersectsTriangle(ray.origin, ray.dir, v0, v1, v2)",
			},
		},
	}
	for _, consumer := range consumers {
		t.Run(consumer.name, func(t *testing.T) {
			source := readBrowserSceneSource(t, consumer.file)
			for _, needle := range consumer.needles {
				if !strings.Contains(source, needle) {
					t.Errorf("%s no longer consumes the shared corrected world bake: missing %q", consumer.name, needle)
				}
			}
		})
	}
}
