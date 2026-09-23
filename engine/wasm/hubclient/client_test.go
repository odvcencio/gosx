package hubclient

import (
	"encoding/json"
	"testing"
)

func TestEnvelopePreservesRawStatePayload(t *testing.T) {
	raw := []byte(`{"event":"state","data":{"phase":"playing","enemies":[{"id":"e1"}]}}`)
	event, data, err := decodeEnvelope(raw)
	if err != nil || event != "state" || string(data) != `{"phase":"playing","enemies":[{"id":"e1"}]}` {
		t.Fatalf("decode = %q %s %v", event, data, err)
	}
	encoded, err := encodeEnvelope("input", map[string]any{"dash": true})
	if err != nil {
		t.Fatal(err)
	}
	var got wireMessage
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got.Event != "input" || string(got.Data) != `{"dash":true}` {
		t.Fatalf("encoded = %s", encoded)
	}
}

func TestEnvelopeRejectsInvalidEventsAndPayloads(t *testing.T) {
	for _, raw := range []string{`{"event":"","data":null}`, `[]`, `{`} {
		if _, _, err := decodeEnvelope([]byte(raw)); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if _, err := encodeEnvelope("  ", nil); err == nil {
		t.Fatal("empty outbound event accepted")
	}
	if _, err := encodeEnvelope("input", make(chan int)); err == nil {
		t.Fatal("unsupported payload accepted")
	}
}

func TestFrameLimitIsBounded(t *testing.T) {
	for _, options := range []Options{{}, {MaxFrameBytes: -1}, {MaxFrameBytes: DefaultMaxFrameBytes + 1}} {
		if got := frameLimit(options); got != DefaultMaxFrameBytes {
			t.Fatalf("limit = %d", got)
		}
	}
	if got := frameLimit(Options{MaxFrameBytes: 128}); got != 128 {
		t.Fatalf("limit = %d", got)
	}
}
