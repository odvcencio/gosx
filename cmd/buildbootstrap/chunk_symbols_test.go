package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
)

// chunk_symbols_test.go pins which symbols each chunk carries.
//
// Every chunk is one lazily fetched script, and the chunk table is the only
// thing that decides what lands in it. Two failures follow from one wrong line
// in that table, and no other gate sees either one:
//
//  1. A chunk loses a symbol it reads. closure_test.go covers that class.
//  2. A chunk gains a symbol another chunk already carries. The browser then
//     downloads the same code twice. The repository has paid this three times:
//     16-scene-webgl.js in the base scene3d chunk cost a Chromium page 160_835
//     minified bytes it never ran, 16b-scene-compute.js shipped twice for
//     27_651 bytes, and the text-layout engine shipped in bootstrap-lite.js
//     for 42_738 bytes on pages with no text block.
//
// The size gates in bootstrap-size.test.mjs measure total bytes, so they can
// see a large regression as a budget breach, but they cannot say which symbol
// moved. The tests below name the symbol and the chunk.

// declaredNames collects every name a chunk declares, at any scope depth. The
// parser builds real scopes, so a name inside a comment or a string never
// appears here.
type declaredNames struct {
	names map[string]bool
}

func (d *declaredNames) Enter(n js.INode) js.IVisitor {
	if block, ok := n.(*js.BlockStmt); ok {
		for _, v := range block.Scope.Declared {
			d.names[string(v.Name())] = true
		}
	}
	return d
}

func (d *declaredNames) Exit(js.INode) {}

// chunkDeclaredNames parses one chunk and returns every name it declares.
func chunkDeclaredNames(t *testing.T, dir string, entry output) map[string]bool {
	t.Helper()
	var b strings.Builder
	for _, src := range entry.sources {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(src.rel)))
		if err != nil {
			t.Fatalf("read %s: %v", src.rel, err)
		}
		if err := validateTypedSource(src, data); err != nil {
			t.Fatalf("validate %s: %v", src.rel, err)
		}
		b.WriteString(normalizeNewlines(string(data)))
		b.WriteByte('\n')
	}
	chunkSource, err := transpileTypedChunk(entry, b.String())
	if err != nil {
		t.Fatalf("transpile %s: %v", entry.name, err)
	}
	ast, err := js.Parse(parse.NewInputString(chunkSource), js.Options{})
	if err != nil {
		t.Fatalf("parse %s: %v", entry.name, err)
	}
	collector := &declaredNames{names: map[string]bool{}}
	js.Walk(collector, ast)
	for _, v := range ast.BlockStmt.Scope.Declared {
		collector.names[string(v.Name())] = true
	}
	return collector.names
}

// symbolPlacement states one symbol, the chunks that must carry it, and the
// chunks that must not. Each entry carries the reason, because the reason is
// what a reader needs when the test fails.
type symbolPlacement struct {
	symbol string
	in     []string
	notIn  []string
	why    string
}

// shippedSymbolPlacements is the pinned map of symbol to chunk. Every entry
// records a split the repository made on purpose, with the cost it removed.
var shippedSymbolPlacements = []symbolPlacement{
	{
		symbol: "SCENE_PBR_FRAGMENT_SOURCE",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-webgl.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "the WebGL2 renderer moved out of the base scene3d chunk; a WebGPU page never runs it and must not download it (160_835 minified bytes)",
	},
	{
		symbol: "createScenePBRRenderer",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-webgl.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js"},
		why:    "same split as SCENE_PBR_FRAGMENT_SOURCE: 20-scene-mount.js fetches the WebGL chunk only when the backend order picks WebGL",
	},
	{
		symbol: "SCENE_COMPUTE_PARTICLE_SOURCE",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-compute.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "16b-scene-compute.js shipped in the base chunk AND the webgpu chunk until v0.35.8, which cost a Chromium page 27_651 duplicate bytes. It then shipped once, in the base chunk. It now has its own chunk, because a scene with one cube and one directional light runs no particle simulation and paid 8_772 gzip bytes for it",
	},
	{
		symbol: "createSceneComputeParticleSystem",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-compute.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "same split as SCENE_COMPUTE_PARTICLE_SOURCE",
	},
	{
		symbol: "createSceneCPUParticleSystem",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-compute.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgl.js"},
		why:    "the CPU particle fallback moved with the GPU path. WebGL drives it through the same system objects, and it now resolves createSceneParticleSystem through window.__gosx_scene3d_api at call time",
	},
	{
		symbol: "textMeasureCache",
		in:     []string{"bootstrap.js", "bootstrap-feature-textlayout.js"},
		notIn:  []string{"bootstrap-lite.js", "bootstrap-runtime.js"},
		why:    "the text-layout engine left lite and runtime; it cost every page 42_738 minified bytes in lite and 42_751 in runtime, including pages with no text block",
	},
	{
		symbol: "textLayoutMergedReason",
		in:     []string{"bootstrap.js", "bootstrap-feature-textlayout.js"},
		notIn:  []string{"bootstrap-lite.js", "bootstrap-runtime.js"},
		why:    "same split as textMeasureCache",
	},
	{
		symbol: "sceneParseGLB",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-gltf.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js"},
		why:    "the glTF parser is lazy; a galaxy or particle scene loads no model asset, and ensureGLTFFeatureLoaded fetches the chunk on the first model request",
	},
	{
		symbol: "createSceneAnimationMixer",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-animation.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js"},
		why:    "the keyframe and skeletal mixer is lazy; a scene with no clip skips the bone math and the quaternion slerp",
	},
	{
		symbol: "BOARD_TEXT_WGSL",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-webgpu.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgl.js"},
		why:    "the glyph shader belongs to the WebGPU renderer; render/boardgpu/board_text_drift_test.go pins this copy to the Go source of truth",
	},
	{
		symbol: "mountAllControllers",
		in:     []string{"bootstrap.js", "bootstrap-feature-controllers.js"},
		notIn:  []string{"bootstrap-lite.js", "bootstrap-runtime.js", "bootstrap-feature-islands.js"},
		why:    "the declarative controller runtime is its own feature chunk; a page with no controller must not carry it",
	},
	{
		symbol: "createSceneWebGLRenderer",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-webgl.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "the legacy vertex-colour renderer left 10-runtime-scene-core.js for 16e-scene-webgl-legacy.js; only createSceneWebGLResult reaches it, and the PBR factory it backs up already ships in the WebGL chunk, so a WebGPU page can never run it",
	},
	{
		symbol: "createSceneThickLineProgram",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-webgl.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js"},
		why:    "same split as createSceneWebGLRenderer: the thick-line expansion belongs to the legacy renderer",
	},
	{
		symbol: "sceneAllocateTextureUnits",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-webgl.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "the texture-unit table and the IBL budget moved to 15a1-scene-texture-budget.js; 16-scene-webgl.js is their only caller in the tree, so a WebGPU page stops paying for a WebGL2 sampler table",
	},
	{
		symbol: "sceneParseRadianceHDR",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-webgl.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "16b-scene-hdr.js followed its only caller, 16-scene-webgl.js. The WebGPU renderer has no Radiance HDR path at all",
	},
	{
		symbol: "createSceneInstancedCullSystem",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-compute.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "the GPU instanced cull ships with the particle systems it shares a file with; the WebGPU renderer already read it through the API object at call time",
	},
	{
		symbol: "sceneDecompressProps",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-decompress.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js"},
		why:    "11a-scene-decompress.ts became a gated chunk. createSceneState awaits it before it decodes, and a scene with plain float arrays never fetches it",
	},
	{
		symbol: "sceneGeneratePointsArrays",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-decompress.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js"},
		why:    "11b-scene-points-generate.js shares the decompress chunk because the two files call each other: sceneDecompressProps expands a generator before it decodes, and a generated layer can arrive compressed",
	},
	{
		symbol: "SCENE_TEXTURE_UNIT_MATERIALS",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-webgl.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "the texture-unit table moved with sceneAllocateTextureUnits into 15a1-scene-texture-budget.js. Pinned separately because no bridge aliases this name, so it proves the payload moved and not just the entry point",
	},
	{
		symbol: "sceneUnpackIndices",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-decompress.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js"},
		why:    "the index unpacker has no bridge, so it proves the whole decompress payload left the base chunk and not only the entry points",
	},
	{
		symbol: "sceneKTX2Parse",
		in:     []string{"bootstrap.js", "bootstrap-feature-scene3d-gltf.js"},
		notIn:  []string{"bootstrap-feature-scene3d.js", "bootstrap-feature-scene3d-webgl.js", "bootstrap-feature-scene3d-webgpu.js"},
		why:    "only 19-scene-gltf.js reads the KTX2 reader lexically, so it ships with the glTF parser. A renderer reaches it through window.__gosx_scene3d_ktx2 at call time, and a page that loads two renderers must not download two copies",
	},
	{
		symbol: "entryRequiresAsyncWebGPUProbe",
		in:     []string{"bootstrap.js", "bootstrap-feature-islands.js", "bootstrap-feature-engines.js"},
		notIn:  []string{"bootstrap-lite.js"},
		why:    "this one IS in two feature chunks on purpose: the hydration path and the engine path both call it, and each chunk loads on its own. The marker based build duplicated it silently because the hydration extract range contained the probe extract range",
	},
}

// TestShippedChunksCarryTheExpectedSymbols checks the pinned map against the
// real source tree.
func TestShippedChunksCarryTheExpectedSymbols(t *testing.T) {
	dir := shippedClientJS(t)
	known := map[string]bool{}
	declared := map[string]map[string]bool{}
	for _, entry := range outputs {
		known[entry.name] = true
		declared[entry.name] = chunkDeclaredNames(t, dir, entry)
	}

	for _, want := range shippedSymbolPlacements {
		t.Run(want.symbol, func(t *testing.T) {
			for _, name := range append(append([]string{}, want.in...), want.notIn...) {
				if !known[name] {
					t.Fatalf("this test names chunk %q, which the chunk table does not build", name)
				}
			}
			for _, name := range want.in {
				if !declared[name][want.symbol] {
					t.Errorf("chunk %s no longer declares %s.\nWhy it must: %s", name, want.symbol, want.why)
				}
			}
			for _, name := range want.notIn {
				if declared[name][want.symbol] {
					t.Errorf("chunk %s now declares %s, so a browser downloads that code twice.\nWhy it must not: %s",
						name, want.symbol, want.why)
				}
			}
		})
	}
}

// filesInMoreThanOneLazyChunk lists the source files that two or more lazily
// fetched chunks carry. A file in two lazy chunks duplicates every byte it
// holds for any page that loads both.
func filesInMoreThanOneLazyChunk() map[string][]string {
	owners := map[string][]string{}
	for _, entry := range outputs {
		// bootstrap.js, bootstrap-lite.js and bootstrap-runtime.js are the three
		// eager entry bundles. A page loads exactly one of them, so a file they
		// share costs nothing.
		if !strings.HasPrefix(entry.name, "bootstrap-feature-") {
			continue
		}
		for _, src := range entry.sources {
			owners[src.rel] = append(owners[src.rel], entry.name)
		}
	}
	for rel, chunks := range owners {
		if len(chunks) < 2 {
			delete(owners, rel)
		}
	}
	return owners
}

// TestNoUnexpectedSourceFileShipsInTwoLazyChunks pins the duplicate payload
// inventory. Every entry in the allowlist below is deliberate and carries its
// reason. A new duplicate fails here, at the file that caused it, instead of
// showing up later as a size budget breach with no named cause.
func TestNoUnexpectedSourceFileShipsInTwoLazyChunks(t *testing.T) {
	allowed := map[string]string{
		"bootstrap-src/30h-tail-capability-probe.js": "the islands chunk and the engines chunk both call entryRequiresAsyncWebGPUProbe, and either can load without the other",
	}
	for rel, chunks := range filesInMoreThanOneLazyChunk() {
		if _, ok := allowed[rel]; ok {
			continue
		}
		t.Errorf("%s ships in %d lazily fetched chunks (%s), so a page that loads more than one downloads it twice.\n"+
			"Move it to one chunk and bridge the symbols through a window.__gosx_* handoff, or add it to the allowlist in this test with the reason",
			rel, len(chunks), strings.Join(chunks, ", "))
	}
	// Keep the allowlist honest: an entry that no longer describes the table is
	// stale documentation, and stale documentation outlives the split it named.
	current := filesInMoreThanOneLazyChunk()
	for rel := range allowed {
		if _, ok := current[rel]; !ok {
			t.Errorf("the allowlist in this test still names %s, which no longer ships in two lazy chunks; remove the entry", rel)
		}
	}
}

// TestEveryChunkNameIsUniqueAndEveryChunkHasSources guards the table shape. Two
// entries with one name make the second overwrite the first, and the build
// reports success either way.
func TestEveryChunkNameIsUniqueAndEveryChunkHasSources(t *testing.T) {
	seen := map[string]bool{}
	for _, entry := range outputs {
		if seen[entry.name] {
			t.Errorf("two chunks build %s; the second overwrites the first", entry.name)
		}
		seen[entry.name] = true
		if len(entry.sources) == 0 {
			t.Errorf("chunk %s lists no sources", entry.name)
		}
		within := map[string]bool{}
		for _, src := range entry.sources {
			if within[src.rel] {
				t.Errorf("chunk %s lists %s twice, so the bundle holds two copies of it", entry.name, src.rel)
			}
			within[src.rel] = true
			if src.label != src.rel {
				t.Errorf("chunk %s labels %s as %q; the source map label must be the path", entry.name, src.rel, src.label)
			}
		}
	}
}
