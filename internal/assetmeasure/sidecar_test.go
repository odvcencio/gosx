package assetmeasure

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"testing"
)

func TestSidecarRejectMismatches(t *testing.T) {
	raw := bytes.Repeat([]byte("counter startup data\n"), 100)
	for _, encoding := range []string{"gzip", "br"} {
		var encoded []byte
		var err error
		if encoding == "gzip" {
			encoded, err = gzipCanonical(raw, 1)
		} else {
			encoded, err = brotliCanonical(raw, 4, 0)
		}
		if err != nil || VerifySidecar(raw, encoded, encoding) != nil {
			t.Fatal("valid release encoding rejected", err)
		}
		for _, bad := range [][]byte{nil, encoded[:len(encoded)-1], append(append([]byte(nil), encoded...), 0), append(append([]byte(nil), encoded...), encoded...)} {
			if VerifySidecar(raw, bad, encoding) == nil {
				t.Fatal("invalid sidecar accepted", encoding, len(bad))
			}
		}
		changed := append([]byte(nil), raw...)
		changed[len(changed)/2] ^= 1
		if VerifySidecar(changed, encoded, encoding) == nil {
			t.Fatal("same-length wrong SHA accepted")
		}
		if VerifySidecar(raw[:len(raw)-1], encoded, encoding) == nil {
			t.Fatal("expanded body exceeded limit")
		}
	}
	if VerifySidecar(raw, raw, "identity") == nil {
		t.Fatal("unknown sidecar encoding accepted")
	}
}

func TestSidecarNoncanonicalGzipRemainsSeparate(t *testing.T) {
	raw := bytes.Repeat([]byte("function counter(){return 123456789;}\n"), 40)
	canonical, err := gzipCanonical(raw, 9)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w, _ := gzip.NewWriterLevel(&out, 1)
	w.Name = "release.js"
	w.Write(raw)
	w.Close()
	if VerifySidecar(raw, out.Bytes(), "gzip") != nil || bytes.Equal(out.Bytes(), canonical) {
		t.Fatal("release representation was treated as canonical")
	}
}

func TestSidecarZopfliDoesNotDefineCanonicalSize(t *testing.T) {
	withBuildIdentity(t, "v1.2.1", false)
	raw := bytes.Repeat([]byte("function counter(){return 123456789;}\n"), 40)
	// Release producer canary using five Zopfli iterations.
	encoded, err := hex.DecodeString("1f8b08000000000002034b2bcd4b2ec9cccf5348ce2fcd2b492dd2d0ac2e4a2d292dca5330343236313533b7b0b4aee51aeaaa46558daa1a5535aa6a541500e656f91bf0050000")
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySidecar(raw, encoded, "gzip"); err != nil {
		t.Fatal(err)
	}
	sizes, err := Measure(raw, canonicalTestPin())
	if err != nil {
		t.Fatal(err)
	}
	if sizes.Gzip != 75 || len(encoded) != 71 {
		t.Fatal("canonical and release producers were conflated", sizes.Gzip, len(encoded))
	}
}
