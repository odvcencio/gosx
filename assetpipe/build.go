package assetpipe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registered for DecodeConfig on embedded images
	_ "image/png"  // registered for DecodeConfig on embedded images
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/assetpipe/gltfedit"
	"m31labs.dev/gosx/assetpipe/manifest"
	"m31labs.dev/gosx/assetpipe/texture"
)

// DefaultLODDistanceScale multiplies a model's bounding radius into the
// switch distance of its first reduction. See lodDistance.
const DefaultLODDistanceScale = 4.0

// ManifestFileName is the name of the manifest `gosx assets build` writes.
const ManifestFileName = "assets.manifest.json"

// Budget caps what one model may ask of the renderer. A zero field is
// unlimited. Budgets apply to the full-detail level.
type Budget struct {
	MaxTriangles    int64 `json:"maxTriangles,omitempty"`
	MaxTextureBytes int64 `json:"maxTextureBytes,omitempty"`
	MaxJoints       int   `json:"maxJoints,omitempty"`
	MaxDrawCalls    int   `json:"maxDrawCalls,omitempty"`
}

// Check returns one violation per exceeded cap.
func (b Budget) Check(model string, stats manifest.Stats) []manifest.Violation {
	var out []manifest.Violation
	add := func(name string, limit, actual int64) {
		if limit > 0 && actual > limit {
			out = append(out, manifest.Violation{Model: model, Budget: name, Limit: limit, Actual: actual})
		}
	}
	add("triangles", b.MaxTriangles, int64(stats.Triangles))
	add("textureBytes", b.MaxTextureBytes, stats.TextureBytes)
	add("joints", int64(b.MaxJoints), int64(stats.Joints))
	add("drawCalls", int64(b.MaxDrawCalls), int64(stats.DrawCalls))
	return out
}

// BuildInput names one model for BuildModels.
type BuildInput struct {
	// Path is the GLB or glTF file.
	Path string
	// Name is the manifest name. Empty derives it from the path.
	Name string
	// LODRatios overrides BuildOptions.LODRatios for this model.
	LODRatios []float64
	// Budget overrides BuildOptions.Budget for this model when non-nil.
	Budget *Budget
}

// BuildOptions controls BuildModels.
type BuildOptions struct {
	// Inputs lists directories and files. A directory contributes every .glb
	// and .gltf below it.
	Inputs []string
	// Models adds explicitly named models, for example from a build manifest.
	Models []BuildInput
	// OutputDir receives the GLBs, textures, and the manifest.
	OutputDir string
	// LODRatios lists the triangle fraction each reduced level keeps. Nil
	// selects DefaultLODRatios.
	LODRatios []float64
	// NoLOD skips the LOD chain.
	NoLOD bool
	// NoTextures skips KTX2 generation.
	NoTextures bool
	// NoOptimize skips vertex welding, reordering, and quantization.
	NoOptimize bool
	// Meshopt compresses mesh vertex and index data with
	// EXT_meshopt_compression. The GLB then requires the extension, which the
	// scene loader decodes in its glTF chunk.
	Meshopt bool
	// Optimize tunes the optimization stage.
	Optimize OptimizeOptions
	// LOD tunes simplification.
	LOD LODOptions
	// Texture tunes KTX2 generation. The zero value selects
	// DefaultTextureOptions.
	Texture *TextureOptions
	// LODDistanceScale scales switch distances. Zero selects
	// DefaultLODDistanceScale.
	LODDistanceScale float64
	// Budget applies to every model without its own budget.
	Budget Budget
}

// BuildModels turns glTF and GLB exports into optimized GLBs, an LOD chain,
// KTX2 textures, and a manifest. It writes the manifest to OutputDir. A budget
// violation does not stop the build; it appears in Manifest.Violations so the
// caller can decide, and the files still exist for inspection.
func BuildModels(opts BuildOptions) (manifest.Manifest, error) {
	if strings.TrimSpace(opts.OutputDir) == "" {
		return manifest.Manifest{}, errors.New("assetpipe: build needs an output directory")
	}
	outDir, err := filepath.Abs(opts.OutputDir)
	if err != nil {
		return manifest.Manifest{}, err
	}
	inputs, err := collectBuildInputs(opts, outDir)
	if err != nil {
		return manifest.Manifest{}, err
	}
	result := manifest.Manifest{SchemaVersion: manifest.SchemaVersion, Models: []manifest.Model{}}
	seen := map[string]string{}
	for _, input := range inputs {
		if prior, dup := seen[input.Name]; dup {
			return result, fmt.Errorf("assetpipe: models %s and %s share the name %q", prior, input.Path, input.Name)
		}
		seen[input.Name] = input.Path
		budget := opts.Budget
		if input.Budget != nil {
			budget = *input.Budget
		}
		model, warnings, err := buildModel(opts, input, outDir)
		if err != nil {
			return result, fmt.Errorf("assetpipe: build %s: %w", input.Path, err)
		}
		result.Models = append(result.Models, model)
		result.Warnings = append(result.Warnings, warnings...)
		result.Violations = append(result.Violations, budget.Check(model.Name, model.Stats)...)
	}
	sort.SliceStable(result.Models, func(i, j int) bool { return result.Models[i].Name < result.Models[j].Name })
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return result, err
	}
	if err := os.WriteFile(filepath.Join(outDir, ManifestFileName), append(data, '\n'), 0o644); err != nil {
		return result, err
	}
	return result, nil
}

func collectBuildInputs(opts BuildOptions, outDir string) ([]BuildInput, error) {
	out := append([]BuildInput(nil), opts.Models...)
	for i := range out {
		abs, err := filepath.Abs(out[i].Path)
		if err != nil {
			return nil, err
		}
		out[i].Path = abs
		if out[i].Name == "" {
			out[i].Name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
		}
	}
	for _, in := range opts.Inputs {
		abs, err := filepath.Abs(in)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			out = append(out, BuildInput{Path: abs, Name: strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))})
			continue
		}
		err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p == outDir {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".glb" && ext != ".gltf" {
				return nil
			}
			rel, err := filepath.Rel(abs, p)
			if err != nil {
				return err
			}
			out = append(out, BuildInput{Path: p, Name: filepath.ToSlash(strings.TrimSuffix(rel, filepath.Ext(rel)))})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func buildModel(opts BuildOptions, input BuildInput, outDir string) (manifest.Model, []string, error) {
	var warnings []string
	source, err := os.ReadFile(input.Path)
	if err != nil {
		return manifest.Model{}, nil, err
	}
	sourceDir := filepath.Dir(input.Path)
	resolve := func(uri string) ([]byte, error) {
		if strings.Contains(uri, "..") || filepath.IsAbs(uri) {
			return nil, fmt.Errorf("URI %q leaves the asset directory", uri)
		}
		return os.ReadFile(filepath.Join(sourceDir, filepath.FromSlash(uri)))
	}
	load := func() (*gltfedit.Document, error) { return gltfedit.Parse(source, resolve) }

	var meshoptStats manifest.Meshopt
	finish := func(document *gltfedit.Document) ([]byte, error) {
		if !opts.NoOptimize {
			if _, err := optimizeDocument(document, opts.Optimize); err != nil {
				return nil, err
			}
		}
		encoded, err := document.WriteGLB()
		if err != nil || !opts.Meshopt {
			return encoded, err
		}
		compressed, stats, cerr := compressMeshopt(encoded)
		if cerr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: meshopt skipped: %v", input.Name, cerr))
			return encoded, nil
		}
		if meshoptStats.Views == 0 {
			meshoptStats = manifest.Meshopt{Views: stats.Views, Skipped: stats.Skipped, ViewBytesBefore: stats.ViewBytesBefore, ViewBytesAfter: stats.ViewBytesAfter}
		}
		return compressed, nil
	}

	document, err := load()
	if err != nil {
		return manifest.Model{}, nil, err
	}
	mainBytes, err := finish(document)
	if err != nil {
		return manifest.Model{}, nil, err
	}
	mainURI := input.Name + ".glb"
	mainStats, err := analyzeGLB(mainBytes, resolve)
	if err != nil {
		return manifest.Model{}, nil, fmt.Errorf("analyze output: %w", err)
	}
	// External image files keep their relative URI inside the GLB, so they
	// travel with it.
	for _, image := range mainStats.images {
		if image.external == "" {
			continue
		}
		data, err := resolve(image.external)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: external image %q was not copied: %v", input.Name, image.external, err))
			continue
		}
		if err := writeFileUnder(outDir, path.Join(path.Dir(input.Name), image.external), data); err != nil {
			return manifest.Model{}, nil, err
		}
	}

	// GLBs are written last: the texture stage adds KTX2 variants to each one.
	glbs := []pendingGLB{{uri: mainURI, data: mainBytes, lod: 0}}
	model := manifest.Model{
		Name:   input.Name,
		Source: filepath.ToSlash(input.Path),
		URI:    mainURI,
		Bytes:  int64(len(mainBytes)),
		Radius: mainStats.radius,
		Stats:  mainStats.stats,
		LODs: []manifest.LOD{{
			Level: 0, URI: mainURI, Triangles: mainStats.stats.Triangles, Bytes: int64(len(mainBytes)), Ratio: 1,
		}},
	}
	model.Stats.TextureBytes = mainStats.textureBytes
	if meshoptStats.Views > 0 {
		model.Meshopt = &meshoptStats
	}
	for _, note := range mainStats.notes {
		warnings = append(warnings, input.Name+": "+note)
	}

	if !opts.NoLOD {
		ratios := input.LODRatios
		if ratios == nil {
			ratios = opts.LODRatios
		}
		if ratios == nil {
			ratios = DefaultLODRatios
		}
		ratios = append([]float64(nil), ratios...)
		sort.Sort(sort.Reverse(sort.Float64Slice(ratios)))
		scale := opts.LODDistanceScale
		if scale <= 0 {
			scale = DefaultLODDistanceScale
		}
		radius := mainStats.radius
		if radius <= 0 {
			radius = 1
			warnings = append(warnings, input.Name+": model bounds are unknown, so LOD distances assume a radius of 1")
		}
		prior := mainStats.stats.Triangles
		for _, ratio := range ratios {
			if ratio <= 0 || ratio >= 1 {
				return manifest.Model{}, nil, fmt.Errorf("LOD ratio %g must be between 0 and 1", ratio)
			}
			doc, err := load()
			if err != nil {
				return manifest.Model{}, nil, err
			}
			summary, err := simplifyDocument(doc, ratio, opts.LOD)
			if err != nil {
				return manifest.Model{}, nil, err
			}
			if summary.simplified == 0 {
				warnings = append(warnings, fmt.Sprintf("%s: no LOD at ratio %g: %s", input.Name, ratio, strings.Join(summary.reasons, "; ")))
				continue
			}
			encoded, err := finish(doc)
			if err != nil {
				return manifest.Model{}, nil, err
			}
			stats, err := analyzeGLB(encoded, resolve)
			if err != nil {
				return manifest.Model{}, nil, fmt.Errorf("analyze LOD: %w", err)
			}
			if stats.stats.Triangles >= prior {
				warnings = append(warnings, fmt.Sprintf("%s: LOD at ratio %g kept %d triangles, so it was dropped", input.Name, ratio, stats.stats.Triangles))
				continue
			}
			prior = stats.stats.Triangles
			level := len(model.LODs)
			uri := fmt.Sprintf("%s.lod%d.glb", input.Name, level)
			glbs = append(glbs, pendingGLB{uri: uri, data: encoded, lod: level})
			achieved := float64(stats.stats.Triangles) / float64(math.Max(1, float64(mainStats.stats.Triangles)))
			model.LODs = append(model.LODs, manifest.LOD{
				Level:     level,
				URI:       uri,
				Triangles: stats.stats.Triangles,
				Bytes:     int64(len(encoded)),
				Ratio:     round4(achieved),
				Distance:  round4(lodDistance(scale, radius, achieved)),
			})
		}
	}

	builtVariants := map[int][]Variant{}
	if !opts.NoTextures {
		textureOpts := DefaultTextureOptions()
		if opts.Texture != nil {
			textureOpts = *opts.Texture
		}
		for index, image := range mainStats.images {
			if len(image.data) == 0 {
				continue
			}
			label := image.label(index)
			base := input.Name + ".textures/" + label
			imageOpts := textureOpts
			imageOpts.Role = string(image.role)
			result, variants, err := BuildTexture(base+".png", image.data, imageOpts)
			if err != nil {
				if errors.Is(err, texture.ErrUnsupportedSource) || errors.Is(err, texture.ErrTooManyPixels) {
					warnings = append(warnings, fmt.Sprintf("%s: image %s has no KTX2 variants: %v", input.Name, label, err))
					continue
				}
				return manifest.Model{}, nil, fmt.Errorf("image %s: %w", label, err)
			}
			entry := manifest.Texture{Image: label, Role: string(image.role), Width: image.width, Height: image.height}
			for i, plan := range result.Variants {
				if err := writeFileUnder(outDir, variants[i].URI, plan.Data); err != nil {
					return manifest.Model{}, nil, err
				}
				entry.Variants = append(entry.Variants, manifest.TextureVariant{
					URI: variants[i].URI, Tier: plan.Tier, Format: plan.Format, Bytes: variants[i].Bytes,
				})
			}
			model.Textures = append(model.Textures, entry)
			builtVariants[index] = variants
		}
	}
	dir := path.Dir(input.Name)
	for _, glb := range glbs {
		data := glb.data
		if len(builtVariants) > 0 {
			patched, err := injectImageVariants(data, builtVariants, dir)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %s keeps its original textures: %v", input.Name, glb.uri, err))
			} else {
				data = patched
			}
		}
		if err := writeFileUnder(outDir, glb.uri, data); err != nil {
			return manifest.Model{}, nil, err
		}
		model.LODs[glb.lod].Bytes = int64(len(data))
		if glb.lod == 0 {
			model.Bytes = int64(len(data))
		}
	}
	return model, warnings, nil
}

// lodDistance returns the camera distance at which a level that keeps the
// given fraction of the triangles takes over. A level with a quarter of the
// triangles tolerates about twice the geometric error, so the distance grows
// with the inverse square root of the fraction: scale * radius / sqrt(ratio).
// The scale is a tuning knob, not a measurement. Pass --lod-distance-scale to
// move every switch point.
func lodDistance(scale, radius, ratio float64) float64 {
	return scale * radius / math.Sqrt(math.Max(ratio, 1e-3))
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

func writeFileUnder(root, rel string, data []byte) error {
	clean := path.Clean(filepath.ToSlash(rel))
	if strings.HasPrefix(clean, "../") || clean == ".." || path.IsAbs(clean) {
		return fmt.Errorf("output path %q leaves the output directory", rel)
	}
	full := filepath.Join(root, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

// glbImage is one image of an analyzed GLB.
type glbImage struct {
	name     string
	data     []byte
	external string
	width    int
	height   int
	role     texture.Role
}

func (g glbImage) label(index int) string {
	name := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		}
		return '-'
	}, g.name)
	name = strings.Trim(name, "-")
	if name == "" {
		return fmt.Sprintf("image%d", index)
	}
	return fmt.Sprintf("%s-%d", name, index)
}

type glbAnalysis struct {
	stats        manifest.Stats
	textureBytes int64
	radius       float64
	images       []glbImage
	notes        []string
}

type texRef struct {
	Index *int `json:"index"`
}

type glbDoc struct {
	Scene  *int `json:"scene"`
	Scenes []struct {
		Nodes []int `json:"nodes"`
	} `json:"scenes"`
	Nodes []struct {
		Mesh        *int                       `json:"mesh"`
		Children    []int                      `json:"children"`
		Matrix      []float64                  `json:"matrix"`
		Translation []float64                  `json:"translation"`
		Rotation    []float64                  `json:"rotation"`
		Scale       []float64                  `json:"scale"`
		Extensions  map[string]json.RawMessage `json:"extensions"`
	} `json:"nodes"`
	Meshes []struct {
		Primitives []struct {
			Attributes map[string]int `json:"attributes"`
			Indices    *int           `json:"indices"`
			Mode       *int           `json:"mode"`
		} `json:"primitives"`
	} `json:"meshes"`
	Accessors []struct {
		Count int       `json:"count"`
		Min   []float64 `json:"min"`
		Max   []float64 `json:"max"`
	} `json:"accessors"`
	Skins []struct {
		Joints []int `json:"joints"`
	} `json:"skins"`
	Materials []struct {
		PBR struct {
			BaseColor         *texRef `json:"baseColorTexture"`
			MetallicRoughness *texRef `json:"metallicRoughnessTexture"`
		} `json:"pbrMetallicRoughness"`
		Normal    *texRef `json:"normalTexture"`
		Occlusion *texRef `json:"occlusionTexture"`
		Emissive  *texRef `json:"emissiveTexture"`
	} `json:"materials"`
	Textures []struct {
		Source *int `json:"source"`
	} `json:"textures"`
	Images []struct {
		URI        string `json:"uri"`
		Name       string `json:"name"`
		BufferView *int   `json:"bufferView"`
	} `json:"images"`
	BufferViews []struct {
		Buffer     int `json:"buffer"`
		ByteOffset int `json:"byteOffset"`
		ByteLength int `json:"byteLength"`
	} `json:"bufferViews"`
}

func splitGLBChunks(data []byte) (jsonChunk, bin []byte, err error) {
	if len(data) < 20 || string(data[:4]) != "glTF" {
		return nil, nil, errors.New("not a GLB file")
	}
	offset := 12
	for offset+8 <= len(data) {
		length := int(binary.LittleEndian.Uint32(data[offset:]))
		kind := binary.LittleEndian.Uint32(data[offset+4:])
		start := offset + 8
		if start+length > len(data) {
			return nil, nil, errors.New("GLB chunk runs past the end of the file")
		}
		switch kind {
		case 0x4E4F534A:
			jsonChunk = data[start : start+length]
		case 0x004E4942:
			if bin == nil {
				bin = data[start : start+length]
			}
		}
		offset = start + length
		if length%4 != 0 {
			offset += 4 - length%4
		}
	}
	if jsonChunk == nil {
		return nil, nil, errors.New("GLB has no JSON chunk")
	}
	return jsonChunk, bin, nil
}

// analyzeGLB counts triangles, draw calls, joints, and texture memory, and
// extracts the model bounds and image bytes. resolve supplies external image
// files.
func analyzeGLB(data []byte, resolve func(string) ([]byte, error)) (glbAnalysis, error) {
	jsonChunk, bin, err := splitGLBChunks(data)
	if err != nil {
		return glbAnalysis{}, err
	}
	var doc glbDoc
	if err := json.Unmarshal(jsonChunk, &doc); err != nil {
		return glbAnalysis{}, err
	}
	var out glbAnalysis

	for _, skin := range doc.Skins {
		if len(skin.Joints) > out.stats.Joints {
			out.stats.Joints = len(skin.Joints)
		}
	}
	out.stats.Materials = len(doc.Materials)

	var roots []int
	if len(doc.Scenes) > 0 {
		sceneIndex := 0
		if doc.Scene != nil && *doc.Scene >= 0 && *doc.Scene < len(doc.Scenes) {
			sceneIndex = *doc.Scene
		}
		roots = doc.Scenes[sceneIndex].Nodes
	} else {
		child := map[int]bool{}
		for _, n := range doc.Nodes {
			for _, c := range n.Children {
				child[c] = true
			}
		}
		for i := range doc.Nodes {
			if !child[i] {
				roots = append(roots, i)
			}
		}
	}

	min := [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	max := [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	vertices := map[int]bool{}
	var walk func(index int, parent mat4, depth int)
	walk = func(index int, parent mat4, depth int) {
		if index < 0 || index >= len(doc.Nodes) || depth > 256 {
			return
		}
		node := doc.Nodes[index]
		world := parent.mul(localNodeMat4(node.Matrix, node.Translation, node.Rotation, node.Scale))
		if node.Mesh != nil && *node.Mesh >= 0 && *node.Mesh < len(doc.Meshes) {
			instances := 1
			if raw, ok := node.Extensions["EXT_mesh_gpu_instancing"]; ok {
				var ext struct {
					Attributes map[string]int `json:"attributes"`
				}
				if json.Unmarshal(raw, &ext) == nil {
					for _, accessor := range ext.Attributes {
						if accessor >= 0 && accessor < len(doc.Accessors) {
							instances = doc.Accessors[accessor].Count
							break
						}
					}
				}
			}
			for _, primitive := range doc.Meshes[*node.Mesh].Primitives {
				out.stats.DrawCalls++
				count := 0
				position, hasPosition := primitive.Attributes["POSITION"]
				if primitive.Indices != nil && *primitive.Indices < len(doc.Accessors) {
					count = doc.Accessors[*primitive.Indices].Count
				} else if hasPosition && position < len(doc.Accessors) {
					count = doc.Accessors[position].Count
				}
				mode := 4
				if primitive.Mode != nil {
					mode = *primitive.Mode
				}
				triangles := 0
				switch mode {
				case 4:
					triangles = count / 3
				case 5, 6:
					if count >= 3 {
						triangles = count - 2
					}
				}
				out.stats.Triangles += triangles * instances
				if hasPosition && position < len(doc.Accessors) {
					accessor := doc.Accessors[position]
					if !vertices[position] {
						vertices[position] = true
						out.stats.Vertices += accessor.Count
					}
					if len(accessor.Min) == 3 && len(accessor.Max) == 3 {
						for corner := 0; corner < 8; corner++ {
							p := [3]float64{accessor.Min[0], accessor.Min[1], accessor.Min[2]}
							for axis := 0; axis < 3; axis++ {
								if corner>>axis&1 == 1 {
									p[axis] = accessor.Max[axis]
								}
							}
							w := world.point(p)
							for axis := 0; axis < 3; axis++ {
								min[axis] = math.Min(min[axis], w[axis])
								max[axis] = math.Max(max[axis], w[axis])
							}
						}
					}
				}
			}
		}
		for _, child := range node.Children {
			walk(child, world, depth+1)
		}
	}
	for _, root := range roots {
		walk(root, identityMat4(), 0)
	}
	if !math.IsInf(min[0], 0) {
		dx, dy, dz := max[0]-min[0], max[1]-min[1], max[2]-min[2]
		out.radius = round4(0.5 * math.Sqrt(dx*dx+dy*dy+dz*dz))
	}

	roleOf := map[int]texture.Role{}
	assign := func(ref *texRef, role texture.Role) {
		if ref == nil || ref.Index == nil || *ref.Index < 0 || *ref.Index >= len(doc.Textures) {
			return
		}
		source := doc.Textures[*ref.Index].Source
		if source == nil {
			return
		}
		if prior, ok := roleOf[*source]; ok && rolePriority(prior) >= rolePriority(role) {
			return
		}
		roleOf[*source] = role
	}
	for _, m := range doc.Materials {
		assign(m.PBR.BaseColor, texture.RoleBaseColor)
		assign(m.Normal, texture.RoleNormal)
		assign(m.PBR.MetallicRoughness, texture.RolePacked)
		assign(m.Emissive, texture.RoleEmissive)
		assign(m.Occlusion, texture.RoleMask)
	}

	for i, img := range doc.Images {
		entry := glbImage{name: img.Name, role: roleOf[i]}
		switch {
		case img.BufferView != nil && *img.BufferView >= 0 && *img.BufferView < len(doc.BufferViews):
			view := doc.BufferViews[*img.BufferView]
			if view.ByteOffset+view.ByteLength <= len(bin) {
				entry.data = bin[view.ByteOffset : view.ByteOffset+view.ByteLength]
			}
		case strings.HasPrefix(img.URI, "data:"):
			if comma := strings.IndexByte(img.URI, ','); comma >= 0 && strings.Contains(img.URI[:comma], "base64") {
				decoded, err := decodeBase64(img.URI[comma+1:])
				if err == nil {
					entry.data = decoded
				}
			}
		case img.URI != "":
			entry.external = img.URI
			if resolve != nil {
				if payload, err := resolve(img.URI); err == nil {
					entry.data = payload
				}
			}
		}
		if len(entry.data) > 0 {
			config, _, err := image.DecodeConfig(bytes.NewReader(entry.data))
			if err != nil {
				out.notes = append(out.notes, fmt.Sprintf("image %d: size unknown (%v), so it is not counted in textureBytes", i, err))
			} else {
				entry.width, entry.height = config.Width, config.Height
				out.textureBytes += mippedRGBA8Bytes(config.Width, config.Height)
			}
		}
		out.images = append(out.images, entry)
	}
	return out, nil
}

func rolePriority(r texture.Role) int {
	switch r {
	case texture.RoleBaseColor:
		return 5
	case texture.RoleNormal:
		return 4
	case texture.RolePacked:
		return 3
	case texture.RoleEmissive:
		return 2
	case texture.RoleMask:
		return 1
	}
	return 0
}

// mippedRGBA8Bytes is the GPU size of an RGBA8 texture with a full mip chain.
func mippedRGBA8Bytes(w, h int) int64 {
	var total int64
	for {
		total += int64(w) * int64(h) * 4
		if w == 1 && h == 1 {
			return total
		}
		if w > 1 {
			w /= 2
		}
		if h > 1 {
			h /= 2
		}
	}
}

// pendingGLB is an output GLB waiting for its texture variants.
type pendingGLB struct {
	uri  string
	data []byte
	lod  int
}

// ImageVariantsExtension is the glTF extension `gosx assets build` writes on
// each image it built KTX2 files for. The scene loader reads it and swaps the
// image for the best variant the device can upload. A device that can use none
// keeps the original image, which stays in the GLB.
const ImageVariantsExtension = "GOSX_texture_variants"

// injectImageVariants adds ImageVariantsExtension to the images of a GLB.
// variants maps an image index to its built variants. Variant URIs in the build
// are relative to the output root; dir is the GLB's directory under that root,
// and each URI becomes relative to the GLB.
func injectImageVariants(glb []byte, variants map[int][]Variant, dir string) ([]byte, error) {
	jsonChunk, bin, err := splitGLBChunks(glb)
	if err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(jsonChunk, &root); err != nil {
		return nil, err
	}
	var images []map[string]json.RawMessage
	if err := json.Unmarshal(root["images"], &images); err != nil {
		return nil, fmt.Errorf("images: %w", err)
	}
	prefix := ""
	if dir != "." && dir != "" {
		prefix = dir + "/"
	}
	changed := false
	for index, list := range variants {
		if index < 0 || index >= len(images) || len(list) == 0 {
			continue
		}
		entries := make([]Variant, 0, len(list))
		for _, v := range list {
			v.URI = strings.TrimPrefix(v.URI, prefix)
			entries = append(entries, v)
		}
		ext := map[string]json.RawMessage{}
		if raw, ok := images[index]["extensions"]; ok {
			if err := json.Unmarshal(raw, &ext); err != nil {
				return nil, fmt.Errorf("image %d extensions: %w", index, err)
			}
		}
		payload, err := json.Marshal(map[string]any{"variants": entries})
		if err != nil {
			return nil, err
		}
		ext[ImageVariantsExtension] = payload
		raw, err := json.Marshal(ext)
		if err != nil {
			return nil, err
		}
		images[index]["extensions"] = raw
		changed = true
	}
	if !changed {
		return glb, nil
	}
	if root["images"], err = json.Marshal(images); err != nil {
		return nil, err
	}
	var used []string
	if raw, ok := root["extensionsUsed"]; ok {
		_ = json.Unmarshal(raw, &used)
	}
	have := false
	for _, name := range used {
		have = have || name == ImageVariantsExtension
	}
	if !have {
		used = append(used, ImageVariantsExtension)
		root["extensionsUsed"], _ = json.Marshal(used)
	}
	newJSON, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	for len(newJSON)%4 != 0 {
		newJSON = append(newJSON, ' ')
	}
	for len(bin)%4 != 0 {
		bin = append(append([]byte(nil), bin...), 0)
	}
	total := 12 + 8 + len(newJSON)
	if len(bin) > 0 {
		total += 8 + len(bin)
	}
	out := make([]byte, 0, total)
	out = append(out, 'g', 'l', 'T', 'F')
	out = binary.LittleEndian.AppendUint32(out, 2)
	out = binary.LittleEndian.AppendUint32(out, uint32(total))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(newJSON)))
	out = binary.LittleEndian.AppendUint32(out, 0x4E4F534A)
	out = append(out, newJSON...)
	if len(bin) > 0 {
		out = binary.LittleEndian.AppendUint32(out, uint32(len(bin)))
		out = binary.LittleEndian.AppendUint32(out, 0x004E4942)
		out = append(out, bin...)
	}
	return out, nil
}
