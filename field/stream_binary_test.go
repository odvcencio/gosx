package field

import (
	"encoding/json"
	"testing"

	"m31labs.dev/gosx/hub"
)

// TestPublishFieldBinaryFrameRoundTrip checks the opt-in binary transport.
func TestPublishFieldBinaryFrameRoundTrip(t *testing.T) {
	h := hub.New("field-binary")
	f := testField(8, 3)

	size, err := PublishFieldBinary(h, "velocity", f, QuantizeOptions{BitWidth: 6})
	if err != nil {
		t.Fatal(err)
	}
	if size <= 0 {
		t.Fatalf("frame size = %d, want a positive count", size)
	}

	// Rebuild the same frame so the test can decode it without a socket.
	q, err := f.QuantizeChecked(QuantizeOptions{BitWidth: 6})
	if err != nil {
		t.Fatal(err)
	}
	body, err := q.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	frame := append([]byte{fieldBinaryPrefix, byte(len("velocity"))}, "velocity"...)
	frame = append(frame, body...)

	topic, decoded, err := DecodeFieldFrame(frame)
	if err != nil {
		t.Fatal(err)
	}
	if topic != "velocity" {
		t.Fatalf("topic = %q, want %q", topic, "velocity")
	}
	got, err := decoded.DecompressChecked()
	if err != nil {
		t.Fatal(err)
	}
	want, err := q.DecompressChecked()
	if err != nil {
		t.Fatal(err)
	}
	sameData(t, "binary frame field", got.Data, want.Data)
}

func TestDecodeFieldFrameRejectsBadInput(t *testing.T) {
	if _, _, err := DecodeFieldFrame(nil); err == nil {
		t.Fatal("decoder accepted an empty frame")
	}
	if _, _, err := DecodeFieldFrame([]byte{0x01, 0x00}); err == nil {
		t.Fatal("decoder accepted a foreign prefix")
	}
	if _, _, err := DecodeFieldFrame([]byte{fieldBinaryPrefix, 0x40}); err == nil {
		t.Fatal("decoder accepted a truncated topic")
	}
}

// TestPublishFieldBinaryDeliversToLocalSubscribers proves the binary path keeps
// the local dispatch behaviour of PublishField.
func TestPublishFieldBinaryDeliversToLocalSubscribers(t *testing.T) {
	h := hub.New("field-binary-subs")
	ch := SubscribeField(h, "density")
	f := testField(6, 1)

	if _, err := PublishFieldBinary(h, "density", f, QuantizeOptions{BitWidth: 8}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if got == nil {
			t.Fatal("subscriber received a nil field")
		}
		if got.Resolution != f.Resolution || got.Components != f.Components {
			t.Fatalf("shape = %v/%d, want %v/%d",
				got.Resolution, got.Components, f.Resolution, f.Components)
		}
	default:
		t.Fatal("subscriber received nothing")
	}
}

// TestBinaryFrameIsSmallerThanJSONFrame compares the two transports end to end.
func TestBinaryFrameIsSmallerThanJSONFrame(t *testing.T) {
	f := testField(32, 3)
	q, err := f.QuantizeChecked(QuantizeOptions{BitWidth: 6})
	if err != nil {
		t.Fatal(err)
	}
	jsonPayload, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	binaryPayload, err := q.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	// The JSON transport also carries the hub envelope.
	jsonFrame := len(`{"event":"field:velocity","data":}`) + len(jsonPayload)
	binaryFrame := 1 + 1 + len("velocity") + len(binaryPayload)
	t.Logf("json frame=%d binary frame=%d saved=%.1f%%",
		jsonFrame, binaryFrame, 100*float64(jsonFrame-binaryFrame)/float64(jsonFrame))
	if binaryFrame >= jsonFrame {
		t.Fatalf("binary frame %d is not smaller than json frame %d", binaryFrame, jsonFrame)
	}
}
