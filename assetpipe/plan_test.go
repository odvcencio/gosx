package assetpipe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/render/bundle/ktx2"
)

func TestPlanSceneAssetOptimizationSurface(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "public", "models", "hero.glb"), buildTestGLB(t, map[string]any{
		"asset":              map[string]any{"version": "2.0"},
		"extensionsUsed":     []string{"KHR_materials_clearcoat", "KHR_draco_mesh_compression"},
		"extensionsRequired": []string{"KHR_materials_clearcoat"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0},
				"extensions": map[string]any{"KHR_draco_mesh_compression": map[string]any{"bufferView": 0}},
			}},
		}},
		"materials": []map[string]any{{
			"extensions": map[string]any{"KHR_materials_clearcoat": map[string]any{"clearcoatFactor": 1}},
		}},
		"images":     []map[string]any{{"uri": "albedo.png"}},
		"textures":   []map[string]any{{"source": 0}},
		"animations": []map[string]any{{"name": "idle"}},
		"skins":      []map[string]any{{"joints": []int{0}}},
		"nodes":      []map[string]any{{"mesh": 0}},
	}))
	mustWriteBytes(t, filepath.Join(dir, "public", "textures", "albedo.png"), []byte("png"))
	mustWriteBytes(t, filepath.Join(dir, "public", "env", "studio.hdr"), []byte("#?RADIANCE\n"))
	mustWriteBytes(t, filepath.Join(dir, "public", "audio", "hit.ogg"), []byte("ogg"))
	mustWriteBytes(t, filepath.Join(dir, "public", "shaders", "spark.wgsl"), []byte("@compute @workgroup_size(1) fn main() {}\n"))
	mustWriteBytes(t, filepath.Join(dir, "public", "textures", "cube.ktx2"), buildTestKTX2(ktx2.VkFormatBC7SRGBBlock, 8, 4, 1, 1, 1, [][]byte{make([]byte, 32)}))

	report, err := Plan([]string{dir}, Options{TurboQuantBitWidth: 10, TurboQuantPreviewBits: 5})
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", report.SchemaVersion, SchemaVersion)
	}
	if report.Totals.Assets != 6 || report.Totals.GLB != 1 || report.Totals.KTX2 != 1 ||
		report.Totals.Texture != 1 || report.Totals.Environment != 1 || report.Totals.Audio != 1 || report.Totals.Shader != 1 {
		t.Fatalf("unexpected totals: %+v", report.Totals)
	}
	if report.Totals.Variants == 0 {
		t.Fatalf("expected planned variants in totals: %+v", report.Totals)
	}
	if report.Budget.ExpectedGPUBytes == 0 || report.Totals.ExpectedGPUBytes != report.Budget.ExpectedGPUBytes {
		t.Fatalf("expected GPU budget totals, budget=%+v totals=%+v", report.Budget, report.Totals)
	}

	hero := findAsset(t, report, "public/models/hero.glb")
	if hero.GLTF == nil || hero.GLTF.Primitives != 1 || hero.GLTF.Images != 1 || hero.GLTF.Animations != 1 || hero.GLTF.Skins != 1 {
		t.Fatalf("unexpected gltf probe: %+v", hero.GLTF)
	}
	assertAction(t, hero, "turboquant-streams", "candidate")
	assertActionTargetContains(t, hero, "turboquant-streams", "10-bit")
	assertAction(t, hero, "draco-compress", "present")
	assertAction(t, hero, "texture-transcode-ktx2", "candidate")
	assertAction(t, hero, "preserve-pbr-extensions", "candidate")
	assertAction(t, hero, "animation-stream-quantization", "candidate")
	assertVariant(t, hero, "public/models/hero.meshopt.glb", "meshopt")
	assertVariant(t, hero, "public/models/hero.tq.glb", "turboquant")
	assertVariant(t, hero, "public/models/hero.textures.bc7.ktx2", "ktx2-bc7")

	ktx := findAsset(t, report, "public/textures/cube.ktx2")
	if ktx.KTX2 == nil || ktx.KTX2.FormatName != "BC7_SRGB_BLOCK" || !ktx.KTX2.Compressed {
		t.Fatalf("unexpected ktx2 probe: %+v", ktx.KTX2)
	}
	assertAction(t, ktx, "webgpu-upload-ready", "ready")

	env := findAsset(t, report, "public/env/studio.hdr")
	if env.Environment == nil || env.Environment.PlanKind != "ggx-prefiltered-cubemap" || env.Environment.TargetCubeSize == 0 || env.Environment.EstimatedGeneratedBytes == 0 {
		t.Fatalf("unexpected environment probe: %+v", env.Environment)
	}
	assertAction(t, env, "prefilter-ibl-ggx", "candidate")
	assertAction(t, env, "generate-split-sum-lut", "candidate")
	assertVariant(t, env, "public/env/studio.ibl.ktx2", "ktx2")

	audio := findAsset(t, report, "public/audio/hit.ogg")
	if audio.Media == nil || audio.Media.Kind != "audio" || !audio.Media.Streamable {
		t.Fatalf("unexpected audio metadata: %+v", audio.Media)
	}
	assertAction(t, audio, "positional-audio-node", "planned")
	assertVariant(t, audio, "public/audio/hit.opus", "opus")

	shader := findAsset(t, report, "public/shaders/spark.wgsl")
	if shader.Shader == nil || len(shader.Shader.EntryPoints) != 1 || shader.Shader.EntryPoints[0].Stage != "compute" || !shader.Shader.HasWorkgroupSize {
		t.Fatalf("unexpected shader probe: %+v", shader.Shader)
	}
	assertAction(t, shader, "shader-entrypoint-inventory", "ready")
}

func TestPlanLargeGLTFUsesFallbackWithoutReadingWholeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "public", "models", "huge.gltf")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(4096); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	report, err := Plan([]string{dir}, Options{MaxProbeBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	asset := findAsset(t, report, "public/models/huge.gltf")
	if len(asset.Warnings) == 0 || !strings.Contains(asset.Warnings[0], errProbeTooLarge.Error()) {
		t.Fatalf("expected max probe warning, got %+v", asset.Warnings)
	}
	assertAction(t, asset, "inspect-gltf", "planned")
	assertAction(t, asset, "turboquant-streams", "candidate")
}

func TestPlanHTMLTextureManifest(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "public", "ui", "hud.html-textures.json"), []byte(`{
		"surfaces": [
			{
				"id": "hud-panel",
				"textureWidth": 512,
				"textureHeight": 256,
				"maxTexturePixels": 262144,
				"dirtyRegions": [{"x": 0, "y": 0, "width": 128, "height": 64}],
				"accessibilityFallback": {"role": "region"}
			},
			{
				"id": "readout",
				"width": 128,
				"height": 64,
				"fallback": {"mode": "dom"}
			}
		]
	}`))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.Assets != 1 || report.Totals.HTMLTextureManifest != 1 {
		t.Fatalf("unexpected totals: %+v", report.Totals)
	}
	asset := findAsset(t, report, "public/ui/hud.html-textures.json")
	if asset.Kind != "html-texture-manifest" || asset.HTML == nil {
		t.Fatalf("unexpected html texture manifest asset: %+v", asset)
	}
	if asset.HTML.Surfaces != 2 || asset.HTML.TexturePixels != 139264 || asset.HTML.TextureBytes != 557056 {
		t.Fatalf("unexpected html texture manifest probe: %+v", asset.HTML)
	}
	if asset.HTML.MaxTexturePixels != 262144 || asset.HTML.DirtyRegionEntries != 1 || asset.HTML.AccessibilityFallbacks != 2 {
		t.Fatalf("unexpected html texture manifest metadata: %+v", asset.HTML)
	}
	assertAction(t, asset, "measure-html-textures", "planned")
	assertAction(t, asset, "enforce-html-texture-budgets", "planned")
	assertAction(t, asset, "dirty-region-upload-plan", "candidate")
	assertAction(t, asset, "accessibility-mirror-dom", "planned")
	assertActionTargetContains(t, asset, "enforce-html-texture-budgets", "557056 bytes")
}

func TestPlanMediaBuffersAndBasisInventory(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "public", "video", "demo.mp4"), []byte("mp4"))
	mustWriteBytes(t, filepath.Join(dir, "public", "models", "scene.bin"), []byte("bin"))
	mustWriteBytes(t, filepath.Join(dir, "public", "textures", "albedo.basis"), []byte("basis"))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Totals.Video != 1 || report.Totals.DataBuffer != 1 || report.Totals.BasisTexture != 1 {
		t.Fatalf("unexpected totals: %+v", report.Totals)
	}
	video := findAsset(t, report, "public/video/demo.mp4")
	if video.Media == nil || video.Media.Kind != "video" || video.Media.Container != "mp4" || !video.Media.Streamable {
		t.Fatalf("unexpected video media metadata: %+v", video.Media)
	}
	assertAction(t, video, "video-surface-manifest", "planned")
	assertVariant(t, video, "public/video/demo.webm", "vp9-opus")

	buffer := findAsset(t, report, "public/models/scene.bin")
	assertAction(t, buffer, "buffer-inventory", "planned")

	basis := findAsset(t, report, "public/textures/albedo.basis")
	assertAction(t, basis, "basis-transcode-ktx2", "candidate")
	assertVariant(t, basis, "public/textures/albedo.bc7.ktx2", "ktx2-bc7")
}

func TestPlanStructuredDiagnosticsFromWarnings(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "public", "ui", "empty.html-textures.json"), []byte(`{}`))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	asset := findAsset(t, report, "public/ui/empty.html-textures.json")
	if len(asset.Diagnostics) != 1 || asset.Diagnostics[0].Code != "html-texture-manifest" {
		t.Fatalf("unexpected asset diagnostics: %+v", asset.Diagnostics)
	}
	if len(report.Diagnostics) != 1 || report.Diagnostics[0].Path != "public/ui/empty.html-textures.json" {
		t.Fatalf("unexpected report diagnostics: %+v", report.Diagnostics)
	}
}

// TestSkinInfoFromGLTF unit-tests the mapping helper directly — no binary
// fixture required.
func TestSkinInfoFromGLTF(t *testing.T) {
	t.Run("skins>0 sets Skinned=true", func(t *testing.T) {
		info := GLTFInfo{Skins: 2, Meshes: 1}
		got := skinInfoFromGLTF(info)
		if !got.Skinned {
			t.Error("expected Skinned=true for Skins=2; got false")
		}
		if got.MorphTargets {
			t.Error("expected MorphTargets=false (not probed); got true")
		}
	})

	t.Run("skins==0 sets Skinned=false", func(t *testing.T) {
		info := GLTFInfo{Skins: 0, Meshes: 3}
		got := skinInfoFromGLTF(info)
		if got.Skinned {
			t.Error("expected Skinned=false for Skins=0; got true")
		}
	})
}

// TestPlanSkinManifest verifies that Plan populates Report.SkinManifest for a
// skinned GLB and omits non-skinned assets.
func TestPlanSkinManifest(t *testing.T) {
	dir := t.TempDir()
	// Skinned GLB: one skin.
	mustWriteBytes(t, filepath.Join(dir, "public", "models", "soldier.glb"), buildTestGLB(t, map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{"primitives": []map[string]any{{"attributes": map[string]any{"POSITION": 0}}}}},
		"skins":  []map[string]any{{"joints": []int{0}}},
		"nodes":  []map[string]any{{"mesh": 0}},
	}))
	// Static (non-skinned) GLB.
	mustWriteBytes(t, filepath.Join(dir, "public", "models", "rock.glb"), buildTestGLB(t, map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{"primitives": []map[string]any{{"attributes": map[string]any{"POSITION": 0}}}}},
	}))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	// SkinManifest must include the skinned asset.
	si, ok := report.SkinManifest["public/models/soldier.glb"]
	if !ok {
		t.Fatalf("expected public/models/soldier.glb in SkinManifest; got %v", report.SkinManifest)
	}
	if !si.Skinned {
		t.Error("expected Skinned=true for soldier.glb")
	}

	// Non-skinned asset must not appear.
	if _, bad := report.SkinManifest["public/models/rock.glb"]; bad {
		t.Error("expected rock.glb absent from SkinManifest (no skins)")
	}
}

func findAsset(t *testing.T, report Report, path string) Asset {
	t.Helper()
	for _, asset := range report.Assets {
		if asset.Path == path {
			return asset
		}
	}
	t.Fatalf("asset %s not found in %+v", path, report.Assets)
	return Asset{}
}

func assertAction(t *testing.T, asset Asset, name, status string) {
	t.Helper()
	for _, action := range asset.Actions {
		if action.Name == name {
			if action.Status != status {
				t.Fatalf("%s action status = %q, want %q", name, action.Status, status)
			}
			return
		}
	}
	t.Fatalf("action %s not found in %+v", name, asset.Actions)
}

func assertActionTargetContains(t *testing.T, asset Asset, name, want string) {
	t.Helper()
	for _, action := range asset.Actions {
		if action.Name == name {
			if !strings.Contains(action.Target, want) {
				t.Fatalf("%s target = %q, want substring %q", name, action.Target, want)
			}
			return
		}
	}
	t.Fatalf("action %s not found in %+v", name, asset.Actions)
}

func assertVariant(t *testing.T, asset Asset, uri, compression string) {
	t.Helper()
	for _, variant := range asset.Variants {
		if variant.URI == uri {
			if variant.Compression != compression {
				t.Fatalf("%s compression = %q, want %q", uri, variant.Compression, compression)
			}
			return
		}
	}
	t.Fatalf("variant %s not found in %+v", uri, asset.Variants)
}

func mustWriteBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func buildTestGLB(t *testing.T, doc map[string]any) []byte {
	t.Helper()
	jsonData, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(jsonData)%4 != 0 {
		jsonData = append(jsonData, ' ')
	}
	total := 12 + 8 + len(jsonData)
	var buf bytes.Buffer
	buf.WriteString("glTF")
	write32(&buf, 2)
	write32(&buf, uint32(total))
	write32(&buf, uint32(len(jsonData)))
	write32(&buf, 0x4E4F534A)
	buf.Write(jsonData)
	return buf.Bytes()
}

func buildTestKTX2(vkFormat int, width, height, depth, layers, faces uint32, levelData [][]byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xAB, 'K', 'T', 'X', ' ', '2', '0', 0xBB, 0x0D, 0x0A, 0x1A, 0x0A})

	header := make([]byte, 68)
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(header[off:], v) }
	put64 := func(off int, v uint64) { binary.LittleEndian.PutUint64(header[off:], v) }
	put32(0, uint32(vkFormat))
	put32(4, 1)
	put32(8, width)
	put32(12, height)
	put32(16, depth)
	put32(20, layers)
	put32(24, faces)
	put32(28, uint32(len(levelData)))
	put64(52, 0)
	put64(60, 0)
	buf.Write(header)

	indexStart := buf.Len()
	dataStart := indexStart + len(levelData)*24
	index := make([]byte, len(levelData)*24)
	running := uint64(dataStart)
	for i, level := range levelData {
		binary.LittleEndian.PutUint64(index[i*24+0:], running)
		binary.LittleEndian.PutUint64(index[i*24+8:], uint64(len(level)))
		binary.LittleEndian.PutUint64(index[i*24+16:], uint64(len(level)))
		running += uint64(len(level))
	}
	buf.Write(index)
	for _, level := range levelData {
		buf.Write(level)
	}
	return buf.Bytes()
}

func write32(buf *bytes.Buffer, value uint32) {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], value)
	buf.Write(tmp[:])
}
