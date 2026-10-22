package hub

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeMessageEmbedsRawJSONAsObject(t *testing.T) {
	state := []byte(`{"coreInner":"#ff8800","activity":0.42}`)
	asBytes, err := encodeMessage("tick", map[string]any{"frame": 7, "state": state})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(asBytes), `"state":"`) {
		t.Fatalf("[]byte must encode as a base64 string (documented pitfall): %s", asBytes)
	}
	asRaw, err := encodeMessage("tick", map[string]any{"frame": 7, "state": json.RawMessage(state)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(asRaw), `"state":{"coreInner":"#ff8800","activity":0.42}`) {
		t.Fatalf("json.RawMessage must embed the object: %s", asRaw)
	}
}
