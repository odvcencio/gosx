package worklet

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The HTTP half of the processor asset checks lives in handler_test.go, which
// builds only where Handler does. This half runs in every build, including
// js/wasm, where Source and Path are the whole asset API.
func TestProcessorAsset(t *testing.T) {
	data := Source()
	if len(data) > 24*1024 {
		t.Fatalf("processor exceeds 24 KiB raw budget: %d", len(data))
	}
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	_, _ = z.Write(data)
	_ = z.Close()
	if compressed.Len() > 6*1024 {
		t.Fatalf("processor exceeds 6 KiB gzip budget: %d", compressed.Len())
	}
	data[0] ^= 1
	if bytes.Equal(data, Source()) {
		t.Fatal("caller can mutate the shared processor")
	}
}

func TestPathIsContentAddressed(t *testing.T) {
	sum := sha256.Sum256(Source())
	want := "worklet/wasm-processor." + hex.EncodeToString(sum[:])[:16] + ".js"
	if got := Path(); got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestBackendValidation(t *testing.T) {
	valid := Backend{MaxFrames: 64, Channels: 1, CommandBytes: 4, MessageBytes: 4, MessageCapacity: 8}
	if _, err := valid.Options(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Backend){
		func(b *Backend) { b.Channels = 0 },
		func(b *Backend) { b.MaxFrames = 4097 },
		func(b *Backend) { b.CommandBytes = 0 },
		func(b *Backend) { b.MessageCapacity = -1 },
		func(b *Backend) { b.FaultOpcode = 7; b.FaultArgumentOffset = 3 },
		func(b *Backend) { b.OpcodeCapabilities = []OpcodeCapability{{Opcode: 9, Mask: 1}} },
		func(b *Backend) { b.Transport = &Transport{Play: 1, Stop: 2, Seek: 3, TicksPerBeat: 1, TicksPerBar: 4} },
	} {
		b := valid
		change(&b)
		if _, err := b.Options(); err == nil {
			t.Fatalf("accepted invalid backend %+v", b)
		}
	}
	if err := valid.ValidateImage(ImageInfo{RequiredCapabilities: 1}); err == nil {
		t.Fatal("accepted unsupported image capability")
	}
}
