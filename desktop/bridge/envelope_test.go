package bridge

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseEnvelopeRequest(t *testing.T) {
	env, err := ParseEnvelope(`{"op":"req","id":"42","method":"file.open","payload":{"path":"/tmp/x"}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if env.Op != OpRequest || env.ID != "42" || env.Method != "file.open" {
		t.Fatalf("envelope fields = %+v", env)
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Path != "/tmp/x" {
		t.Fatalf("payload.path = %q", payload.Path)
	}
}

func TestParseEnvelopeInvalidJSON(t *testing.T) {
	_, err := ParseEnvelope(`not json`)
	var be *Error
	if !errors.As(err, &be) {
		t.Fatalf("error type = %T, want *bridge.Error", err)
	}
	if be.Code != CodeDecode {
		t.Fatalf("code = %q, want %q", be.Code, CodeDecode)
	}
}

func TestParseEnvelopeMissingOp(t *testing.T) {
	_, err := ParseEnvelope(`{"id":"1","method":"x"}`)
	var be *Error
	if !errors.As(err, &be) {
		t.Fatalf("error = %v, want *bridge.Error", err)
	}
	if be.Code != CodeDecode {
		t.Fatalf("code = %q", be.Code)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	original := Envelope{
		Op:      OpResponse,
		ID:      "abc",
		Payload: json.RawMessage(`{"ok":true}`),
	}
	raw, err := EncodeEnvelope(original)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if decoded.Op != original.Op || decoded.ID != original.ID {
		t.Fatalf("mismatch: %+v vs %+v", decoded, original)
	}
	if string(decoded.Payload) != `{"ok":true}` {
		t.Fatalf("payload = %s", decoded.Payload)
	}
}

func TestEncodeRejectsEmptyOp(t *testing.T) {
	_, err := EncodeEnvelope(Envelope{})
	if err == nil {
		t.Fatal("expected error for empty op")
	}
}

func TestNewResponseEnvelope(t *testing.T) {
	env, err := newResponseEnvelope("7", map[string]int{"n": 3})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if env.Op != OpResponse || env.ID != "7" {
		t.Fatalf("envelope = %+v", env)
	}
	if string(env.Payload) != `{"n":3}` {
		t.Fatalf("payload = %s", env.Payload)
	}
}

func TestNewErrorEnvelope(t *testing.T) {
	env := newErrorEnvelope("9", &Error{Code: "x.y", Message: "nope"})
	if env.Op != OpError || env.ID != "9" || env.Error == nil {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Error.Code != "x.y" {
		t.Fatalf("code = %q", env.Error.Code)
	}
}

func TestNewStreamFrameAndEnd(t *testing.T) {
	frame, err := newStreamFrameEnvelope("s1", []int{1, 2, 3})
	if err != nil {
		t.Fatalf("frame: %v", err)
	}
	if frame.Op != OpStreamFrame || frame.ID != "s1" {
		t.Fatalf("frame = %+v", frame)
	}
	end := newStreamEndEnvelope("s1")
	if end.Op != OpStreamEnd || end.ID != "s1" {
		t.Fatalf("end = %+v", end)
	}
}

func TestNewEventEnvelope(t *testing.T) {
	env, err := newEventEnvelope("toast", map[string]string{"msg": "hi"})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if env.Op != OpEvent || env.Method != "toast" || env.ID != "" {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestMarshalPayloadNil(t *testing.T) {
	buf, err := marshalPayload(nil)
	if err != nil {
		t.Fatalf("nil: %v", err)
	}
	if buf != nil {
		t.Fatalf("expected nil RawMessage, got %s", buf)
	}
}

func TestMarshalPayloadPassThrough(t *testing.T) {
	raw := json.RawMessage(`{"cached":true}`)
	buf, err := marshalPayload(raw)
	if err != nil {
		t.Fatalf("raw: %v", err)
	}
	if string(buf) != string(raw) {
		t.Fatalf("RawMessage re-encoded")
	}
}

func TestErrorStringIncludesDetail(t *testing.T) {
	e := &Error{Code: "a.b", Message: "no", Detail: "ctx"}
	got := e.Error()
	want := "a.b: no (ctx)"
	if got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if (&Error{Code: "c", Message: "d"}).Error() != "c: d" {
		t.Fatal("detail-less formatting wrong")
	}
	var nilErr *Error
	if nilErr.Error() != "" {
		t.Fatal("nil Error should stringify empty")
	}
}
