package assetmeasure

import (
	"bytes"
	"compress/gzip"
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
