package worklet

import (
	"bytes"
	"crypto/sha256"
	"fmt"
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
