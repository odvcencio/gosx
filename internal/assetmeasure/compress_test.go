package assetmeasure

import (
	"bytes"
	"compress/gzip"
	"runtime/debug"
	"testing"
)

func canonicalTestPin() CompressorPin {
	return CompressorPin{GoVersion: "1.26.0", BrotliVersion: "v1.2.1", GzipLevel: 9, BrotliQuality: 11}
}

// Test binaries omit module metadata; each case supplies its build identity.
func withBuildIdentity(t *testing.T, version string, replaced bool) {
	t.Helper()
	previous := readBuildInfo
	t.Cleanup(func() { readBuildInfo = previous })
	readBuildInfo = func() (*debug.BuildInfo, bool) {
		dep := &debug.Module{Path: "github.com/andybalholm/brotli", Version: version}
		if replaced {
			dep.Replace = &debug.Module{Path: "example.invalid/compressor", Version: version}
		}
		return &debug.BuildInfo{GoVersion: "go1.26.0", Deps: []*debug.Module{dep}}, true
	}
}

func TestAssetMeasureCanaries(t *testing.T) {
	withBuildIdentity(t, "v1.2.1", false)
	for _, test := range []struct {
		data              string
		raw, gzip, brotli int64
	}{
		{"", 0, 23, 1}, {"hello world\n", 12, 36, 16},
	} {
		data := []byte(test.data)
		a, err := Measure(data, canonicalTestPin())
		if err != nil {
			t.Fatal(err)
		}
		b, err := Measure(data, canonicalTestPin())
		if err != nil || a != b {
			t.Fatal("unstable normalization", a, b, err)
		}
		if a.Raw != test.raw || a.Gzip != test.gzip || a.Brotli != test.brotli {
			t.Fatal("changed compression canary", a, test)
		}
		gz, err := gzipCanonical(data, 9)
		if err != nil || gz[9] != 255 || !bytes.Equal(gz[4:8], []byte{0, 0, 0, 0}) {
			t.Fatal("noncanonical gzip header", err)
		}
		r, err := gzip.NewReader(bytes.NewReader(gz))
		if err != nil {
			t.Fatal(err)
		}
		if r.Name != "" || r.Comment != "" || len(r.Extra) != 0 || !r.ModTime.IsZero() {
			t.Fatal("gzip metadata leaked")
		}
		r.Close()
		br, err := brotliCanonical(data, 11, 0)
		if err != nil || VerifySidecar(data, gz, "gzip") != nil || VerifySidecar(data, br, "br") != nil {
			t.Fatal("canonical decode mismatch", err)
		}
	}
}

func TestAssetMeasureRejectPinAndMVS(t *testing.T) {
	for _, test := range []struct {
		version  string
		replaced bool
	}{{"v1.2.2", false}, {"v1.2.1", true}, {"", false}} {
		t.Run(test.version, func(t *testing.T) {
			withBuildIdentity(t, test.version, test.replaced)
			if _, err := Measure([]byte("body"), canonicalTestPin()); err == nil {
				t.Fatal("wrong MVS identity accepted")
			}
		})
	}
	withBuildIdentity(t, "v1.2.1", false)
	for field := 0; field < 5; field++ {
		pin := canonicalTestPin()
		switch field {
		case 0:
			pin.GoVersion = "1.26.1"
		case 1:
			pin.BrotliVersion = "v1.2.2"
		case 2:
			pin.GzipLevel = 1
		case 3:
			pin.BrotliQuality = 4
		case 4:
			pin.BrotliWindow = 22
		}
		if _, err := Measure(nil, pin); err == nil {
			t.Fatal("changed pin accepted")
		}
	}
	readBuildInfo = func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{GoVersion: "go1.26.0"}, true }
	if _, err := Measure(nil, canonicalTestPin()); err == nil {
		t.Fatal("missing selected module accepted")
	}
}

func TestAssetMeasureRejectCompilerIdentity(t *testing.T) {
	withBuildIdentity(t, "v1.2.1", false)
	for _, version := range []string{"", "go1.26.8"} {
		readBuildInfo = func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{GoVersion: version, Deps: []*debug.Module{{Path: "github.com/andybalholm/brotli", Version: "v1.2.1"}}}, true
		}
		if _, err := Measure([]byte("body"), canonicalTestPin()); err == nil {
			t.Fatal("compiler mismatch accepted")
		}
	}
	readBuildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	if _, err := Measure([]byte("body"), canonicalTestPin()); err == nil {
		t.Fatal("missing build identity accepted")
	}
}
