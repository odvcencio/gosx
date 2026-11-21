package worklet

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestPortableProcessorURLMatchesImmutableSource(t *testing.T) {
	data := Source()
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if want := "worklet/wasm-processor." + digest[:16] + ".js"; Path() != want {
		t.Fatalf("processor URL %q, want %q", Path(), want)
	}
	data[0] ^= 1
	if bytes.Equal(data, Source()) || Path() != "worklet/wasm-processor."+digest[:16]+".js" {
		t.Fatal("caller mutation changed the processor source or URL")
	}
}

func TestDataURLCarriesTheProcessorSource(t *testing.T) {
	const prefix = "data:text/javascript;base64,"
	url := DataURL()
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("data URL starts %q, want %q", url[:min(len(url), 40)], prefix)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, prefix))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, Source()) {
		t.Fatal("data URL does not decode to the processor source")
	}
}
