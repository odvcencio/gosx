package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"testing"

	"m31labs.dev/gosx/assetpipe/hdrimage"
	"m31labs.dev/gosx/assetpipe/ibl"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

// Literal expected controls, independent of buildFixture.
var (
	wantRadianceRGB   = [3]float32{0.75, 0.875, 1}
	wantLUTHalf       = [2]uint16{0x3800, 0x3400} // half(0.5), half(0.25)
	wantRadianceModel = ibl.BRDFModel
)

func checkMeta(t *testing.T, data []byte, role, model string) {
	t.Helper()
	kv, err := ktx2.KeyValues(data)
	if err != nil {
		t.Fatalf("KeyValues: %v", err)
	}
	want := map[string]string{
		"GoSXiblRole": role, "GoSXColorSpace": "linear", "GoSXiblModel": model,
	}
	for k, v := range want {
		if kv[k] != v {
			t.Errorf("metadata %s = %q, want %q", k, kv[k], v)
		}
	}
}

func TestBuildFixtureDeterministic(t *testing.T) {
	a, err := buildFixture()
	if err != nil {
		t.Fatalf("buildFixture: %v", err)
	}
	b, err := buildFixture()
	if err != nil {
		t.Fatalf("buildFixture: %v", err)
	}
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(ja, jb) {
		t.Fatal("buildFixture is not byte-deterministic across invocations")
	}
}

func TestContainers(t *testing.T) {
	p, err := buildFixture()
	if err != nil {
		t.Fatalf("buildFixture: %v", err)
	}

	// Radiance: RGBA16F cube, 2 mips (2x2, 1x1), 6 faces.
	img, err := ktx2.Parse(p.Radiance)
	if err != nil {
		t.Fatalf("parse radiance: %v", err)
	}
	if img.Format != ktx2.VkFormatR16G16B16A16Sfloat || img.Faces != 6 ||
		len(img.Levels) != 2 || img.Width != 2 || img.Height != 2 ||
		img.Levels[1].Width != 1 || img.Levels[1].Height != 1 {
		t.Fatalf("radiance container mismatch: %+v", img)
	}
	checkMeta(t, p.Radiance, string(ibl.ProductRoleRadiance), wantRadianceModel)

	chain, err := ibl.DecodeCubeKTX2(p.Radiance)
	if err != nil {
		t.Fatalf("decode radiance: %v", err)
	}
	for level, cube := range chain {
		for face := 0; face < 6; face++ {
			for i := 0; i < cube.Texels(); i++ {
				got := [3]float32{
					cube.Faces[face][i*3], cube.Faces[face][i*3+1], cube.Faces[face][i*3+2],
				}
				if got != wantRadianceRGB {
					t.Fatalf("radiance level %d face %d texel %d = %v, want %v",
						level, face, i, got, wantRadianceRGB)
				}
			}
		}
	}

	// Irradiance: zero RGB cube, 1 mip at 1x1, 6 faces.
	img, err = ktx2.Parse(p.Irradiance)
	if err != nil {
		t.Fatalf("parse irradiance: %v", err)
	}
	if img.Format != ktx2.VkFormatR16G16B16A16Sfloat || img.Faces != 6 ||
		len(img.Levels) != 1 || img.Width != 1 || img.Height != 1 {
		t.Fatalf("irradiance container mismatch: %+v", img)
	}
	checkMeta(t, p.Irradiance, string(ibl.ProductRoleIrradiance), "lambert-sh9")

	chain, err = ibl.DecodeCubeKTX2(p.Irradiance)
	if err != nil {
		t.Fatalf("decode irradiance: %v", err)
	}
	for face := 0; face < 6; face++ {
		for _, v := range chain[0].Faces[face] {
			if v != 0 {
				t.Fatalf("irradiance face %d texel = %v, want 0", face, v)
			}
		}
	}

	// BRDF LUT: RG16F, 1 mip at 1x1, single face; half payload must be
	// exactly half(0.5), half(0.25).
	img, err = ktx2.Parse(p.BRDFLUT)
	if err != nil {
		t.Fatalf("parse lut: %v", err)
	}
	if img.Format != ktx2.VkFormatR16G16Sfloat || img.Faces != 1 ||
		len(img.Levels) != 1 || img.Width != 1 || img.Height != 1 {
		t.Fatalf("lut container mismatch: %+v", img)
	}
	checkMeta(t, p.BRDFLUT, string(ibl.ProductRoleBRDFLUT), wantRadianceModel)
	raw := img.Levels[0].Bytes
	if len(raw) != 4 {
		t.Fatalf("lut payload = %d bytes, want 4", len(raw))
	}
	for i, want := range wantLUTHalf {
		if got := binary.LittleEndian.Uint16(raw[i*2:]); got != want {
			t.Fatalf("lut half %d = %#04x, want %#04x", i, got, want)
		}
	}
	values := [2]float32{
		hdrimage.HalfToFloat32(binary.LittleEndian.Uint16(raw[0:])),
		hdrimage.HalfToFloat32(binary.LittleEndian.Uint16(raw[2:])),
	}
	if values[0] != 0.5 || values[1] != 0.25 {
		t.Fatalf("lut values = %v, want [0.5 0.25]", values)
	}
}

func TestJSONRoundtrip(t *testing.T) {
	p, err := buildFixture()
	if err != nil {
		t.Fatalf("buildFixture: %v", err)
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out payload
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !bytes.Equal(out.Radiance, p.Radiance) ||
		!bytes.Equal(out.Irradiance, p.Irradiance) ||
		!bytes.Equal(out.BRDFLUT, p.BRDFLUT) {
		t.Fatal("blob roundtrip mismatch")
	}
	if !reflect.DeepEqual(out.Descriptor, p.Descriptor) {
		t.Fatal("descriptor roundtrip mismatch")
	}
	d := p.Descriptor
	if d.SchemaVersion != ibl.DescriptorSchemaVersion || d.Source != "synthetic-specular-isolation" ||
		d.Radiance.URI != "/ibl/spec-radiance.ktx2" || d.Radiance.MipLevels != 2 ||
		d.Irradiance.URI != "/ibl/spec-irradiance.ktx2" || d.BRDFLUT.URI != "/ibl/spec-brdf-lut.ktx2" ||
		d.BRDFModel != ibl.BRDFModel || !reflect.DeepEqual(d.RoughnessPerLevel, []float64{0, 1}) {
		t.Fatalf("descriptor literal controls mismatch: %+v", d)
	}
}
