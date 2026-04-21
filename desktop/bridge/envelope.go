// Package bridge carries typed IPC between a GoSX desktop host and the
// page it hosts inside a native webview.
//
// All traffic travels as an [Envelope] serialized to JSON. The webview
// transport is still raw strings (postWebMessageAsString / OnWebMessage),
// but every frame carries an opcode, a correlation ID, and optional typed
// payload or error — so requests round-trip with a response, errors
// surface as typed frames instead of silent drops, and long-running calls
// can stream partial results back under the same ID.
package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Op names the kind of frame travelling through the bridge.
type Op string

const (
	// OpRequest originates in the webview: page asks the host to run a
	// registered method with the given Payload. Host replies with
	// OpResponse / OpError / OpStream* under the same ID.
	OpRequest Op = "req"

	// OpResponse is the terminal success frame for a request. Payload
	// carries the return value, or is empty for void methods.
	OpResponse Op = "res"

	// OpError is the terminal failure frame for a request. Error is
	// populated; Payload is ignored.
	OpError Op = "err"

	// OpStreamFrame delivers one chunk of a streamed response. Multiple
	// frames share the same ID; the exchange ends with OpStreamEnd.
	OpStreamFrame Op = "frame"

	// OpStreamEnd terminates a streaming exchange successfully. No
	// payload. A streaming exchange that fails terminates with OpError
	// instead.
	OpStreamEnd Op = "end"

	// OpEvent is a fire-and-forget host→page notification. No
	// correlation ID required; the page consumes it by method name.
	OpEvent Op = "evt"
)

// Envelope is the wire frame for a single IPC message.
type Envelope struct {
	// Op distinguishes request, response, error, stream frame, stream
	// terminator, or one-way event.
	Op Op `json:"op"`

	// ID correlates a request with its response / error / stream frames.
	// Required for OpRequest / OpResponse / OpError / OpStreamFrame /
	// OpStreamEnd. Empty for OpEvent.
	ID string `json:"id,omitempty"`

	// Method names the operation. Required for OpRequest / OpEvent.
	// Optional on other ops (the router already has the correlation ID).
	Method string `json:"method,omitempty"`

	// Payload carries the operation's data as raw JSON so the router can
	// decode it into the handler-chosen type without re-parsing the
	// whole envelope.
	Payload json.RawMessage `json:"payload,omitempty"`

	// Error is populated for OpError frames. Ignored otherwise.
	Error *Error `json:"error,omitempty"`
}

// Error is the typed failure surface returned to the page when a handler
// returns or panics. Code is a stable machine-readable string; Message
// is human-readable; Detail is optional additional context.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// Standard error codes. Handlers can emit their own codes too — these
// cover the bridge-produced failure modes.
const (
	CodeDecode      = "bridge.decode"       // envelope failed to parse
	CodeNotFound    = "bridge.not_found"    // method not registered
	CodeInternal    = "bridge.internal"     // handler panic / unexpected
	CodeRateLimited = "bridge.rate_limited" // rate limit rejected message
	CodeTooLarge    = "bridge.too_large"    // payload exceeded size cap
	CodeInvalidOp   = "bridge.invalid_op"   // unknown / disallowed op
)

// Error returns the formatted code+message so bridge.Error satisfies
// the error interface. Detail is included when present.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Detail)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ParseEnvelope decodes a JSON string off the webview wire. Returns a
// typed bridge.Error with CodeDecode on parse failure so the caller can
// surface the reason consistently.
func ParseEnvelope(raw string) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		return Envelope{}, &Error{
			Code:    CodeDecode,
			Message: "envelope is not valid JSON",
			Detail:  err.Error(),
		}
	}
	if env.Op == "" {
		return Envelope{}, &Error{
			Code:    CodeDecode,
			Message: "envelope missing op",
		}
	}
	return env, nil
}

// EncodeEnvelope serializes an envelope for transport. Payload is
// validated as JSON if set so the page-side runtime never receives a
// malformed frame.
func EncodeEnvelope(env Envelope) (string, error) {
	if env.Op == "" {
		return "", errors.New("bridge: envelope missing op")
	}
	buf, err := json.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("bridge: marshal envelope: %w", err)
	}
	return string(buf), nil
}

// newResponseEnvelope builds a response frame carrying payload under the
// given correlation ID.
func newResponseEnvelope(id string, payload any) (Envelope, error) {
	buf, err := marshalPayload(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Op: OpResponse, ID: id, Payload: buf}, nil
}

// newErrorEnvelope builds an error frame for the given correlation ID.
// The id may be empty when the bridge fails before a request ID is
// known (e.g. envelope-parse failure).
func newErrorEnvelope(id string, bridgeErr *Error) Envelope {
	return Envelope{Op: OpError, ID: id, Error: bridgeErr}
}

// newStreamFrameEnvelope builds a partial-result frame under id.
func newStreamFrameEnvelope(id string, payload any) (Envelope, error) {
	buf, err := marshalPayload(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Op: OpStreamFrame, ID: id, Payload: buf}, nil
}

// newStreamEndEnvelope builds the terminal frame for a streaming exchange.
func newStreamEndEnvelope(id string) Envelope {
	return Envelope{Op: OpStreamEnd, ID: id}
}

// newEventEnvelope builds a fire-and-forget host→page event.
func newEventEnvelope(method string, payload any) (Envelope, error) {
	buf, err := marshalPayload(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Op: OpEvent, Method: method, Payload: buf}, nil
}

// marshalPayload converts any Go value to a json.RawMessage. A nil
// payload becomes an empty RawMessage so the envelope omits the field.
func marshalPayload(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	if raw, ok := v.(json.RawMessage); ok {
		return raw, nil
	}
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("bridge: marshal payload: %w", err)
	}
	return buf, nil
}
