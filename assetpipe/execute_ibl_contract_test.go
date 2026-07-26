package assetpipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx/assetpipe/ibl"
	"m31labs.dev/gosx/render/bundle/ktx2"
)

// TestIBLSidecarPinsBRDFModel checks the three records of the split-sum
// convention against each other.
//
// ibl.BRDFModel is authoritative. The sidecar's brdfModel field and the
// GoSXiblModel key of every generated container are copies the executor writes
// from it. A consumer that reads a copy must get the same string, otherwise a
// shader author can follow one record and the lookup table can follow another.
func TestIBLSidecarPinsBRDFModel(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "env", "studio.hdr"), writeTestHDR(t, 16, 8, 1, 1, 1))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = Execute(report, ExecuteOptions{
		Root: dir,
		IBL:  IBLOptions{CubeSize: 8, Samples: 8, IrradianceSize: 4, BRDFLUTSize: 8, BRDFSamples: 16},
	}); err != nil {
		t.Fatal(err)
	}

	sidecarBytes, err := os.ReadFile(filepath.Join(dir, "env", "studio.ibl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sidecar iblSidecar
	if err := json.Unmarshal(sidecarBytes, &sidecar); err != nil {
		t.Fatal(err)
	}
	if sidecar.BRDFModel != ibl.BRDFModel {
		t.Fatalf("the sidecar copy is %q, the authority is %q", sidecar.BRDFModel, ibl.BRDFModel)
	}
	if !strings.Contains(sidecar.Consumer.BRDFModelAuthority, "assetpipe/ibl.BRDFModel") {
		t.Fatalf("the sidecar does not name the authority: %q", sidecar.Consumer.BRDFModelAuthority)
	}

	// Both container copies must agree with the authority too.
	for _, name := range []string{"studio.ibl.ktx2", "studio.brdf-lut.ktx2"} {
		data, err := os.ReadFile(filepath.Join(dir, "env", name))
		if err != nil {
			t.Fatal(err)
		}
		keys, err := ktx2.KeyValues(data)
		if err != nil {
			t.Fatal(err)
		}
		if keys["GoSXiblModel"] != ibl.BRDFModel {
			t.Fatalf("%s carries GoSXiblModel %q, the authority is %q", name, keys["GoSXiblModel"], ibl.BRDFModel)
		}
	}

	// The lookup table container must also pin the axis convention, because a
	// shader that offsets the coordinates reads the wrong texels.
	lut, err := os.ReadFile(filepath.Join(dir, "env", "studio.brdf-lut.ktx2"))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ktx2.KeyValues(lut)
	if err != nil {
		t.Fatal(err)
	}
	if keys["GoSXlutAxes"] != "x=NdotV,y=roughness,texel-centre" {
		t.Fatalf("the lookup table axes key is %q", keys["GoSXlutAxes"])
	}
}

// TestIBLSidecarReportsTheMissingConsumer checks that the build states the gap
// in the file it writes, next to the products nobody reads.
func TestIBLSidecarReportsTheMissingConsumer(t *testing.T) {
	dir := t.TempDir()
	mustWriteBytes(t, filepath.Join(dir, "env", "studio.hdr"), writeTestHDR(t, 16, 8, 1, 1, 1))

	report, err := Plan([]string{dir}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = Execute(report, ExecuteOptions{
		Root: dir,
		IBL:  IBLOptions{CubeSize: 8, Samples: 8, IrradianceSize: 4, BRDFLUTSize: 8, BRDFSamples: 16},
	}); err != nil {
		t.Fatal(err)
	}
	sidecarBytes, err := os.ReadFile(filepath.Join(dir, "env", "studio.ibl.json"))
	if err != nil {
		t.Fatal(err)
	}
	var sidecar iblSidecar
	if err := json.Unmarshal(sidecarBytes, &sidecar); err != nil {
		t.Fatal(err)
	}
	if sidecar.Consumer.Ready {
		t.Fatal("the sidecar claims a ready consumer; no renderer samples these products")
	}
	got := append([]string(nil), sidecar.Consumer.Missing...)
	sort.Strings(got)
	want := []string{"brdf-lut-upload", "cube-upload", "ir-carrier", "ktx2-reader-js", "shader-combine"}
	if len(got) != len(want) {
		t.Fatalf("the sidecar lists %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("the sidecar lists %v, want %v", got, want)
		}
	}
	if !strings.Contains(sidecar.Consumer.Note, "tone maps") {
		t.Fatalf("the sidecar note does not say what the runtime does instead: %q", sidecar.Consumer.Note)
	}
}
