package assetpipe

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe/gltfedit"
	"m31labs.dev/gosx/assetpipe/meshoptim"
)

// worldTriangle holds one triangle after the node transforms are applied.
type worldTriangle struct {
	corners [3][3]float64
}

// flattenGLB walks the node graph of a GLB and returns every triangle in world
// space. It reads a quantized accessor the same way a glTF loader does: the
// stored integers come out of the accessor and the node transform carries the
// scale and the offset.
func flattenGLB(t *testing.T, data []byte) ([]worldTriangle, [3]float64, [3]float64) {
	t.Helper()
	document, err := gltfedit.Parse(data, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	var out []worldTriangle

	var walk func(node int, parent [16]float64)
	walk = func(index int, parent [16]float64) {
		if index < 0 || index >= len(document.Nodes) {
			return
		}
		node := document.Nodes[index]
		world := matrixMultiply(parent, nodeMatrix(node))
		if node.Mesh != nil && *node.Mesh < len(document.Meshes) {
			for _, primitive := range document.Meshes[*node.Mesh].Primitives {
				accessor, ok := primitive.Attributes["POSITION"]
				if !ok {
					continue
				}
				values, components, err := document.ReadAccessor(accessor)
				if err != nil || components != 3 {
					t.Fatalf("read POSITION: %v", err)
				}
				var indices []uint32
				if primitive.Indices != nil {
					indices, err = document.ReadIndices(*primitive.Indices)
					if err != nil {
						t.Fatalf("read indices: %v", err)
					}
				} else {
					indices = make([]uint32, len(values)/3)
					for i := range indices {
						indices[i] = uint32(i)
					}
				}
				points := make([][3]float64, len(values)/3)
				for i := range points {
					points[i] = transformPoint(world, values[i*3], values[i*3+1], values[i*3+2])
					for axis := 0; axis < 3; axis++ {
						low[axis] = math.Min(low[axis], points[i][axis])
						high[axis] = math.Max(high[axis], points[i][axis])
					}
				}
				for i := 0; i+2 < len(indices); i += 3 {
					out = append(out, worldTriangle{corners: [3][3]float64{
						points[indices[i]], points[indices[i+1]], points[indices[i+2]],
					}})
				}
			}
		}
		for _, child := range node.Children {
			walk(child, world)
		}
	}

	identity := [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
	for _, scene := range document.Scenes {
		for _, root := range scene.Nodes {
			walk(root, identity)
		}
	}
	return out, low, high
}

func nodeMatrix(node gltfedit.Node) [16]float64 {
	if len(node.Matrix) == 16 {
		var out [16]float64
		copy(out[:], node.Matrix)
		return out
	}
	translation := [3]float64{0, 0, 0}
	if len(node.Translation) == 3 {
		copy(translation[:], node.Translation)
	}
	rotation := [4]float64{0, 0, 0, 1}
	if len(node.Rotation) == 4 {
		copy(rotation[:], node.Rotation)
	}
	scale := [3]float64{1, 1, 1}
	if len(node.Scale) == 3 {
		copy(scale[:], node.Scale)
	}
	x, y, z, w := rotation[0], rotation[1], rotation[2], rotation[3]
	rotationMatrix := [9]float64{
		1 - 2*(y*y+z*z), 2 * (x*y + z*w), 2 * (x*z - y*w),
		2 * (x*y - z*w), 1 - 2*(x*x+z*z), 2 * (y*z + x*w),
		2 * (x*z + y*w), 2 * (y*z - x*w), 1 - 2*(x*x+y*y),
	}
	var out [16]float64
	for column := 0; column < 3; column++ {
		for row := 0; row < 3; row++ {
			out[column*4+row] = rotationMatrix[column*3+row] * scale[column]
		}
	}
	out[12], out[13], out[14] = translation[0], translation[1], translation[2]
	out[15] = 1
	return out
}

func matrixMultiply(a, b [16]float64) [16]float64 {
	var out [16]float64
	for column := 0; column < 4; column++ {
		for row := 0; row < 4; row++ {
			sum := 0.0
			for k := 0; k < 4; k++ {
				sum += a[k*4+row] * b[column*4+k]
			}
			out[column*4+row] = sum
		}
	}
	return out
}

func transformPoint(m [16]float64, x, y, z float64) [3]float64 {
	return [3]float64{
		m[0]*x + m[4]*y + m[8]*z + m[12],
		m[1]*x + m[5]*y + m[9]*z + m[13],
		m[2]*x + m[6]*y + m[10]*z + m[14],
	}
}

func triangleArea(t worldTriangle) float64 {
	ax := t.corners[1][0] - t.corners[0][0]
	ay := t.corners[1][1] - t.corners[0][1]
	az := t.corners[1][2] - t.corners[0][2]
	bx := t.corners[2][0] - t.corners[0][0]
	by := t.corners[2][1] - t.corners[0][1]
	bz := t.corners[2][2] - t.corners[0][2]
	cx := ay*bz - az*by
	cy := az*bx - ax*bz
	cz := ax*by - ay*bx
	return 0.5 * math.Sqrt(cx*cx+cy*cy+cz*cz)
}

func totalArea(triangles []worldTriangle) float64 {
	total := 0.0
	for _, triangle := range triangles {
		total += triangleArea(triangle)
	}
	return total
}

// nearestVertexDistance returns the largest distance from any corner of probe to
// the closest corner of reference. A spatial hash keeps the search linear.
func nearestVertexDistance(reference, probe []worldTriangle, cell float64) float64 {
	if cell <= 0 {
		cell = 1e-3
	}
	buckets := map[[3]int][][3]float64{}
	key := func(point [3]float64) [3]int {
		return [3]int{
			int(math.Floor(point[0] / cell)),
			int(math.Floor(point[1] / cell)),
			int(math.Floor(point[2] / cell)),
		}
	}
	for _, triangle := range reference {
		for _, corner := range triangle.corners {
			at := key(corner)
			buckets[at] = append(buckets[at], corner)
		}
	}
	worst := 0.0
	for _, triangle := range probe {
		for _, corner := range triangle.corners {
			at := key(corner)
			best := math.Inf(1)
			for dx := -1; dx <= 1; dx++ {
				for dy := -1; dy <= 1; dy++ {
					for dz := -1; dz <= 1; dz++ {
						for _, candidate := range buckets[[3]int{at[0] + dx, at[1] + dy, at[2] + dz}] {
							distance := math.Sqrt(
								(candidate[0]-corner[0])*(candidate[0]-corner[0]) +
									(candidate[1]-corner[1])*(candidate[1]-corner[1]) +
									(candidate[2]-corner[2])*(candidate[2]-corner[2]))
							best = math.Min(best, distance)
						}
					}
				}
			}
			if math.IsInf(best, 1) {
				return math.Inf(1)
			}
			worst = math.Max(worst, best)
		}
	}
	return worst
}

func runOptimizeOn(t *testing.T, source []byte, opts OptimizeOptions) (ActionResult, []byte, string) {
	t.Helper()
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "models", "mesh.glb"), source)
	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	updated, execReport, err := Execute(report, ExecuteOptions{
		Root:     dir,
		Only:     []string{"optimize-mesh"},
		Optimize: opts,
	})
	if err != nil {
		t.Fatal(err)
	}
	var result ActionResult
	for _, item := range execReport.Results {
		if item.Action == "optimize-mesh" {
			result = item
		}
	}
	if result.Action == "" {
		t.Fatalf("the stage did not run: %+v", execReport.Results)
	}
	if result.Status == StatusExecuted {
		// The plan and execute seam: only Execute may mark a variant built, and
		// the recorded size must match the file on disk.
		for _, asset := range updated.Assets {
			for _, variant := range asset.Variants {
				if variant.SourceAction != "optimize-mesh" {
					continue
				}
				if variant.State != VariantBuilt {
					t.Fatalf("variant %s state %q, want %q", variant.URI, variant.State, VariantBuilt)
				}
				info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(variant.URI)))
				if err != nil {
					t.Fatalf("stat %s: %v", variant.URI, err)
				}
				if info.Size() != variant.Bytes {
					t.Fatalf("variant %s records %d bytes, file holds %d", variant.URI, variant.Bytes, info.Size())
				}
			}
		}
	}
	path := filepath.Join(dir, "models", "mesh.opt.glb")
	optimized, err := os.ReadFile(path)
	if err != nil {
		return result, nil, dir
	}
	return result, optimized, dir
}

func TestOptimizeMeshQuantizesAndKeepsTheGeometry(t *testing.T) {
	source := buildGridGLB(t, 32)
	result, optimized, _ := runOptimizeOn(t, source, OptimizeOptions{Measure: true})
	if result.Status != StatusExecuted {
		t.Fatalf("status %q, reason %q", result.Status, result.Reason)
	}
	t.Logf("grid 32: %d -> %d bytes (%.3f)", len(source), len(optimized),
		float64(len(optimized))/float64(len(source)))
	t.Logf("metrics: %v", result.Metrics)

	if len(optimized) >= len(source) {
		t.Fatalf("the optimized file grew: %d -> %d", len(source), len(optimized))
	}

	// The stage must declare the extension it used.
	document, err := gltfedit.Parse(optimized, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, extension := range document.ExtensionsUsed {
		if extension == "KHR_mesh_quantization" {
			found = true
		}
	}
	if !found {
		t.Fatalf("extensionsUsed must name KHR_mesh_quantization, got %v", document.ExtensionsUsed)
	}

	// The component types must be the narrow ones.
	primitive := document.Meshes[0].Primitives[0]
	position := document.AccessorInfo(primitive.Attributes["POSITION"])
	if position.ComponentType != gltfedit.ComponentUnsignedShort || position.Normalized {
		t.Fatalf("POSITION componentType %d normalized %v, want 5123 and false",
			position.ComponentType, position.Normalized)
	}
	normal := document.AccessorInfo(primitive.Attributes["NORMAL"])
	if normal.ComponentType != gltfedit.ComponentByte || !normal.Normalized {
		t.Fatalf("NORMAL componentType %d normalized %v, want 5120 and true",
			normal.ComponentType, normal.Normalized)
	}
	uv := document.AccessorInfo(primitive.Attributes["TEXCOORD_0"])
	if uv.ComponentType != gltfedit.ComponentUnsignedShort || !uv.Normalized {
		t.Fatalf("TEXCOORD_0 componentType %d normalized %v, want 5123 and true",
			uv.ComponentType, uv.Normalized)
	}

	// The world-space geometry must survive. The grid is one unit across, so
	// the 16-bit lattice step is about 1.5e-5 units.
	sourceTriangles, sourceLow, sourceHigh := flattenGLB(t, source)
	optimizedTriangles, optimizedLow, optimizedHigh := flattenGLB(t, optimized)
	if len(optimizedTriangles) != len(sourceTriangles) {
		t.Fatalf("triangle count changed: %d, want %d", len(optimizedTriangles), len(sourceTriangles))
	}
	bound := 1e-4
	for axis := 0; axis < 3; axis++ {
		if math.Abs(sourceLow[axis]-optimizedLow[axis]) > bound {
			t.Fatalf("axis %d low moved from %g to %g", axis, sourceLow[axis], optimizedLow[axis])
		}
		if math.Abs(sourceHigh[axis]-optimizedHigh[axis]) > bound {
			t.Fatalf("axis %d high moved from %g to %g", axis, sourceHigh[axis], optimizedHigh[axis])
		}
	}
	sourceArea := totalArea(sourceTriangles)
	optimizedArea := totalArea(optimizedTriangles)
	t.Logf("surface area %g -> %g", sourceArea, optimizedArea)
	if math.Abs(optimizedArea-sourceArea) > sourceArea*1e-3 {
		t.Fatalf("surface area changed from %g to %g", sourceArea, optimizedArea)
	}
	worst := nearestVertexDistance(sourceTriangles, optimizedTriangles, 0.05)
	t.Logf("largest vertex displacement %g", worst)
	if worst > bound {
		t.Fatalf("a vertex moved %g, more than the quantization bound %g", worst, bound)
	}

	// The measured error must sit under the lattice bound the codec promises.
	maxError, _ := strconv.ParseFloat(result.Metrics["positionMaxError"], 64)
	errorBound, _ := strconv.ParseFloat(result.Metrics["positionErrorBound"], 64)
	if errorBound <= 0 || maxError > errorBound {
		t.Fatalf("position max error %g against bound %g", maxError, errorBound)
	}
	if result.Metrics["positionBoundsContainSource"] != "true" {
		t.Fatal("the decoded bounds must still contain the source geometry")
	}
}

func TestOptimizeMeshShrinksAttributeBytes(t *testing.T) {
	source := buildGridGLB(t, 24)
	result, _, _ := runOptimizeOn(t, source, OptimizeOptions{})
	before, err := strconv.Atoi(result.Metrics["attributeBytesBefore"])
	if err != nil {
		t.Fatal(err)
	}
	after, err := strconv.Atoi(result.Metrics["attributeBytesAfter"])
	if err != nil {
		t.Fatal(err)
	}
	ratio := float64(after) / float64(before)
	t.Logf("attribute bytes %d -> %d (%.3f)", before, after, ratio)
	// Positions fall from 12 to 6 bytes, normals from 12 to 3, and texture
	// coordinates from 8 to 4. That is 32 bytes down to 13.
	if ratio > 0.45 {
		t.Fatalf("attribute ratio %.3f is worse than the 13 of 32 bytes the formats promise", ratio)
	}
}

func TestOptimizeMeshLowersACMR(t *testing.T) {
	source := buildShuffledGridGLB(t, 32)
	result, optimized, _ := runOptimizeOn(t, source, OptimizeOptions{Measure: true})
	if result.Status != StatusExecuted {
		t.Fatalf("status %q reason %q", result.Status, result.Reason)
	}
	before, _ := strconv.ParseFloat(result.Metrics["acmrBefore"], 64)
	after, _ := strconv.ParseFloat(result.Metrics["acmrAfter"], 64)
	t.Logf("ACMR %.4f -> %.4f, winner %v", before, after, result.Metrics)
	if after >= before {
		t.Fatalf("ACMR did not fall: %.4f -> %.4f", before, after)
	}

	// Measure the written file directly, not only the reported number.
	document, err := gltfedit.Parse(optimized, nil)
	if err != nil {
		t.Fatal(err)
	}
	primitive := document.Meshes[0].Primitives[0]
	indices, err := document.ReadIndices(*primitive.Indices)
	if err != nil {
		t.Fatal(err)
	}
	vertices := document.AccessorInfo(primitive.Attributes["POSITION"]).Count
	written := meshoptim.ACMR(indices, vertices, meshoptim.DefaultCacheSize)
	t.Logf("ACMR of the written file %.4f", written)
	if written > 0.9 {
		t.Fatalf("the written order has ACMR %.4f", written)
	}
}

func TestOptimizeMeshWeldsATriangleSoup(t *testing.T) {
	source := buildSoupGLB(t, 16)
	result, optimized, _ := runOptimizeOn(t, source, OptimizeOptions{})
	if result.Status != StatusExecuted {
		t.Fatalf("status %q reason %q", result.Status, result.Reason)
	}
	welded, err := strconv.Atoi(result.Metrics["weldedVertices"])
	if err != nil {
		t.Fatal(err)
	}
	inputVertices, _ := strconv.Atoi(result.Metrics["inputVertices"])
	outputVertices, _ := strconv.Atoi(result.Metrics["outputVertices"])
	t.Logf("vertices %d -> %d, welded %d, bytes %d -> %d",
		inputVertices, outputVertices, welded, len(source), len(optimized))
	if welded == 0 {
		t.Fatal("a triangle soup must weld")
	}

	sourceTriangles, _, _ := flattenGLB(t, source)
	optimizedTriangles, _, _ := flattenGLB(t, optimized)
	if len(optimizedTriangles) != len(sourceTriangles) {
		t.Fatalf("welding changed the triangle count: %d, want %d",
			len(optimizedTriangles), len(sourceTriangles))
	}
	sourceArea := totalArea(sourceTriangles)
	if delta := math.Abs(totalArea(optimizedTriangles) - sourceArea); delta > sourceArea*1e-3 {
		t.Fatalf("welding changed the surface area by %g", delta)
	}
}

func TestOptimizeMeshSkipsQuantizationForASkinnedMesh(t *testing.T) {
	source := buildSkinnedGLB(t)
	result, optimized, _ := runOptimizeOn(t, source, OptimizeOptions{})
	if result.Status != StatusExecuted {
		t.Fatalf("status %q reason %q", result.Status, result.Reason)
	}
	if result.Metrics["positionsQuantized"] != "0" {
		t.Fatalf("a skinned mesh must keep float positions, metrics %v", result.Metrics)
	}
	document, err := gltfedit.Parse(optimized, nil)
	if err != nil {
		t.Fatal(err)
	}
	position := document.AccessorInfo(document.Meshes[0].Primitives[0].Attributes["POSITION"])
	if position.ComponentType != gltfedit.ComponentFloat {
		t.Fatalf("POSITION componentType %d, want float", position.ComponentType)
	}
	// The mesh must stay on the skinned node, never move to a child.
	if document.Nodes[0].Mesh == nil {
		t.Fatal("the skinned node lost its mesh")
	}
}

func TestOptimizeMeshRecordsEverySkipReason(t *testing.T) {
	source := buildPointCloudGLB(t)
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "models", "points.glb"), source)
	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, execReport, err := Execute(report, ExecuteOptions{Root: dir, Only: []string{"optimize-mesh"}})
	if err != nil {
		t.Fatal(err)
	}
	var result ActionResult
	for _, item := range execReport.Results {
		if item.Action == "optimize-mesh" {
			result = item
		}
	}
	if result.Status != StatusSkipped {
		t.Fatalf("status %q, want %q", result.Status, StatusSkipped)
	}
	if !strings.Contains(result.Reason, "triangle list") {
		t.Fatalf("the skip reason must name the cause, got %q", result.Reason)
	}
	if _, err := os.Stat(filepath.Join(dir, "models", "points.opt.glb")); !os.IsNotExist(err) {
		t.Fatal("a skipped action must write no file")
	}
}

func TestOptimizeMeshDetectsInstances(t *testing.T) {
	source := buildInstancedGLB(t, 6)
	result, _, _ := runOptimizeOn(t, source, OptimizeOptions{InstanceThreshold: 4})
	if result.Metrics["instanceGroups"] != "1" {
		t.Fatalf("instanceGroups = %q, want 1; metrics %v", result.Metrics["instanceGroups"], result.Metrics)
	}
	if result.Metrics["instanceNodes"] != "6" {
		t.Fatalf("instanceNodes = %q, want 6", result.Metrics["instanceNodes"])
	}
	if _, emitted := result.Metrics["instancesEmitted"]; emitted {
		t.Fatal("the stage must not write the extension unless the caller asks")
	}
}

func TestOptimizeMeshEmitsInstancesWithTheSameWorldTransforms(t *testing.T) {
	source := buildInstancedGLB(t, 6)
	result, optimized, _ := runOptimizeOn(t, source,
		OptimizeOptions{EmitInstancing: true, InstanceThreshold: 4})
	if result.Metrics["instancesEmitted"] != "1" {
		t.Fatalf("instancesEmitted = %q, metrics %v", result.Metrics["instancesEmitted"], result.Metrics)
	}
	if result.Metrics["instanceNodesRemoved"] != "6" {
		t.Fatalf("instanceNodesRemoved = %q, want 6", result.Metrics["instanceNodesRemoved"])
	}
	// Compare the world geometry, not the node translations. The instance
	// transforms now live in the wrapper's space, so a raw translation no longer
	// matches the source translation even when the drawing is identical.
	sourceTriangles, sourceLow, sourceHigh := flattenGLB(t, source)
	outTriangles, outLow, outHigh := flattenGLBWithInstances(t, optimized)
	if len(outTriangles) > len(sourceTriangles) {
		t.Fatalf("the stage invented triangles: %d, want at most %d", len(outTriangles), len(sourceTriangles))
	}
	for axis := 0; axis < 3; axis++ {
		if math.Abs(sourceLow[axis]-outLow[axis]) > 1e-3 || math.Abs(sourceHigh[axis]-outHigh[axis]) > 1e-3 {
			t.Fatalf("axis %d bounds moved from %g..%g to %g..%g",
				axis, sourceLow[axis], sourceHigh[axis], outLow[axis], outHigh[axis])
		}
	}
	areaBefore := totalArea(sourceTriangles)
	if delta := math.Abs(totalArea(outTriangles) - areaBefore); delta > areaBefore*1e-3 {
		t.Fatalf("surface area changed by %g", delta)
	}
}

func TestOptimizeMeshComposesInstancingWithTheQuantizationFold(t *testing.T) {
	// The hard case: instance transforms with rotation and scale, a mesh whose
	// bounding box does not sit on the origin, and quantized positions. The
	// world geometry of every instance must come out unchanged.
	source := buildRotatedInstancedGLB(t, 5)
	result, optimized, _ := runOptimizeOn(t, source,
		OptimizeOptions{EmitInstancing: true, InstanceThreshold: 4})
	if result.Status != StatusExecuted {
		t.Fatalf("status %q reason %q", result.Status, result.Reason)
	}
	if result.Metrics["positionsQuantized"] != "1" {
		t.Fatalf("the fold must still quantize an instanced mesh, metrics %v", result.Metrics)
	}
	if result.Metrics["instancesEmitted"] != "1" {
		t.Fatalf("instancesEmitted = %q", result.Metrics["instancesEmitted"])
	}

	sourceTriangles, sourceLow, sourceHigh := flattenGLB(t, source)
	outTriangles, outLow, outHigh := flattenGLBWithInstances(t, optimized)
	t.Logf("triangles %d -> %d, bounds %v %v -> %v %v",
		len(sourceTriangles), len(outTriangles), sourceLow, sourceHigh, outLow, outHigh)
	if len(outTriangles) > len(sourceTriangles) {
		t.Fatalf("the stage invented triangles: %d, want at most %d", len(outTriangles), len(sourceTriangles))
	}
	diagonal := 0.0
	for axis := 0; axis < 3; axis++ {
		delta := sourceHigh[axis] - sourceLow[axis]
		diagonal += delta * delta
	}
	diagonal = math.Sqrt(diagonal)
	for axis := 0; axis < 3; axis++ {
		if math.Abs(sourceLow[axis]-outLow[axis]) > diagonal*1e-4 {
			t.Fatalf("axis %d low moved from %g to %g", axis, sourceLow[axis], outLow[axis])
		}
		if math.Abs(sourceHigh[axis]-outHigh[axis]) > diagonal*1e-4 {
			t.Fatalf("axis %d high moved from %g to %g", axis, sourceHigh[axis], outHigh[axis])
		}
	}
	displacement := nearestVertexDistance(sourceTriangles, outTriangles, diagonal/16)
	t.Logf("largest vertex displacement %g, %g of the diagonal", displacement, displacement/diagonal)
	if displacement > diagonal*1e-4 {
		t.Fatalf("a vertex moved %g, more than one part in ten thousand of the diagonal %g", displacement, diagonal)
	}
	areaBefore := totalArea(sourceTriangles)
	areaAfter := totalArea(outTriangles)
	if math.Abs(areaAfter-areaBefore) > areaBefore*1e-3 {
		t.Fatalf("surface area changed from %g to %g", areaBefore, areaAfter)
	}
}

func TestOptimizeMeshDropsRedundantKeyframes(t *testing.T) {
	source := buildAnimatedGLB(t)
	result, optimized, _ := runOptimizeOn(t, source, OptimizeOptions{})
	dropped, _ := strconv.Atoi(result.Metrics["animationKeyframesDropped"])
	total, _ := strconv.Atoi(result.Metrics["animationKeyframes"])
	t.Logf("keyframes %d, dropped %d, bytes %d -> %d", total, dropped, len(source), len(optimized))
	if dropped == 0 {
		t.Fatalf("a straight line of keyframes must reduce, metrics %v", result.Metrics)
	}

	// The remaining keyframes must still reproduce the source motion.
	document, err := gltfedit.Parse(optimized, nil)
	if err != nil {
		t.Fatal(err)
	}
	sampler := document.Animations[0].Samplers[0]
	times, _, err := document.ReadAccessor(sampler.Input)
	if err != nil {
		t.Fatal(err)
	}
	values, components, err := document.ReadAccessor(sampler.Output)
	if err != nil {
		t.Fatal(err)
	}
	if components != 3 {
		t.Fatalf("output components = %d, want 3", components)
	}
	if len(times) < 2 {
		t.Fatalf("a reduced sampler still needs its ends, got %d keys", len(times))
	}
	// The source motion is y = 2*t on the y axis. Check the reduced curve.
	for i, at := range times {
		want := 2 * at
		if math.Abs(values[i*3+1]-want) > 1e-4 {
			t.Fatalf("key %d at time %g holds y %g, want %g", i, at, values[i*3+1], want)
		}
	}
}

func TestOptimizeMeshRespectsEverySkipFlag(t *testing.T) {
	source := buildGridGLB(t, 16)
	result, optimized, _ := runOptimizeOn(t, source, OptimizeOptions{
		SkipQuantize:    true,
		SkipWeld:        true,
		SkipVertexCache: true,
		SkipOverdraw:    true,
		SkipVertexFetch: true,
		SkipAnimation:   true,
	})
	if result.Status != StatusExecuted {
		t.Fatalf("status %q reason %q", result.Status, result.Reason)
	}
	document, err := gltfedit.Parse(optimized, nil)
	if err != nil {
		t.Fatal(err)
	}
	position := document.AccessorInfo(document.Meshes[0].Primitives[0].Attributes["POSITION"])
	if position.ComponentType != gltfedit.ComponentFloat {
		t.Fatalf("SkipQuantize left componentType %d", position.ComponentType)
	}
	if len(document.Nodes) != 1 {
		t.Fatalf("no fold means no extra node, got %d nodes", len(document.Nodes))
	}
	sourceTriangles, _, _ := flattenGLB(t, source)
	optimizedTriangles, _, _ := flattenGLB(t, optimized)
	if len(optimizedTriangles) != len(sourceTriangles) {
		t.Fatalf("triangle count changed: %d, want %d", len(optimizedTriangles), len(sourceTriangles))
	}
}

func TestPlanStaysFreeOfSideEffects(t *testing.T) {
	source := buildGridGLB(t, 8)
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "models", "mesh.glb"), source)
	before := listFiles(t, dir)
	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := listFiles(t, dir); len(got) != len(before) {
		t.Fatalf("Plan wrote files: %v, want %v", got, before)
	}
	// The plan must reserve the URI the executor writes, and it must leave the
	// action a candidate.
	var reserved bool
	for _, asset := range report.Assets {
		for _, variant := range asset.Variants {
			if variant.SourceAction == "optimize-mesh" {
				reserved = true
				if variant.State != "" {
					t.Fatalf("a planned variant must have no state, got %q", variant.State)
				}
			}
		}
		for _, action := range asset.Actions {
			if action.Name == "optimize-mesh" && action.Status != StatusCandidate {
				t.Fatalf("planned action status %q, want %q", action.Status, StatusCandidate)
			}
		}
	}
	if !reserved {
		t.Fatal("the plan must reserve the optimized variant URI")
	}
}

// -----------------------------------------------------------------------------
// Test asset builders
// -----------------------------------------------------------------------------

// glbFrom assembles a GLB from a JSON document and a binary payload.
func glbFrom(t *testing.T, doc map[string]any, payload []byte) []byte {
	t.Helper()
	jsonData, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(jsonData)%4 != 0 {
		jsonData = append(jsonData, ' ')
	}
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}
	total := 12 + 8 + len(jsonData) + 8 + len(payload)
	out := make([]byte, 0, total)
	out = append(out, 'g', 'l', 'T', 'F')
	out = appendLE32(out, 2)
	out = appendLE32(out, uint32(total))
	out = appendLE32(out, uint32(len(jsonData)))
	out = appendLE32(out, 0x4E4F534A)
	out = append(out, jsonData...)
	out = appendLE32(out, uint32(len(payload)))
	out = appendLE32(out, 0x004E4942)
	out = append(out, payload...)
	return out
}

func appendLE32(dst []byte, value uint32) []byte {
	return append(dst, byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
}

func appendFloat32s(dst []byte, values []float32) []byte {
	for _, value := range values {
		bits := math.Float32bits(value)
		dst = appendLE32(dst, bits)
	}
	return dst
}

func appendUint32s(dst []byte, values []uint32) []byte {
	for _, value := range values {
		dst = appendLE32(dst, value)
	}
	return dst
}

// buildShuffledGridGLB writes a grid whose triangle order is deliberately bad,
// so the cache pass has work to do.
func buildShuffledGridGLB(t *testing.T, n int) []byte {
	t.Helper()
	data := buildGridGLB(t, n)
	document, err := gltfedit.Parse(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	primitive := document.Meshes[0].Primitives[0]
	indices, err := document.ReadIndices(*primitive.Indices)
	if err != nil {
		t.Fatal(err)
	}
	triangles := len(indices) / 3
	shuffled := make([]uint32, 0, len(indices))
	// A large stride between consecutive triangles destroys locality without a
	// random source, which keeps the test repeatable.
	stride := 977
	for i := 0; i < triangles; i++ {
		triangle := (i * stride) % triangles
		shuffled = append(shuffled, indices[triangle*3:triangle*3+3]...)
	}
	values := make([]float64, len(shuffled))
	for i, value := range shuffled {
		values[i] = float64(value)
	}
	if err := document.SetAccessorData(*primitive.Indices, values, "SCALAR", gltfedit.ComponentUnsignedInt, false, gltfedit.TargetElementArrayBuffer); err != nil {
		t.Fatal(err)
	}
	out, err := document.WriteGLB()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// buildSoupGLB writes an unindexed grid, so every shared vertex appears once
// per triangle corner.
func buildSoupGLB(t *testing.T, n int) []byte {
	t.Helper()
	var positions, normals []float32
	for z := 0; z < n; z++ {
		for x := 0; x < n; x++ {
			x0, x1 := float32(x)/float32(n), float32(x+1)/float32(n)
			z0, z1 := float32(z)/float32(n), float32(z+1)/float32(n)
			corners := [][3]float32{
				{x0, 0, z0}, {x0, 0, z1}, {x1, 0, z0},
				{x1, 0, z0}, {x0, 0, z1}, {x1, 0, z1},
			}
			for _, corner := range corners {
				positions = append(positions, corner[0], corner[1], corner[2])
				normals = append(normals, 0, 1, 0)
			}
		}
	}
	var payload []byte
	positionOffset := len(payload)
	payload = appendFloat32s(payload, positions)
	normalOffset := len(payload)
	payload = appendFloat32s(payload, normals)

	count := len(positions) / 3
	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0, "NORMAL": 1},
				"mode":       4,
			}},
		}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": count, "type": "VEC3",
				"min": []float64{0, 0, 0}, "max": []float64{1, 0, 1}},
			{"bufferView": 1, "componentType": 5126, "count": count, "type": "VEC3"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": positionOffset, "byteLength": normalOffset - positionOffset, "target": 34962},
			{"buffer": 0, "byteOffset": normalOffset, "byteLength": len(payload) - normalOffset, "target": 34962},
		},
		"buffers": []map[string]any{{"byteLength": len(payload)}},
	}
	return glbFrom(t, doc, payload)
}

// buildSkinnedGLB writes one skinned sphere bound to a single joint. A skinned
// node ignores its own transform, so the stage must refuse the position fold.
// The sphere is large enough that the rest of the chain still pays, so the size
// guard does not hide the behaviour under test.
func buildSkinnedGLB(t *testing.T) []byte {
	t.Helper()
	positions, normals, indices := sphereArrays(12, 16, 1)
	vertices := len(positions) / 3
	joints := make([]uint32, vertices*4)
	weights := make([]float32, vertices*4)
	for i := 0; i < vertices; i++ {
		weights[i*4] = 1
	}
	inverseBind := []float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}

	var payload []byte
	positionOffset := len(payload)
	payload = appendFloat32s(payload, positions)
	normalOffset := len(payload)
	payload = appendFloat32s(payload, normals)
	jointOffset := len(payload)
	payload = appendUint32s(payload, joints)
	weightOffset := len(payload)
	payload = appendFloat32s(payload, weights)
	indexOffset := len(payload)
	payload = appendUint32s(payload, indices)
	bindOffset := len(payload)
	payload = appendFloat32s(payload, inverseBind)

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0, "NORMAL": 1, "JOINTS_0": 2, "WEIGHTS_0": 3},
				"indices":    4,
				"mode":       4,
			}},
		}},
		"nodes": []map[string]any{
			{"mesh": 0, "skin": 0},
			{"name": "joint"},
		},
		"skins":  []map[string]any{{"joints": []int{1}, "inverseBindMatrices": 5}},
		"scenes": []map[string]any{{"nodes": []int{0, 1}}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": vertices, "type": "VEC3",
				"min": []float64{-1, -1, -1}, "max": []float64{1, 1, 1}},
			{"bufferView": 1, "componentType": 5126, "count": vertices, "type": "VEC3"},
			{"bufferView": 2, "componentType": 5125, "count": vertices, "type": "VEC4"},
			{"bufferView": 3, "componentType": 5126, "count": vertices, "type": "VEC4"},
			{"bufferView": 4, "componentType": 5125, "count": len(indices), "type": "SCALAR"},
			{"bufferView": 5, "componentType": 5126, "count": 1, "type": "MAT4"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": positionOffset, "byteLength": normalOffset - positionOffset},
			{"buffer": 0, "byteOffset": normalOffset, "byteLength": jointOffset - normalOffset},
			{"buffer": 0, "byteOffset": jointOffset, "byteLength": weightOffset - jointOffset},
			{"buffer": 0, "byteOffset": weightOffset, "byteLength": indexOffset - weightOffset},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": bindOffset - indexOffset},
			{"buffer": 0, "byteOffset": bindOffset, "byteLength": len(payload) - bindOffset},
		},
		"buffers": []map[string]any{{"byteLength": len(payload)}},
	}
	return glbFrom(t, doc, payload)
}

// buildPointCloudGLB writes a primitive in POINTS mode, which the stage cannot
// reorder.
func buildPointCloudGLB(t *testing.T) []byte {
	t.Helper()
	positions := []float32{0, 0, 0, 1, 1, 1, 2, 0, 2}
	payload := appendFloat32s(nil, positions)
	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0},
				"mode":       0,
			}},
		}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3",
				"min": []float64{0, 0, 0}, "max": []float64{2, 1, 2}},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": 0, "byteLength": len(payload), "target": 34962},
		},
		"buffers": []map[string]any{{"byteLength": len(payload)}},
	}
	return glbFrom(t, doc, payload)
}

// buildInstancedGLB writes one sphere drawn by count sibling nodes at different
// positions. The sphere holds enough vertices that narrowing them pays for the
// dequantization nodes, so the stage does not refuse the whole asset on size.
func buildInstancedGLB(t *testing.T, count int) []byte {
	t.Helper()
	positions, normals, indices := sphereArrays(16, 24, 1)

	var payload []byte
	positionOffset := len(payload)
	payload = appendFloat32s(payload, positions)
	normalOffset := len(payload)
	payload = appendFloat32s(payload, normals)
	indexOffset := len(payload)
	payload = appendUint32s(payload, indices)

	nodes := []map[string]any{{"name": "root", "children": []int{}}}
	children := []int{}
	for i := 0; i < count; i++ {
		nodes = append(nodes, map[string]any{
			"mesh":        0,
			"translation": []float64{float64(i) * 2, 0, 0},
		})
		children = append(children, i+1)
	}
	nodes[0]["children"] = children

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0, "NORMAL": 1},
				"indices":    2,
				"mode":       4,
			}},
		}},
		"nodes":  nodes,
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": len(positions) / 3, "type": "VEC3",
				"min": []float64{-1, -1, -1}, "max": []float64{1, 1, 1}},
			{"bufferView": 1, "componentType": 5126, "count": len(normals) / 3, "type": "VEC3"},
			{"bufferView": 2, "componentType": 5125, "count": len(indices), "type": "SCALAR"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": positionOffset, "byteLength": normalOffset - positionOffset, "target": 34962},
			{"buffer": 0, "byteOffset": normalOffset, "byteLength": indexOffset - normalOffset, "target": 34962},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": len(payload) - indexOffset, "target": 34963},
		},
		"buffers": []map[string]any{{"byteLength": len(payload)}},
	}
	return glbFrom(t, doc, payload)
}

// sphereArrays builds one UV sphere as positions, normals and a triangle list.
func sphereArrays(rings, segments int, radius float64) ([]float32, []float32, []uint32) {
	var positions, normals []float32
	for ring := 0; ring <= rings; ring++ {
		phi := math.Pi * float64(ring) / float64(rings)
		for segment := 0; segment < segments; segment++ {
			theta := 2 * math.Pi * float64(segment) / float64(segments)
			x := math.Sin(phi) * math.Cos(theta)
			y := math.Cos(phi)
			z := math.Sin(phi) * math.Sin(theta)
			positions = append(positions, float32(radius*x), float32(radius*y), float32(radius*z))
			normals = append(normals, float32(x), float32(y), float32(z))
		}
	}
	var indices []uint32
	for ring := 0; ring < rings; ring++ {
		for segment := 0; segment < segments; segment++ {
			a := uint32(ring*segments + segment)
			b := uint32(ring*segments + (segment+1)%segments)
			c := a + uint32(segments)
			d := b + uint32(segments)
			indices = append(indices, a, c, b, b, c, d)
		}
	}
	return positions, normals, indices
}

// buildAnimatedGLB writes one mesh with a straight-line translation animation.
// Linear interpolation reproduces every interior keyframe exactly.
func buildAnimatedGLB(t *testing.T) []byte {
	t.Helper()
	positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}
	indices := []uint32{0, 1, 2}
	frames := 64
	times := make([]float32, 0, frames)
	values := make([]float32, 0, frames*3)
	for i := 0; i < frames; i++ {
		at := float32(i) / float32(frames-1)
		times = append(times, at)
		values = append(values, 0, 2*at, 0)
	}

	var payload []byte
	positionOffset := len(payload)
	payload = appendFloat32s(payload, positions)
	indexOffset := len(payload)
	payload = appendUint32s(payload, indices)
	timeOffset := len(payload)
	payload = appendFloat32s(payload, times)
	valueOffset := len(payload)
	payload = appendFloat32s(payload, values)

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0},
				"indices":    1,
				"mode":       4,
			}},
		}},
		"nodes":  []map[string]any{{"mesh": 0}},
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"animations": []map[string]any{{
			"channels": []map[string]any{{
				"sampler": 0,
				"target":  map[string]any{"node": 0, "path": "translation"},
			}},
			"samplers": []map[string]any{{"input": 2, "output": 3, "interpolation": "LINEAR"}},
		}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": 3, "type": "VEC3",
				"min": []float64{0, 0, 0}, "max": []float64{1, 1, 0}},
			{"bufferView": 1, "componentType": 5125, "count": 3, "type": "SCALAR"},
			{"bufferView": 2, "componentType": 5126, "count": frames, "type": "SCALAR",
				"min": []float64{0}, "max": []float64{1}},
			{"bufferView": 3, "componentType": 5126, "count": frames, "type": "VEC3"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": positionOffset, "byteLength": indexOffset - positionOffset},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": timeOffset - indexOffset},
			{"buffer": 0, "byteOffset": timeOffset, "byteLength": valueOffset - timeOffset},
			{"buffer": 0, "byteOffset": valueOffset, "byteLength": len(payload) - valueOffset},
		},
		"buffers": []map[string]any{{"byteLength": len(payload)}},
	}
	return glbFrom(t, doc, payload)
}

func instanceAttributesOf(t *testing.T, node gltfedit.Node) map[string]int {
	t.Helper()
	if len(node.Extensions) == 0 {
		return nil
	}
	var decoded map[string]struct {
		Attributes map[string]int `json:"attributes"`
	}
	if err := json.Unmarshal(node.Extensions, &decoded); err != nil {
		t.Fatalf("decode node extensions: %v", err)
	}
	return decoded["EXT_mesh_gpu_instancing"].Attributes
}

// buildRotatedInstancedGLB writes a sphere shifted off the origin, drawn by
// count sibling nodes with a translation, a rotation and a scale each. It
// exercises the composition of EXT_mesh_gpu_instancing with the quantization
// fold, where the grid offset has to travel through the instance rotation.
func buildRotatedInstancedGLB(t *testing.T, count int) []byte {
	t.Helper()
	positions, normals, indices := sphereArrays(16, 24, 1)
	// Move the mesh away from the origin, so the grid offset is not zero.
	for i := 0; i < len(positions); i += 3 {
		positions[i] += 5
		positions[i+1] += 2
		positions[i+2] -= 3
	}

	var payload []byte
	positionOffset := len(payload)
	payload = appendFloat32s(payload, positions)
	normalOffset := len(payload)
	payload = appendFloat32s(payload, normals)
	indexOffset := len(payload)
	payload = appendUint32s(payload, indices)

	nodes := []map[string]any{{"name": "root"}}
	children := []int{}
	for i := 0; i < count; i++ {
		angle := math.Pi * float64(i) / float64(count)
		// A rotation about the y axis, so the quaternion is not the identity.
		nodes = append(nodes, map[string]any{
			"mesh":        0,
			"translation": []float64{float64(i) * 3, float64(i), -float64(i) * 2},
			"rotation":    []float64{0, math.Sin(angle / 2), 0, math.Cos(angle / 2)},
			"scale":       []float64{1 + 0.25*float64(i), 1 + 0.25*float64(i), 1 + 0.25*float64(i)},
		})
		children = append(children, i+1)
	}
	nodes[0]["children"] = children

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0, "NORMAL": 1},
				"indices":    2,
				"mode":       4,
			}},
		}},
		"nodes":  nodes,
		"scenes": []map[string]any{{"nodes": []int{0}}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": len(positions) / 3, "type": "VEC3",
				"min": []float64{4, 1, -4}, "max": []float64{6, 3, -2}},
			{"bufferView": 1, "componentType": 5126, "count": len(normals) / 3, "type": "VEC3"},
			{"bufferView": 2, "componentType": 5125, "count": len(indices), "type": "SCALAR"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": positionOffset, "byteLength": normalOffset - positionOffset, "target": 34962},
			{"buffer": 0, "byteOffset": normalOffset, "byteLength": indexOffset - normalOffset, "target": 34962},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": len(payload) - indexOffset, "target": 34963},
		},
		"buffers": []map[string]any{{"byteLength": len(payload)}},
	}
	return glbFrom(t, doc, payload)
}

// flattenGLBWithInstances is flattenGLB with EXT_mesh_gpu_instancing expanded,
// which is what the runtime loader does.
func flattenGLBWithInstances(t *testing.T, data []byte) ([]worldTriangle, [3]float64, [3]float64) {
	t.Helper()
	document, err := gltfedit.Parse(data, nil)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	low := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	high := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	var out []worldTriangle

	drawMesh := func(mesh int, world [16]float64) {
		for _, primitive := range document.Meshes[mesh].Primitives {
			accessor, ok := primitive.Attributes["POSITION"]
			if !ok {
				continue
			}
			values, components, err := document.ReadAccessor(accessor)
			if err != nil || components != 3 {
				t.Fatalf("read POSITION: %v", err)
			}
			var indices []uint32
			if primitive.Indices != nil {
				indices, err = document.ReadIndices(*primitive.Indices)
				if err != nil {
					t.Fatalf("read indices: %v", err)
				}
			} else {
				indices = make([]uint32, len(values)/3)
				for i := range indices {
					indices[i] = uint32(i)
				}
			}
			points := make([][3]float64, len(values)/3)
			for i := range points {
				points[i] = transformPoint(world, values[i*3], values[i*3+1], values[i*3+2])
				for axis := 0; axis < 3; axis++ {
					low[axis] = math.Min(low[axis], points[i][axis])
					high[axis] = math.Max(high[axis], points[i][axis])
				}
			}
			for i := 0; i+2 < len(indices); i += 3 {
				out = append(out, worldTriangle{corners: [3][3]float64{
					points[indices[i]], points[indices[i+1]], points[indices[i+2]],
				}})
			}
		}
	}

	var walk func(int, [16]float64)
	walk = func(index int, parent [16]float64) {
		node := document.Nodes[index]
		world := matrixMultiply(parent, nodeMatrix(node))
		if node.Mesh != nil {
			attributes := instanceAttributesOf(t, node)
			if len(attributes) > 0 {
				count := 0
				readAttribute := func(name string, components int) []float64 {
					accessor, ok := attributes[name]
					if !ok {
						return nil
					}
					values, got, err := document.ReadAccessor(accessor)
					if err != nil || got != components {
						t.Fatalf("read instance %s: %v", name, err)
					}
					if length := len(values) / components; length > count {
						count = length
					}
					return values
				}
				translations := readAttribute("TRANSLATION", 3)
				rotations := readAttribute("ROTATION", 4)
				scales := readAttribute("SCALE", 3)
				for i := 0; i < count; i++ {
					local := gltfedit.Node{}
					if len(translations) >= (i+1)*3 {
						local.Translation = translations[i*3 : i*3+3]
					}
					if len(rotations) >= (i+1)*4 {
						local.Rotation = rotations[i*4 : i*4+4]
					}
					if len(scales) >= (i+1)*3 {
						local.Scale = scales[i*3 : i*3+3]
					}
					drawMesh(*node.Mesh, matrixMultiply(world, nodeMatrix(local)))
				}
			} else {
				drawMesh(*node.Mesh, world)
			}
		}
		for _, child := range node.Children {
			walk(child, world)
		}
	}
	for _, scene := range document.Scenes {
		for _, root := range scene.Nodes {
			walk(root, [16]float64{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1})
		}
	}
	return out, low, high
}
