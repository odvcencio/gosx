package assetpipe

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/assetpipe/gltfedit"
	"m31labs.dev/gosx/assetpipe/meshoptim"
	"m31labs.dev/gosx/assetpipe/quantize"
)

// OptimizeOptions controls the mesh optimization stage. The zero value runs
// every pass, so a caller that wants the whole chain passes nothing.
type OptimizeOptions struct {
	// SkipQuantize leaves every attribute at its authored component type.
	SkipQuantize bool
	// SkipWeld keeps duplicate vertices.
	SkipWeld bool
	// SkipVertexCache keeps the authored triangle order.
	SkipVertexCache bool
	// SkipOverdraw keeps the cache order without a front-to-back sort.
	SkipOverdraw bool
	// SkipVertexFetch keeps the authored vertex order.
	SkipVertexFetch bool
	// SkipAnimation keeps every keyframe.
	SkipAnimation bool

	// PositionBits is the width of one quantized position component. Zero
	// selects 16 bits. Only 8 and 16 are legal.
	PositionBits int
	// NormalBits is the width of one quantized normal or tangent component.
	// Zero selects 8 bits.
	NormalBits int
	// UVBits is the width of one quantized texture coordinate component. Zero
	// selects 16 bits.
	UVBits int
	// UVTolerance accepts texture coordinates slightly outside the unit square
	// before the stage refuses to quantize them. Zero selects
	// DefaultUVTolerance.
	UVTolerance float64
	// OverdrawThreshold bounds how much cache efficiency the overdraw sort may
	// trade away. Zero selects DefaultOverdrawThreshold.
	OverdrawThreshold float64
	// AnimationTolerance is the largest change a dropped keyframe may cause,
	// as a fraction of the channel value range. Zero selects
	// DefaultAnimationTolerance.
	AnimationTolerance float64

	// EmitInstancing writes EXT_mesh_gpu_instancing for repeated meshes.
	// The stage always reports the candidates it found, but it only writes the
	// extension when a caller asks, because a loader without the extension
	// draws one instance instead of many.
	EmitInstancing bool
	// InstanceThreshold is the smallest group size that becomes an instanced
	// draw. Zero selects DefaultInstanceThreshold.
	InstanceThreshold int

	// Measure enables the error and order metrics. They cost extra time on a
	// large mesh, and the rasterized overdraw metric costs the most.
	Measure bool
	// MeasureResolution is the side length of the overdraw depth buffer. Zero
	// selects DefaultOverdrawResolution.
	MeasureResolution int
}

// Stage defaults. Each one names the reason it holds that value.
const (
	// DefaultUVTolerance accepts the rounding a float32 export leaves on a
	// texture coordinate that the author placed exactly on the unit square.
	DefaultUVTolerance = 1e-4
	// DefaultOverdrawThreshold allows the front-to-back sort to raise the
	// average cache miss ratio by five percent.
	DefaultOverdrawThreshold = 1.05
	// DefaultAnimationTolerance drops a keyframe when linear interpolation
	// reproduces it to within one part in ten thousand of the channel range.
	DefaultAnimationTolerance = 1e-4
	// DefaultInstanceThreshold needs this many copies before an instanced draw
	// pays for its own accessors. Measurement on a sphere of 408 vertices put
	// the break even near twenty copies: the extension trades about thirty
	// bytes of node JSON per copy for twelve bytes of accessor data, against a
	// fixed cost of roughly five hundred bytes for the accessors and the buffer
	// views.
	DefaultInstanceThreshold = 24
	// DefaultOverdrawResolution keeps the rasterized overdraw measurement
	// under a second for a mesh of a few hundred thousand triangles.
	DefaultOverdrawResolution = 192
	// DequantizeNodeBytes estimates the JSON one dequantization wrapper node
	// costs, including its entry in the parent child list. A wrapper holds a
	// mesh reference, a translation, a uniform scale and a name.
	DequantizeNodeBytes = 110
)

// runOptimizeMesh rewrites one model with the passes that need no runtime
// decoder, then writes a single GLB.
func runOptimizeMesh(ctx *executeContext, asset Asset) (actionOutcome, error) {
	opts := ctx.opts.Optimize
	data, err := ctx.readSource(asset.Path)
	if err != nil {
		return actionOutcome{}, err
	}
	// A .gltf document keeps its geometry in a separate file. Count those bytes
	// too, or the reported ratio would compare a whole GLB against a JSON
	// header and claim the stage made the asset larger.
	var bufferBytes int64
	resolve := gltfBufferResolver(ctx, asset.Path)
	document, err := gltfedit.Parse(data, func(uri string) ([]byte, error) {
		payload, err := resolve(uri)
		bufferBytes += int64(len(payload))
		return payload, err
	})
	if err != nil {
		return actionOutcome{}, fmt.Errorf("parse %s: %w", asset.Path, err)
	}
	sourceBytes := int64(len(data)) + bufferBytes

	summary, err := optimizeDocument(document, opts)
	if err != nil {
		return actionOutcome{}, err
	}
	if summary.changed == 0 {
		return actionOutcome{skipReason: summary.skipReason()}, nil
	}
	encoded, err := document.WriteGLB()
	if err != nil {
		return actionOutcome{}, err
	}
	if int64(len(encoded)) > sourceBytes {
		// The reordering passes help the GPU without shrinking the file, so a
		// small growth could still be worth shipping. A growth means the JSON
		// this stage added cost more than every pass saved, which is a loss on
		// both counts.
		return actionOutcome{
			skipReason: fmt.Sprintf("the optimized file would be %d bytes against a source of %d, so the stage kept the source",
				len(encoded), sourceBytes),
			metrics: summary.metrics(),
		}, nil
	}
	uri := plannedVariantURI(asset, "optimize-mesh", ".opt", ".glb")
	size, err := ctx.writeOutput(uri, encoded)
	if err != nil {
		return actionOutcome{}, err
	}

	metrics := summary.metrics()
	metrics["sourceBytes"] = strconv.FormatInt(sourceBytes, 10)
	metrics["outputBytes"] = strconv.FormatInt(size, 10)
	if bufferBytes > 0 {
		metrics["sourceBufferBytes"] = strconv.FormatInt(bufferBytes, 10)
	}
	if sourceBytes > 0 {
		metrics["ratio"] = strconv.FormatFloat(float64(size)/float64(sourceBytes), 'f', 4, 64)
	}
	for _, reason := range summary.skipReasons() {
		ctx.warn("optimize-mesh %s: %s", asset.Path, reason)
	}
	return actionOutcome{
		outputs: []Variant{{
			URI:          uri,
			Kind:         "model",
			Quality:      "optimized",
			SourceAction: "optimize-mesh",
			State:        VariantBuilt,
			Bytes:        size,
		}},
		metrics: metrics,
	}, nil
}

// gltfBufferResolver reads the external buffer files a .gltf document names. It
// refuses a URI that leaves the asset directory and it keeps the execution read
// bound.
func gltfBufferResolver(ctx *executeContext, assetPath string) func(string) ([]byte, error) {
	sourceDir := filepath.Dir(filepath.Join(ctx.root, filepath.FromSlash(assetPath)))
	return func(uri string) ([]byte, error) {
		if strings.Contains(uri, "..") {
			return nil, fmt.Errorf("buffer URI %q leaves the asset directory", uri)
		}
		path := filepath.Join(sourceDir, filepath.FromSlash(uri))
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		if info.Size() > ctx.maxBytes {
			return nil, fmt.Errorf("%w: %s is %d bytes", errSourceTooLarge, uri, info.Size())
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		ctx.sourceBytes += int64(len(payload))
		if int64(len(payload)) > ctx.peakReadBytes {
			ctx.peakReadBytes = int64(len(payload))
		}
		return payload, nil
	}
}

// optimizeSummary collects what every pass did, so the report can name a per
// technique gain and a per technique error.
type optimizeSummary struct {
	changed          int
	primitives       int
	skippedPrimitive map[string]int

	inputVertices  int
	outputVertices int
	inputTriangles int
	outputTrangles int

	weldedVertices  int
	droppedVertices int
	degenerate      int

	attributeBytesBefore int
	attributeBytesAfter  int
	indexBytesBefore     int
	indexBytesAfter      int

	// The order metrics accumulate raw cache misses and raw totals, then
	// divide once. Averaging a ratio per primitive would weight a small
	// primitive the same as a large one.
	cacheMissesBefore int
	cacheMissesAfter  int
	acmrTriangles     int
	atvrVertices      int
	overdrawBefore    float64
	overdrawAfter     float64
	overdrawMeshes    int

	positionMaxError     float64
	positionRMSError     float64
	positionBound        float64
	positionBoundsHold   bool
	positionQuantized    int
	normalMaxDegrees     float64
	normalBoundDegrees   float64
	uvMaxError           float64
	uvQuantized          int
	instanceGroups       int
	instanceNodes        int
	instanceEmitted      int
	instanceNodesRemoved int
	animationKeyframes   int
	animationDropped     int
	animationMaxError    float64
	positionSkipReasons  map[string]int
	cacheWinners         map[string]int
}

func newOptimizeSummary() *optimizeSummary {
	return &optimizeSummary{
		skippedPrimitive:    map[string]int{},
		positionSkipReasons: map[string]int{},
		cacheWinners:        map[string]int{},
		positionBoundsHold:  true,
	}
}

func (s *optimizeSummary) skipReason() string {
	if len(s.skippedPrimitive) == 0 {
		return "the document holds no primitive this stage can change"
	}
	return "no primitive changed: " + strings.Join(s.skipReasons(), "; ")
}

func (s *optimizeSummary) skipReasons() []string {
	out := make([]string, 0, len(s.skippedPrimitive)+len(s.positionSkipReasons))
	for reason, count := range s.skippedPrimitive {
		out = append(out, fmt.Sprintf("%s (%d primitives)", reason, count))
	}
	for reason, count := range s.positionSkipReasons {
		out = append(out, fmt.Sprintf("positions unquantized: %s (%d meshes)", reason, count))
	}
	sort.Strings(out)
	return out
}

func (s *optimizeSummary) metrics() map[string]string {
	out := map[string]string{
		"primitives":            strconv.Itoa(s.primitives),
		"primitivesChanged":     strconv.Itoa(s.changed),
		"inputVertices":         strconv.Itoa(s.inputVertices),
		"outputVertices":        strconv.Itoa(s.outputVertices),
		"inputTriangles":        strconv.Itoa(s.inputTriangles),
		"outputTriangles":       strconv.Itoa(s.outputTrangles),
		"attributeBytesBefore":  strconv.Itoa(s.attributeBytesBefore),
		"attributeBytesAfter":   strconv.Itoa(s.attributeBytesAfter),
		"indexBytesBefore":      strconv.Itoa(s.indexBytesBefore),
		"indexBytesAfter":       strconv.Itoa(s.indexBytesAfter),
		"weldedVertices":        strconv.Itoa(s.weldedVertices),
		"unusedVerticesDropped": strconv.Itoa(s.droppedVertices),
		"degenerateTriangles":   strconv.Itoa(s.degenerate),
		"positionsQuantized":    strconv.Itoa(s.positionQuantized),
		"uvsQuantized":          strconv.Itoa(s.uvQuantized),
		"instanceGroups":        strconv.Itoa(s.instanceGroups),
		"instanceNodes":         strconv.Itoa(s.instanceNodes),
	}
	if s.attributeBytesBefore > 0 {
		out["attributeRatio"] = strconv.FormatFloat(float64(s.attributeBytesAfter)/float64(s.attributeBytesBefore), 'f', 4, 64)
	}
	if s.positionQuantized > 0 && s.positionBound > 0 {
		out["positionMaxError"] = strconv.FormatFloat(s.positionMaxError, 'g', 4, 64)
		out["positionRMSError"] = strconv.FormatFloat(s.positionRMSError, 'g', 4, 64)
		out["positionErrorBound"] = strconv.FormatFloat(s.positionBound, 'g', 4, 64)
		out["positionBoundsContainSource"] = strconv.FormatBool(s.positionBoundsHold)
	}
	if s.normalBoundDegrees > 0 {
		out["normalMaxDegrees"] = strconv.FormatFloat(s.normalMaxDegrees, 'g', 4, 64)
		out["normalDegreesBound"] = strconv.FormatFloat(s.normalBoundDegrees, 'g', 4, 64)
	}
	if s.uvQuantized > 0 {
		out["uvMaxError"] = strconv.FormatFloat(s.uvMaxError, 'g', 4, 64)
	}
	if s.acmrTriangles > 0 {
		out["acmrBefore"] = strconv.FormatFloat(float64(s.cacheMissesBefore)/float64(s.acmrTriangles), 'f', 4, 64)
		out["acmrAfter"] = strconv.FormatFloat(float64(s.cacheMissesAfter)/float64(s.acmrTriangles), 'f', 4, 64)
	}
	if s.atvrVertices > 0 {
		out["atvrBefore"] = strconv.FormatFloat(float64(s.cacheMissesBefore)/float64(s.atvrVertices), 'f', 4, 64)
		out["atvrAfter"] = strconv.FormatFloat(float64(s.cacheMissesAfter)/float64(s.atvrVertices), 'f', 4, 64)
	}
	if s.overdrawMeshes > 0 {
		out["overdrawBefore"] = strconv.FormatFloat(s.overdrawBefore/float64(s.overdrawMeshes), 'f', 4, 64)
		out["overdrawAfter"] = strconv.FormatFloat(s.overdrawAfter/float64(s.overdrawMeshes), 'f', 4, 64)
	}
	if s.animationKeyframes > 0 {
		out["animationKeyframes"] = strconv.Itoa(s.animationKeyframes)
		out["animationKeyframesDropped"] = strconv.Itoa(s.animationDropped)
		out["animationMaxError"] = strconv.FormatFloat(s.animationMaxError, 'g', 4, 64)
	}
	if s.instanceEmitted > 0 {
		out["instancesEmitted"] = strconv.Itoa(s.instanceEmitted)
		out["instanceNodesRemoved"] = strconv.Itoa(s.instanceNodesRemoved)
	}
	for winner, count := range s.cacheWinners {
		out["cacheOrder."+winner] = strconv.Itoa(count)
	}
	return out
}

// attributeStream is one vertex attribute while the passes run. After
// quantization the values hold stored integers, so a weld compares them
// exactly.
type attributeStream struct {
	name          string
	values        []float64
	components    int
	accessorType  string
	componentType int
	normalized    bool
	accessorIndex int
	quantized     bool
}

func (a attributeStream) elementBytes() int {
	return a.components * componentBytes(a.componentType)
}

func componentBytes(componentType int) int {
	switch componentType {
	case gltfedit.ComponentByte, gltfedit.ComponentUnsignedByte:
		return 1
	case gltfedit.ComponentShort, gltfedit.ComponentUnsignedShort:
		return 2
	default:
		return 4
	}
}

// primitiveWork is one primitive the stage can rewrite.
type primitiveWork struct {
	mesh      int
	primitive *gltfedit.Primitive
	order     []string
	streams   map[string]attributeStream
	indices   []uint32
	vertices  int
	hadIndex  bool
	indexType int
}

// optimizeDocument runs every enabled pass over one document.
func optimizeDocument(document *gltfedit.Document, opts OptimizeOptions) (*optimizeSummary, error) {
	summary := newOptimizeSummary()
	// Instancing runs first. It changes which node draws a mesh, and the
	// position fold needs the final answer to that question.
	detectInstancing(document, opts, summary)
	uses := document.AccessorUseCount()
	nodesByMesh := document.NodesByMesh()

	// Collect the work first. A mesh only earns a position fold when every one
	// of its primitives is eligible, because the fold lands on the node and the
	// node draws every primitive of the mesh.
	work := map[int][]*primitiveWork{}
	blocked := map[int]bool{}
	for meshIndex := range document.Meshes {
		for primitiveIndex := range document.Meshes[meshIndex].Primitives {
			summary.primitives++
			primitive := &document.Meshes[meshIndex].Primitives[primitiveIndex]
			item, reason := readPrimitive(document, meshIndex, primitive)
			if reason != "" {
				summary.skippedPrimitive[reason]++
				blocked[meshIndex] = true
				continue
			}
			work[meshIndex] = append(work[meshIndex], item)
		}
	}

	for meshIndex := range document.Meshes {
		items := work[meshIndex]
		if len(items) == 0 {
			continue
		}
		grid, foldNodes, reason := positionFold(document, meshIndex, items, nodesByMesh, blocked[meshIndex], opts)
		if reason != "" {
			summary.positionSkipReasons[reason]++
		}
		for _, item := range items {
			if err := optimizePrimitive(document, item, grid, uses, opts, summary); err != nil {
				return summary, err
			}
			summary.changed++
		}
		if grid != nil {
			if err := applyPositionFold(document, meshIndex, *grid, foldNodes); err != nil {
				return summary, err
			}
			document.DeclareExtension("KHR_mesh_quantization")
		}
	}

	if !opts.SkipAnimation {
		reduceAnimations(document, opts, summary)
	}
	// Drop the accessors the passes orphaned, so their bytes leave the file.
	document.CompactAccessors()
	return summary, nil
}

// readPrimitive loads every attribute of one primitive. It returns a reason
// string when the stage must leave the primitive alone.
func readPrimitive(document *gltfedit.Document, meshIndex int, primitive *gltfedit.Primitive) (*primitiveWork, string) {
	if primitive.Mode != nil && *primitive.Mode != 4 {
		return nil, "primitive is not a triangle list"
	}
	if len(primitive.Extensions) > 0 {
		return nil, "primitive carries an extension this stage cannot rewrite"
	}
	if len(primitive.Targets) > 0 {
		return nil, "primitive carries morph targets"
	}
	if _, ok := primitive.Attributes["POSITION"]; !ok {
		return nil, "primitive has no POSITION attribute"
	}

	names := make([]string, 0, len(primitive.Attributes))
	for name := range primitive.Attributes {
		names = append(names, name)
	}
	sort.Strings(names)

	item := &primitiveWork{mesh: meshIndex, primitive: primitive, streams: map[string]attributeStream{}, order: names}
	for _, name := range names {
		index := primitive.Attributes[name]
		values, components, err := document.ReadAccessor(index)
		if err != nil {
			return nil, "an attribute accessor is unreadable"
		}
		info := document.AccessorInfo(index)
		item.streams[name] = attributeStream{
			name:          name,
			values:        values,
			components:    components,
			accessorType:  info.Type,
			componentType: info.ComponentType,
			normalized:    info.Normalized,
			accessorIndex: index,
		}
	}
	positions := item.streams["POSITION"]
	if positions.components != 3 {
		return nil, "POSITION is not a VEC3 attribute"
	}
	item.vertices = len(positions.values) / 3
	for _, name := range names {
		stream := item.streams[name]
		if len(stream.values)/stream.components != item.vertices {
			return nil, "an attribute count disagrees with POSITION"
		}
	}

	if primitive.Indices != nil {
		indices, err := document.ReadIndices(*primitive.Indices)
		if err != nil {
			return nil, "the index accessor is unreadable"
		}
		item.indices = indices
		item.hadIndex = true
		item.indexType = document.AccessorInfo(*primitive.Indices).ComponentType
	} else {
		item.indices = make([]uint32, item.vertices)
		for i := range item.indices {
			item.indices[i] = uint32(i)
		}
		item.indexType = gltfedit.ComponentUnsignedInt
	}
	if len(item.indices) < 3 {
		return nil, "primitive holds fewer than three indices"
	}
	for _, index := range item.indices {
		if int(index) >= item.vertices {
			return nil, "an index points past the vertex list"
		}
	}
	return item, ""
}

// positionFold decides whether a mesh may carry quantized positions, and where
// the dequantization goes. It returns the grid, the nodes that receive a
// wrapper child, and a reason when the mesh keeps float positions.
func positionFold(
	document *gltfedit.Document,
	meshIndex int,
	items []*primitiveWork,
	nodesByMesh map[int][]int,
	blocked bool,
	opts OptimizeOptions,
) (*quantize.PositionGrid, []int, string) {
	if opts.SkipQuantize {
		return nil, nil, ""
	}
	if blocked {
		return nil, nil, "the mesh holds a primitive this stage cannot rewrite, so a node fold would move it too"
	}
	nodes := nodesByMesh[meshIndex]
	if len(nodes) == 0 {
		return nil, nil, "no node draws the mesh, so there is no transform to carry the dequantization"
	}
	for _, nodeIndex := range nodes {
		node := document.Nodes[nodeIndex]
		if node.Skin != nil {
			return nil, nil, "a skinned node ignores its own transform, so the dequantization has nowhere to live"
		}
		// EXT_mesh_gpu_instancing is the one extension the fold can carry,
		// because the fold moves it onto the wrapper and rebases the instance
		// transforms. Any other extension may read the mesh transform in a way
		// this stage cannot follow.
		if len(node.Extensions) > 0 && !gltfedit.OnlyInstancingExtension(node) {
			return nil, nil, "a node extension may read the mesh transform"
		}
	}

	var positions []float64
	for _, item := range items {
		positions = append(positions, item.streams["POSITION"].values...)
	}
	bits := opts.PositionBits
	if bits == 0 {
		bits = 16
	}
	if bits != 8 && bits != 16 {
		return nil, nil, fmt.Sprintf("position width %d is not 8 or 16 bits", bits)
	}
	// Every node that draws the mesh needs its own wrapper child, and a wrapper
	// costs JSON. A mesh with few vertices and many nodes would grow.
	saved := (len(positions) / 3) * (12 - 3*componentBytes(positionComponentType(bits)))
	cost := len(nodes) * DequantizeNodeBytes
	if saved <= cost {
		return nil, nil, fmt.Sprintf(
			"the dequantization nodes would cost about %d bytes and the narrow positions would save about %d", cost, saved)
	}
	grid := quantize.FitPositionGrid(positions, bits)
	return &grid, nodes, ""
}

// applyPositionFold moves the mesh onto a new child node that carries the
// dequantization. The original node keeps its own transform, so an animation
// channel or a joint list that names it stays correct.
func applyPositionFold(document *gltfedit.Document, meshIndex int, grid quantize.PositionGrid, nodes []int) error {
	for _, nodeIndex := range nodes {
		node := document.Nodes[nodeIndex]
		wrapper := gltfedit.Node{
			Mesh:        intPointer(meshIndex),
			Translation: []float64{grid.Offset[0], grid.Offset[1], grid.Offset[2]},
			Scale:       []float64{grid.Scale, grid.Scale, grid.Scale},
			Name:        "gosx-dequantize",
			Weights:     node.Weights,
		}
		if gltfedit.OnlyInstancingExtension(node) {
			if err := moveInstancingToWrapper(document, &node, &wrapper, grid); err != nil {
				return err
			}
		}
		child := document.AddNode(wrapper)
		node.Mesh = nil
		node.Weights = nil
		node.Children = append(node.Children, child)
		if err := document.SetNode(nodeIndex, node); err != nil {
			return err
		}
	}
	return nil
}

// moveInstancingToWrapper carries EXT_mesh_gpu_instancing across the
// dequantization node and rewrites every instance transform into the wrapper's
// space.
//
// The original transform chain is L(node) * T(instance) * p, where p is a float
// position. After the fold the position is a lattice coordinate q and the
// wrapper holds D = translate(offset) * scale(s), so p = D * q. The new chain is
// L(node) * D * X(instance) * q, which needs D * X = T * D, that is
// X = inverse(D) * T * D.
//
// Writing T as translate(t) * rotate(R) * scale(k) and using the fact that s is
// one number for all three axes, the product resolves to a translation, the same
// rotation and the same scale:
//
//	X = translate((t - offset + R * (k * offset)) / s) * rotate(R) * scale(k)
//
// A per-axis scale would leave a shear here, which no translation, rotation and
// scale triple can express. That is the second reason the grid keeps one scale.
func moveInstancingToWrapper(document *gltfedit.Document, node, wrapper *gltfedit.Node, grid quantize.PositionGrid) error {
	attributes := gltfedit.InstanceAttributes(*node)
	if len(attributes) == 0 {
		return nil
	}
	count := 0
	read := func(name string, components int) ([]float64, error) {
		index, ok := attributes[name]
		if !ok {
			return nil, nil
		}
		values, got, err := document.ReadAccessor(index)
		if err != nil {
			return nil, err
		}
		if got != components {
			return nil, fmt.Errorf("instance attribute %s has %d components, want %d", name, got, components)
		}
		if length := len(values) / components; length > count {
			count = length
		}
		return values, nil
	}
	translations, err := read("TRANSLATION", 3)
	if err != nil {
		return err
	}
	rotations, err := read("ROTATION", 4)
	if err != nil {
		return err
	}
	scales, err := read("SCALE", 3)
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	rebased := make([]float64, 0, count*3)
	for i := 0; i < count; i++ {
		t := [3]float64{0, 0, 0}
		if len(translations) >= (i+1)*3 {
			t = [3]float64{translations[i*3], translations[i*3+1], translations[i*3+2]}
		}
		rotation := [4]float64{0, 0, 0, 1}
		if len(rotations) >= (i+1)*4 {
			rotation = [4]float64{rotations[i*4], rotations[i*4+1], rotations[i*4+2], rotations[i*4+3]}
		}
		k := [3]float64{1, 1, 1}
		if len(scales) >= (i+1)*3 {
			k = [3]float64{scales[i*3], scales[i*3+1], scales[i*3+2]}
		}
		scaledOffset := [3]float64{k[0] * grid.Offset[0], k[1] * grid.Offset[1], k[2] * grid.Offset[2]}
		turned := rotateByQuaternion(rotation, scaledOffset)
		for axis := 0; axis < 3; axis++ {
			rebased = append(rebased, (t[axis]-grid.Offset[axis]+turned[axis])/grid.Scale)
		}
	}

	// Write new accessors. The source accessors may be shared with another node,
	// so an in-place rewrite could change what that node draws.
	moved := map[string]int{}
	index, err := document.AddAccessor(rebased, "VEC3", gltfedit.ComponentFloat, false, gltfedit.TargetArrayBuffer)
	if err != nil {
		return err
	}
	moved["TRANSLATION"] = index
	if rotations != nil {
		moved["ROTATION"] = attributes["ROTATION"]
	}
	if scales != nil {
		moved["SCALE"] = attributes["SCALE"]
	}
	if err := gltfedit.SetInstanceAttributes(wrapper, moved); err != nil {
		return err
	}
	node.Extensions = nil
	return nil
}

// rotateByQuaternion turns one vector by a glTF rotation, which stores x, y, z
// and then w.
func rotateByQuaternion(q [4]float64, v [3]float64) [3]float64 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	// The standard form: v + 2 * cross(qv, cross(qv, v) + w * v).
	tx := 2 * (y*v[2] - z*v[1])
	ty := 2 * (z*v[0] - x*v[2])
	tz := 2 * (x*v[1] - y*v[0])
	return [3]float64{
		v[0] + w*tx + (y*tz - z*ty),
		v[1] + w*ty + (z*tx - x*tz),
		v[2] + w*tz + (x*ty - y*tx),
	}
}

// positionComponentType maps a component width to the glTF component type the
// stage stores quantized positions in.
func positionComponentType(bits int) int {
	if bits == 8 {
		return gltfedit.ComponentUnsignedByte
	}
	return gltfedit.ComponentUnsignedShort
}

func intPointer(value int) *int {
	out := value
	return &out
}

// optimizePrimitive runs the vertex and index passes on one primitive and
// writes the result back into the document.
func optimizePrimitive(
	document *gltfedit.Document,
	item *primitiveWork,
	grid *quantize.PositionGrid,
	uses map[int]int,
	opts OptimizeOptions,
	summary *optimizeSummary,
) error {
	summary.inputVertices += item.vertices
	summary.inputTriangles += len(item.indices) / 3
	for _, name := range item.order {
		stream := item.streams[name]
		summary.attributeBytesBefore += stream.elementBytes() * item.vertices
	}
	summary.indexBytesBefore += componentBytes(item.indexType) * len(item.indices)

	sourcePositions := append([]float64(nil), item.streams["POSITION"].values...)

	if !opts.SkipQuantize {
		quantizeStreams(item, grid, opts, summary)
	}

	// Weld after quantization. Quantized values sit on the lattice, so two
	// vertices that should merge hold the same numbers exactly.
	if !opts.SkipWeld {
		streams := make([]meshoptim.Stream, 0, len(item.order))
		for _, name := range item.order {
			stream := item.streams[name]
			streams = append(streams, meshoptim.Stream{Values: stream.values, Components: stream.components})
		}
		remap, unique := meshoptim.Weld(item.vertices, streams)
		if unique < item.vertices {
			summary.weldedVertices += item.vertices - unique
			item.indices = meshoptim.ApplyWeld(item.indices, remap)
			for _, name := range item.order {
				stream := item.streams[name]
				stream.values = meshoptim.CollapseWeld(stream.values, stream.components, remap, unique)
				item.streams[name] = stream
			}
			item.vertices = unique
			before := len(item.indices) / 3
			item.indices = meshoptim.DropDegenerate(item.indices)
			summary.degenerate += before - len(item.indices)/3
		}
	}

	if opts.Measure {
		summary.cacheMissesBefore += meshoptim.CacheMisses(item.indices, item.vertices, meshoptim.DefaultCacheSize)
	}
	orderBefore := append([]uint32(nil), item.indices...)

	if !opts.SkipVertexCache {
		order, winner := meshoptim.OptimizeVertexCacheBest(item.indices, item.vertices)
		item.indices = order
		summary.cacheWinners[winner]++
	}
	if !opts.SkipOverdraw {
		threshold := opts.OverdrawThreshold
		if threshold == 0 {
			threshold = DefaultOverdrawThreshold
		}
		positions := positionFloat32(item)
		item.indices = meshoptim.OptimizeOverdraw(item.indices, positions, item.vertices, threshold)
	}
	if opts.Measure {
		summary.acmrTriangles += len(item.indices) / 3
		summary.atvrVertices += item.vertices
		summary.cacheMissesAfter += meshoptim.CacheMisses(item.indices, item.vertices, meshoptim.DefaultCacheSize)
		measureOverdraw(item, orderBefore, opts, summary)
	}

	if !opts.SkipVertexFetch {
		newIndices, remap, used := meshoptim.OptimizeVertexFetch(item.indices, item.vertices)
		if used < item.vertices {
			summary.droppedVertices += item.vertices - used
		}
		item.indices = newIndices
		for _, name := range item.order {
			stream := item.streams[name]
			stream.values = meshoptim.ApplyRemapFloat64(stream.values, stream.components, remap, used)
			item.streams[name] = stream
		}
		item.vertices = used
	}

	if opts.Measure && !opts.SkipQuantize && grid != nil {
		report := grid.MeasurePositionError(sourcePositions)
		summary.positionMaxError = math.Max(summary.positionMaxError, report.Max)
		summary.positionRMSError = math.Max(summary.positionRMSError, report.RMS)
		summary.positionBound = math.Max(summary.positionBound, report.Bound)
		if !report.Contains() {
			summary.positionBoundsHold = false
		}
	}

	summary.outputVertices += item.vertices
	summary.outputTrangles += len(item.indices) / 3
	return writePrimitive(document, item, uses, summary)
}

// measureOverdraw rasterizes the mesh twice, once in the incoming order and
// once in the order this stage produced. It counts the fragments a depth test
// would let through, so it measures the result rather than the sort key.
func measureOverdraw(item *primitiveWork, before []uint32, opts OptimizeOptions, summary *optimizeSummary) {
	resolution := opts.MeasureResolution
	if resolution == 0 {
		resolution = DefaultOverdrawResolution
	}
	positions := positionFloat32(item)
	first := meshoptim.MeasureOverdraw(before, positions, item.vertices, resolution)
	second := meshoptim.MeasureOverdraw(item.indices, positions, item.vertices, resolution)
	if first.Covered == 0 || second.Covered == 0 {
		return
	}
	summary.overdrawMeshes++
	summary.overdrawBefore += first.Ratio
	summary.overdrawAfter += second.Ratio
}

// positionFloat32 returns the current positions as float32. The values may be
// lattice coordinates, which is fine: every pass that reads positions cares
// only about relative geometry, and the lattice is a uniform scale of the
// source.
func positionFloat32(item *primitiveWork) []float32 {
	stream := item.streams["POSITION"]
	out := make([]float32, len(stream.values))
	for i, value := range stream.values {
		out[i] = float32(value)
	}
	return out
}

// quantizeStreams rewrites every attribute this stage knows how to narrow.
func quantizeStreams(item *primitiveWork, grid *quantize.PositionGrid, opts OptimizeOptions, summary *optimizeSummary) {
	normalBits := opts.NormalBits
	if normalBits == 0 {
		normalBits = 8
	}
	uvBits := opts.UVBits
	if uvBits == 0 {
		uvBits = 16
	}
	uvTolerance := opts.UVTolerance
	if uvTolerance == 0 {
		uvTolerance = DefaultUVTolerance
	}

	for _, name := range item.order {
		stream := item.streams[name]
		switch {
		case name == "POSITION":
			if grid == nil {
				continue
			}
			stored := grid.EncodeStream(stream.values)
			stream.values = intsToFloats(stored)
			stream.componentType = positionComponentType(grid.Bits)
			stream.normalized = false
			stream.quantized = true
			summary.positionQuantized++
		case name == "NORMAL" || name == "TANGENT":
			if stream.componentType != gltfedit.ComponentFloat {
				continue
			}
			codec := quantize.UnitCodec{Bits: normalBits, Signed: true}
			stored, report := quantize.EncodeUnitVectors(stream.values, stream.components, codec)
			if len(stored) == 0 {
				continue
			}
			stream.values = intsToFloats(stored)
			stream.componentType = gltfedit.ComponentByte
			if normalBits == 16 {
				stream.componentType = gltfedit.ComponentShort
			}
			stream.normalized = true
			stream.quantized = true
			summary.normalMaxDegrees = math.Max(summary.normalMaxDegrees, report.MaxDegrees)
			summary.normalBoundDegrees = math.Max(summary.normalBoundDegrees, report.Bound)
		case strings.HasPrefix(name, "TEXCOORD_"):
			if stream.componentType != gltfedit.ComponentFloat {
				continue
			}
			low, high := quantize.Range(stream.values)
			if low < -uvTolerance || high > 1+uvTolerance {
				summary.skippedPrimitive["texture coordinates leave the unit square, so a normalized accessor would clamp them"]++
				continue
			}
			codec := quantize.UnitCodec{Bits: uvBits, Signed: false}
			stored, report := quantize.EncodeUnitRange(stream.values, codec, uvTolerance)
			stream.values = intsToFloats(stored)
			stream.componentType = gltfedit.ComponentUnsignedShort
			if uvBits == 8 {
				stream.componentType = gltfedit.ComponentUnsignedByte
			}
			stream.normalized = true
			stream.quantized = true
			summary.uvQuantized++
			summary.uvMaxError = math.Max(summary.uvMaxError, report.Max)
		case strings.HasPrefix(name, "COLOR_"):
			if stream.componentType != gltfedit.ComponentFloat {
				continue
			}
			codec := quantize.UnitCodec{Bits: 8, Signed: false}
			stored, _ := quantize.EncodeUnitRange(stream.values, codec, 0)
			stream.values = intsToFloats(stored)
			stream.componentType = gltfedit.ComponentUnsignedByte
			stream.normalized = true
			stream.quantized = true
		case strings.HasPrefix(name, "WEIGHTS_"):
			if stream.componentType != gltfedit.ComponentFloat {
				continue
			}
			codec := quantize.UnitCodec{Bits: 16, Signed: false}
			stored, _ := quantize.EncodeUnitRange(stream.values, codec, 0)
			stream.values = intsToFloats(stored)
			stream.componentType = gltfedit.ComponentUnsignedShort
			stream.normalized = true
			stream.quantized = true
		case strings.HasPrefix(name, "JOINTS_"):
			// Narrowing a joint index loses nothing while every index fits.
			high := 0.0
			for _, value := range stream.values {
				high = math.Max(high, value)
			}
			if high > 255 || stream.componentType == gltfedit.ComponentUnsignedByte {
				continue
			}
			stream.componentType = gltfedit.ComponentUnsignedByte
			stream.normalized = false
			stream.quantized = true
		default:
			continue
		}
		item.streams[name] = stream
	}
}

func intsToFloats(values []int32) []float64 {
	out := make([]float64, len(values))
	for i, value := range values {
		out[i] = float64(value)
	}
	return out
}

func floatsToInts(values []float64) []int32 {
	out := make([]int32, len(values))
	for i, value := range values {
		out[i] = int32(math.Round(value))
	}
	return out
}

// writePrimitive stores the finished streams. An accessor that more than one
// primitive reads becomes a new accessor, so the other reader keeps its data.
func writePrimitive(document *gltfedit.Document, item *primitiveWork, uses map[int]int, summary *optimizeSummary) error {
	for _, name := range item.order {
		stream := item.streams[name]
		summary.attributeBytesAfter += stream.elementBytes() * item.vertices
		writeInts := stream.quantized || stream.componentType != gltfedit.ComponentFloat
		if uses[stream.accessorIndex] > 1 {
			var index int
			var err error
			if writeInts {
				index, err = document.AddAccessorInts(floatsToInts(stream.values), stream.accessorType, stream.componentType, stream.normalized, gltfedit.TargetArrayBuffer)
			} else {
				index, err = document.AddAccessor(stream.values, stream.accessorType, stream.componentType, stream.normalized, gltfedit.TargetArrayBuffer)
			}
			if err != nil {
				return err
			}
			item.primitive.Attributes[name] = index
			continue
		}
		var err error
		if writeInts {
			err = document.SetAccessorInts(stream.accessorIndex, floatsToInts(stream.values), stream.accessorType, stream.componentType, stream.normalized, gltfedit.TargetArrayBuffer)
		} else {
			err = document.SetAccessorData(stream.accessorIndex, stream.values, stream.accessorType, stream.componentType, stream.normalized, gltfedit.TargetArrayBuffer)
		}
		if err != nil {
			return err
		}
	}

	componentType := gltfedit.IndexComponentType(item.vertices)
	summary.indexBytesAfter += componentBytes(componentType) * len(item.indices)
	stored := make([]int32, len(item.indices))
	for i, value := range item.indices {
		stored[i] = int32(value)
	}
	if item.hadIndex && item.primitive.Indices != nil && uses[*item.primitive.Indices] == 1 {
		return document.SetAccessorInts(*item.primitive.Indices, stored, "SCALAR", componentType, false, gltfedit.TargetElementArrayBuffer)
	}
	index, err := document.AddAccessorInts(stored, "SCALAR", componentType, false, gltfedit.TargetElementArrayBuffer)
	if err != nil {
		return err
	}
	item.primitive.Indices = &index
	return nil
}
