// Opt-in measurements for the executor stages. Set GOSX_MEASURE=1 to run
// them. They report timings and output sizes at production settings, which
// keeps the build-time cost of a stage visible.
package assetpipe

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/gosx/assetpipe/hdrimage"
	"m31labs.dev/gosx/assetpipe/ibl"
	"m31labs.dev/gosx/assetpipe/meshsimplify"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

func writeSkyHDR(width, height int) []byte {
	var buf bytes.Buffer
	buf.WriteString("#?RADIANCE\nFORMAT=32-bit_rle_rgbe\n\n")
	fmt.Fprintf(&buf, "-Y %d +X %d\n", height, width)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			theta := math.Pi * (float64(y) + 0.5) / float64(height)
			phi := 2 * math.Pi * (float64(x) + 0.5) / float64(width)
			sky := math.Max(0, math.Cos(theta))
			r := 0.4 + 0.6*sky
			g := 0.5 + 0.5*sky
			b := 0.9 + 0.1*sky
			// A small bright sun makes the prefilter work.
			if math.Abs(theta-0.6) < 0.03 && math.Abs(phi-2.0) < 0.03 {
				r, g, b = 400, 380, 340
			}
			peak := math.Max(r, math.Max(g, b))
			mantissa, exponent := math.Frexp(peak)
			scale := mantissa * 256 / peak
			buf.Write([]byte{byte(r * scale), byte(g * scale), byte(b * scale), byte(exponent + 128)})
		}
	}
	return buf.Bytes()
}

func TestMeasureIBLStage(t *testing.T) {
	if os.Getenv("GOSX_MEASURE") == "" {
		t.Skip("set GOSX_MEASURE=1")
	}
	dir := t.TempDir()
	source := writeSkyHDR(2048, 1024)
	mustWriteBytes(t, filepath.Join(dir, "env", "sky.hdr"), source)
	t.Logf("source hdr bytes: %d", len(source))

	start := time.Now()
	image, err := hdrimage.Decode(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("decode 2048x1024 radiance: %v", time.Since(start))

	start = time.Now()
	cube, err := ibl.EquirectToCube(equirectSource{image}, 256, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("project to 256 cube (2x2 samples): %v", time.Since(start))

	start = time.Now()
	chain := ibl.Prefilter(cube, ibl.PrefilterOptions{Samples: 128, MipSelect: true})
	t.Logf("prefilter 256 cube, 128 samples, %d levels: %v", len(chain), time.Since(start))

	start = time.Now()
	sh := ibl.ProjectSH(ibl.BuildChain(cube)[3])
	irradiance := ibl.IrradianceCube(sh, 32)
	t.Logf("spherical harmonics and 32 irradiance cube: %v", time.Since(start))

	start = time.Now()
	specular, err := ibl.EncodeCubeKTX2(chain, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("encode specular ktx2: %v, %d bytes", time.Since(start), len(specular))

	diffuse, err := ibl.EncodeCubeKTX2(ibl.Chain{irradiance}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("encode irradiance ktx2: %d bytes", len(diffuse))

	parsed, err := ktx2.Parse(specular)
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	packed, err := ktx2.Encode(parsed, ktx2.EncodeOptions{Supercompression: ktx2.SupercompressionZlib, ZlibLevel: 6})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("specular with zlib supercompression: %d bytes (%.1f%% of plain) in %v",
		len(packed), 100*float64(len(packed))/float64(len(specular)), time.Since(start))

	start = time.Now()
	lut := ibl.GenerateBRDFLUT(256, 1024)
	t.Logf("bake 256 split-sum lut, 1024 samples: %v", time.Since(start))
	encoded, err := ibl.EncodeBRDFLUTKTX2(lut, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("encode lut ktx2: %d bytes", len(encoded))

	// Whole stage through the public entry point.
	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	_, execReport, err := Execute(report, ExecuteOptions{Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Execute total: %v, %+v", time.Since(start), execReport.Totals)
	for _, result := range execReport.Results {
		t.Logf("  %s %s %dms %d bytes %v", result.Action, result.Status, result.DurationMS, result.OutputBytes, result.Metrics)
	}
}

func sphereGLB(t *testing.T, rings, segments int) []byte {
	t.Helper()
	var positions, normals []float32
	for ring := 0; ring <= rings; ring++ {
		phi := math.Pi * float64(ring) / float64(rings)
		for segment := 0; segment < segments; segment++ {
			theta := 2 * math.Pi * float64(segment) / float64(segments)
			x := math.Sin(phi) * math.Cos(theta)
			y := math.Cos(phi)
			z := math.Sin(phi) * math.Sin(theta)
			positions = append(positions, float32(x), float32(y), float32(z))
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
	var bin bytes.Buffer
	for _, value := range positions {
		binary.Write(&bin, binary.LittleEndian, value)
	}
	normalOffset := bin.Len()
	for _, value := range normals {
		binary.Write(&bin, binary.LittleEndian, value)
	}
	indexOffset := bin.Len()
	for _, value := range indices {
		binary.Write(&bin, binary.LittleEndian, value)
	}
	indexLength := bin.Len() - indexOffset

	doc := map[string]any{
		"asset": map[string]any{"version": "2.0"},
		"meshes": []map[string]any{{
			"primitives": []map[string]any{{
				"attributes": map[string]any{"POSITION": 0, "NORMAL": 1},
				"indices":    2,
				"mode":       4,
			}},
		}},
		"accessors": []map[string]any{
			{"bufferView": 0, "componentType": 5126, "count": len(positions) / 3, "type": "VEC3",
				"min": []float64{-1, -1, -1}, "max": []float64{1, 1, 1}},
			{"bufferView": 1, "componentType": 5126, "count": len(normals) / 3, "type": "VEC3"},
			{"bufferView": 2, "componentType": 5125, "count": len(indices), "type": "SCALAR"},
		},
		"bufferViews": []map[string]any{
			{"buffer": 0, "byteOffset": 0, "byteLength": normalOffset, "target": 34962},
			{"buffer": 0, "byteOffset": normalOffset, "byteLength": indexOffset - normalOffset, "target": 34962},
			{"buffer": 0, "byteOffset": indexOffset, "byteLength": indexLength, "target": 34963},
		},
		"buffers": []map[string]any{{"byteLength": bin.Len()}},
	}
	jsonData, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	for len(jsonData)%4 != 0 {
		jsonData = append(jsonData, ' ')
	}
	payload := bin.Bytes()
	for len(payload)%4 != 0 {
		payload = append(payload, 0)
	}
	total := 12 + 8 + len(jsonData) + 8 + len(payload)
	var out bytes.Buffer
	out.WriteString("glTF")
	write32(&out, 2)
	write32(&out, uint32(total))
	write32(&out, uint32(len(jsonData)))
	write32(&out, 0x4E4F534A)
	out.Write(jsonData)
	write32(&out, uint32(len(payload)))
	write32(&out, 0x004E4942)
	out.Write(payload)
	return out.Bytes()
}

func TestMeasureLODStage(t *testing.T) {
	if os.Getenv("GOSX_MEASURE") == "" {
		t.Skip("set GOSX_MEASURE=1")
	}
	dir := t.TempDir()
	glb := sphereGLB(t, 160, 256)
	mustWriteBytes(t, filepath.Join(dir, "models", "sphere.glb"), glb)
	t.Logf("source glb bytes: %d, triangles: %d", len(glb), 160*256*2)

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, execReport, err := Execute(report, ExecuteOptions{
		Root: dir,
		Only: []string{"build-lod-stack"},
		LOD:  LODOptions{Ratios: []float64{0.5, 0.25, 0.1}, MeasureError: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Execute LOD total: %v", time.Since(start))
	for _, result := range execReport.Results {
		t.Logf("  %s %s %dms %d bytes %v", result.Action, result.Status, result.DurationMS, result.OutputBytes, result.Metrics)
	}
	sidecar, err := os.ReadFile(filepath.Join(dir, "models", "sphere.lod.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("sidecar:\n%s", sidecar)

	// Bare simplifier timing without any glTF work.
	var positions []float32
	var indices []uint32
	for ring := 0; ring <= 160; ring++ {
		phi := math.Pi * float64(ring) / 160
		for segment := 0; segment < 256; segment++ {
			theta := 2 * math.Pi * float64(segment) / 256
			positions = append(positions,
				float32(math.Sin(phi)*math.Cos(theta)),
				float32(math.Cos(phi)),
				float32(math.Sin(phi)*math.Sin(theta)))
		}
	}
	for ring := 0; ring < 160; ring++ {
		for segment := 0; segment < 256; segment++ {
			a := uint32(ring*256 + segment)
			b := uint32(ring*256 + (segment+1)%256)
			c := a + 256
			d := b + 256
			indices = append(indices, a, c, b, b, c, d)
		}
	}
	mesh := meshsimplify.Mesh{Positions: positions, Indices: indices}
	start = time.Now()
	result := meshsimplify.Simplify(mesh, meshsimplify.Options{TargetRatio: 0.25})
	simplifyTime := time.Since(start)
	start = time.Now()
	stats := meshsimplify.MeasureError(mesh, result.Mesh)
	t.Logf("simplify %d -> %d triangles: %v; measure: %v; maxFraction %.6f rmsFraction %.6f",
		mesh.TriangleCount(), result.OutputTriangles, simplifyTime, time.Since(start), stats.MaxFraction, stats.RMSFraction)
}
