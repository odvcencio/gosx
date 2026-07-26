package assetpipe

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/assetpipe/gltfedit"
	"m31labs.dev/gosx/assetpipe/meshsimplify"
)

// LODOptions controls the mesh simplification stage.
type LODOptions struct {
	// Ratios lists the fraction of triangles each generated level keeps.
	// Zero length selects DefaultLODRatios. The source asset stays the full
	// detail level, so the first ratio produces the first reduction.
	Ratios []float64
	// MaxErrorFraction stops a level early when the next collapse would move
	// the surface further than this fraction of the bounding box diagonal.
	MaxErrorFraction float64
	// MinTriangles keeps a level from falling below this triangle count.
	MinTriangles int
	// MeasureError enables the point-to-surface error measurement. It costs
	// extra time on large meshes.
	MeasureError bool
}

// DefaultLODRatios matches the two levels the planner reserves, `.lod0` at
// high quality and `.lod1` at medium quality.
var DefaultLODRatios = []float64{0.5, 0.25}

// lodQualities names the quality label of each generated level.
var lodQualities = []string{"high", "medium", "low", "lowest"}

// runBuildLODStack simplifies every triangle primitive of a model and writes
// one GLB per requested ratio.
func runBuildLODStack(ctx *executeContext, asset Asset) (actionOutcome, error) {
	opts := ctx.opts.LOD
	ratios := opts.Ratios
	if len(ratios) == 0 {
		ratios = DefaultLODRatios
	}

	data, err := ctx.readSource(asset.Path)
	if err != nil {
		return actionOutcome{}, err
	}
	sourceDir := filepath.Dir(filepath.Join(ctx.root, filepath.FromSlash(asset.Path)))
	resolve := func(uri string) ([]byte, error) {
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
		return os.ReadFile(path)
	}

	var outputs []Variant
	levels := make([]lodLevel, 0, len(ratios)+1)
	var totalSourceTriangles, skipped int
	metrics := map[string]string{}

	// Every level derives from the source document, not from the level above
	// it. Cascading collapses would compound the error of each step.
	for level, ratio := range ratios {
		document, err := gltfedit.Parse(data, resolve)
		if err != nil {
			return actionOutcome{}, fmt.Errorf("parse %s: %w", asset.Path, err)
		}
		summary, err := simplifyDocument(document, ratio, opts)
		if err != nil {
			return actionOutcome{}, err
		}
		if summary.simplified == 0 {
			return actionOutcome{
				skipReason: fmt.Sprintf("no primitive could be simplified: %s", strings.Join(summary.reasons, "; ")),
			}, nil
		}
		totalSourceTriangles = summary.inputTriangles
		skipped = summary.skipped
		encoded, err := document.WriteGLB()
		if err != nil {
			return actionOutcome{}, err
		}
		uri := lodVariantURI(asset, level)
		size, err := ctx.writeOutput(uri, encoded)
		if err != nil {
			return actionOutcome{}, err
		}
		quality := "lowest"
		if level < len(lodQualities) {
			quality = lodQualities[level]
		}
		outputs = append(outputs, Variant{
			URI:          uri,
			Kind:         "model",
			Quality:      quality,
			SourceAction: "build-lod-stack",
			State:        VariantBuilt,
			Bytes:        size,
		})
		levels = append(levels, lodLevel{
			URI:               uri,
			Ratio:             ratio,
			Quality:           quality,
			Triangles:         summary.outputTriangles,
			Vertices:          summary.outputVertices,
			Bytes:             size,
			MaxErrorFrac:      summary.maxErrorFraction,
			RMSErrorFrac:      summary.rmsErrorFraction,
			SkippedPrimitives: summary.skipped,
		})
		metrics["level"+strconv.Itoa(level)+"Triangles"] = strconv.Itoa(summary.outputTriangles)
		metrics["level"+strconv.Itoa(level)+"Bytes"] = strconv.FormatInt(size, 10)
		if opts.MeasureError {
			metrics["level"+strconv.Itoa(level)+"MaxErrorFraction"] = strconv.FormatFloat(summary.maxErrorFraction, 'g', 4, 64)
		}
	}

	sidecar := lodSidecar{
		SchemaVersion:     1,
		Source:            asset.Path,
		SourceTriangles:   totalSourceTriangles,
		SourceBytes:       asset.Bytes,
		Levels:            levels,
		SkippedPrimitives: skipped,
	}
	sidecarBytes, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return actionOutcome{}, err
	}
	sidecarURI := siblingVariant(asset.Path, ".lod", ".json")
	sidecarSize, err := ctx.writeOutput(sidecarURI, append(sidecarBytes, '\n'))
	if err != nil {
		return actionOutcome{}, err
	}
	outputs = append(outputs, Variant{
		URI:          sidecarURI,
		Kind:         "model-lod-manifest",
		SourceAction: "build-lod-stack",
		State:        VariantBuilt,
		Bytes:        sidecarSize,
	})
	metrics["sourceTriangles"] = strconv.Itoa(totalSourceTriangles)
	metrics["skippedPrimitives"] = strconv.Itoa(skipped)
	return actionOutcome{outputs: outputs, metrics: metrics}, nil
}

// lodVariantURI keeps the URIs the planner reserved and extends the pattern
// for extra levels.
func lodVariantURI(asset Asset, level int) string {
	suffix := fmt.Sprintf(".lod%d", level)
	for _, variant := range asset.Variants {
		if variant.SourceAction == "build-lod-stack" && strings.Contains(variant.URI, suffix+".") {
			return variant.URI
		}
	}
	return siblingVariant(asset.Path, suffix, ".glb")
}

type lodLevel struct {
	URI               string  `json:"uri"`
	Ratio             float64 `json:"ratio"`
	Quality           string  `json:"quality"`
	Triangles         int     `json:"triangles"`
	Vertices          int     `json:"vertices"`
	Bytes             int64   `json:"bytes"`
	MaxErrorFrac      float64 `json:"maxErrorFraction,omitempty"`
	RMSErrorFrac      float64 `json:"rmsErrorFraction,omitempty"`
	SkippedPrimitives int     `json:"skippedPrimitives,omitempty"`
}

type lodSidecar struct {
	SchemaVersion     int        `json:"schemaVersion"`
	Source            string     `json:"source"`
	SourceTriangles   int        `json:"sourceTriangles"`
	SourceBytes       int64      `json:"sourceBytes"`
	Levels            []lodLevel `json:"levels"`
	SkippedPrimitives int        `json:"skippedPrimitives,omitempty"`
}

type simplifySummary struct {
	inputTriangles   int
	outputTriangles  int
	outputVertices   int
	simplified       int
	skipped          int
	reasons          []string
	maxErrorFraction float64
	rmsErrorFraction float64
}

// simplifyDocument reduces every eligible primitive of a document in place.
func simplifyDocument(document *gltfedit.Document, ratio float64, opts LODOptions) (simplifySummary, error) {
	summary := simplifySummary{}
	uses := document.AccessorUseCount()
	reasons := map[string]bool{}

	for meshIndex := range document.Meshes {
		for primitiveIndex := range document.Meshes[meshIndex].Primitives {
			primitive := &document.Meshes[meshIndex].Primitives[primitiveIndex]
			reason, err := simplifyPrimitive(document, primitive, uses, ratio, opts, &summary)
			if err != nil {
				return summary, err
			}
			if reason != "" {
				summary.skipped++
				reasons[reason] = true
				continue
			}
			summary.simplified++
		}
	}
	for reason := range reasons {
		summary.reasons = append(summary.reasons, reason)
	}
	sort.Strings(summary.reasons)
	return summary, nil
}

// simplifyPrimitive rewrites one primitive. It returns a reason string when
// the primitive stays unchanged.
func simplifyPrimitive(
	document *gltfedit.Document,
	primitive *gltfedit.Primitive,
	uses map[int]int,
	ratio float64,
	opts LODOptions,
	summary *simplifySummary,
) (string, error) {
	if primitive.Mode != nil && *primitive.Mode != 4 {
		return "primitive is not a triangle list", nil
	}
	if len(primitive.Extensions) > 0 {
		return "primitive carries a compression extension", nil
	}
	if len(primitive.Targets) > 0 {
		return "primitive carries morph targets", nil
	}
	positionAccessor, ok := primitive.Attributes["POSITION"]
	if !ok {
		return "primitive has no POSITION attribute", nil
	}
	positions, components, err := document.ReadAccessor(positionAccessor)
	if err != nil {
		return "position accessor is unreadable", nil
	}
	if components != 3 {
		return "POSITION is not a VEC3 attribute", nil
	}
	vertexCount := len(positions) / 3

	var indices []uint32
	if primitive.Indices != nil {
		indices, err = document.ReadIndices(*primitive.Indices)
		if err != nil {
			return "index accessor is unreadable", nil
		}
	} else {
		indices = make([]uint32, vertexCount)
		for i := range indices {
			indices[i] = uint32(i)
		}
	}
	if len(indices) < 3 {
		return "primitive holds fewer than three indices", nil
	}

	source := meshsimplify.Mesh{Positions: make([]float32, len(positions)), Indices: indices}
	for i, value := range positions {
		source.Positions[i] = float32(value)
	}
	summary.inputTriangles += source.TriangleCount()

	target := int(float64(source.TriangleCount()) * ratio)
	if opts.MinTriangles > 0 && target < opts.MinTriangles {
		target = opts.MinTriangles
	}
	if target >= source.TriangleCount() {
		return "target level keeps every triangle", nil
	}
	result := meshsimplify.Simplify(source, meshsimplify.Options{
		TargetTriangles:  target,
		MaxErrorFraction: opts.MaxErrorFraction,
	})
	if result.OutputTriangles == 0 || result.OutputTriangles >= source.TriangleCount() {
		return "simplification could not remove a triangle", nil
	}
	if opts.MeasureError {
		stats := meshsimplify.MeasureError(source, result.Mesh)
		if stats.MaxFraction > summary.maxErrorFraction {
			summary.maxErrorFraction = stats.MaxFraction
		}
		if stats.RMSFraction > summary.rmsErrorFraction {
			summary.rmsErrorFraction = stats.RMSFraction
		}
	}
	summary.outputTriangles += result.OutputTriangles
	summary.outputVertices += result.OutputVertices

	// Rewrite every attribute with the vertex blend the simplifier reported.
	names := make([]string, 0, len(primitive.Attributes))
	for name := range primitive.Attributes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		accessorIndex := primitive.Attributes[name]
		values, attributeComponents, err := document.ReadAccessor(accessorIndex)
		if err != nil {
			return "attribute accessor is unreadable", nil
		}
		if len(values)/attributeComponents != vertexCount {
			return "attribute count disagrees with POSITION", nil
		}
		accessor := document.AccessorInfo(accessorIndex)
		rebuilt := blendAttribute(values, attributeComponents, result.Sources, attributeBlendMode(name))
		if name == "POSITION" {
			// Take the simplifier's positions, which may sit between the two
			// source vertices.
			rebuilt = make([]float64, len(result.Mesh.Positions))
			for i, value := range result.Mesh.Positions {
				rebuilt[i] = float64(value)
			}
		}
		if name == "NORMAL" {
			normalizeVectors(rebuilt, attributeComponents)
		}
		bufferTarget := gltfedit.TargetArrayBuffer
		if uses[accessorIndex] > 1 {
			newIndex, err := document.AddAccessor(rebuilt, accessor.Type, accessor.ComponentType, accessor.Normalized, bufferTarget)
			if err != nil {
				return "", err
			}
			primitive.Attributes[name] = newIndex
			continue
		}
		if err := document.SetAccessorData(accessorIndex, rebuilt, accessor.Type, accessor.ComponentType, accessor.Normalized, bufferTarget); err != nil {
			return "", err
		}
	}

	indexValues := make([]float64, len(result.Mesh.Indices))
	for i, value := range result.Mesh.Indices {
		indexValues[i] = float64(value)
	}
	componentType := gltfedit.IndexComponentType(result.Mesh.VertexCount())
	if primitive.Indices != nil && uses[*primitive.Indices] == 1 {
		if err := document.SetAccessorData(*primitive.Indices, indexValues, "SCALAR", componentType, false, gltfedit.TargetElementArrayBuffer); err != nil {
			return "", err
		}
	} else {
		newIndex, err := document.AddAccessor(indexValues, "SCALAR", componentType, false, gltfedit.TargetElementArrayBuffer)
		if err != nil {
			return "", err
		}
		primitive.Indices = &newIndex
	}
	return "", nil
}

// blendMode selects how one attribute crosses a collapsed edge.
type blendMode int

const (
	// blendLinear interpolates between the two source vertices.
	blendLinear blendMode = iota
	// blendNearest copies the closer source vertex. Joint indices and their
	// weights must not mix, because two vertices can bind different joints.
	blendNearest
)

func attributeBlendMode(name string) blendMode {
	if strings.HasPrefix(name, "JOINTS_") || strings.HasPrefix(name, "WEIGHTS_") {
		return blendNearest
	}
	return blendLinear
}

func blendAttribute(values []float64, components int, sources []meshsimplify.VertexSource, mode blendMode) []float64 {
	out := make([]float64, len(sources)*components)
	for i, source := range sources {
		a := int(source.A)
		b := int(source.B)
		t := source.T
		if mode == blendNearest {
			if t >= 0.5 {
				t = 1
			} else {
				t = 0
			}
		}
		for c := 0; c < components; c++ {
			va := values[a*components+c]
			vb := values[b*components+c]
			out[i*components+c] = va*(1-t) + vb*t
		}
	}
	return out
}

func normalizeVectors(values []float64, components int) {
	if components < 3 {
		return
	}
	for i := 0; i+components <= len(values); i += components {
		length := 0.0
		for c := 0; c < 3; c++ {
			length += values[i+c] * values[i+c]
		}
		if length <= 0 {
			values[i+1] = 1
			continue
		}
		length = 1 / math.Sqrt(length)
		for c := 0; c < 3; c++ {
			values[i+c] *= length
		}
	}
}
