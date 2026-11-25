package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// This file holds the wire protocol between a test driver (the native server in
// game/host/hosttest) and a page (the browser adapter, or a Go fake page in
// tests). It lives in package host, and not in hosttest, because hosttest
// imports host for GamepadState: a browser adapter inside host could not import
// hosttest without a cycle. hosttest re-exports every name here as an alias.

// PadTestProtocolVersion is the protocol version in hello and ready.
const PadTestProtocolVersion = 1

// PadTestPath is the fixed WebSocket path. A page never accepts a path from the
// URL.
const PadTestPath = "/gosx-padtest"

// PadTestQueryField is the one query field that opts a page in.
const PadTestQueryField = "gosx-padtest"

// PadTestMaxMessage is the largest message, in bytes, either side accepts.
const PadTestMaxMessage = 64 << 10

// Message types. The sender is shown in brackets.
const (
	PadMsgHello    = "hello"    // [page] Version, Token
	PadMsgReady    = "ready"    // [server] Version
	PadMsgSet      = "set"      // [server] Seq, At, Frame (when At is "frame"), Pads
	PadMsgApplied  = "applied"  // [page] Seq, Latched, Frame, Drawn, Sampled
	PadMsgRejected = "rejected" // [page] Seq, Frame (the current frame), Reason
	PadMsgWatch    = "watch"    // [server] Watch, Drawn, Sampled, Relative
	PadMsgProgress = "progress" // [page] Watch (0 if unsolicited), Drawn, Sampled, Seq, Frame
)

// Values of PadMessage.At.
const (
	PadAtNext  = "next"
	PadAtFrame = "frame"
)

// Values of PadMessage.Reason.
const (
	PadRejectLate     = "late"     // exact frame already begun
	PadRejectMissed   = "missed"   // exact frame passed while an earlier change held the queue
	PadRejectSequence = "sequence" // sequence number did not increase
	PadRejectInvalid  = "invalid"  // validation failed
	PadRejectLost     = "lost"     // the source was shut down
)

// PadWire is one pad on the wire.
type PadWire struct {
	Index   int       `json:"index"`
	ID      string    `json:"id"`
	Mapping string    `json:"mapping"`
	Buttons []float64 `json:"buttons"`
	Axes    []float64 `json:"axes"`
}

// PadMessage is every message of the protocol. Type picks the fields in use
// (see the Msg constants); unused fields are zero and omitted. Decoding rejects
// unknown fields.
type PadMessage struct {
	Type     string    `json:"type"`
	Version  int       `json:"version,omitempty"`
	Token    string    `json:"token,omitempty"`
	Seq      uint64    `json:"seq,omitempty"`
	At       string    `json:"at,omitempty"`
	Frame    uint64    `json:"frame,omitempty"`
	Latched  uint64    `json:"latched,omitempty"`
	Pads     []PadWire `json:"pads,omitempty"`
	Watch    uint64    `json:"watch,omitempty"`
	Drawn    uint64    `json:"drawn,omitempty"`
	Sampled  uint64    `json:"sampled,omitempty"`
	Relative bool      `json:"relative,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

// PadWireFrom converts states to wire form, copying their slices.
func PadWireFrom(states []GamepadState) []PadWire {
	if len(states) == 0 {
		return nil
	}
	out := make([]PadWire, len(states))
	for i, s := range states {
		out[i] = PadWire{
			Index: s.Index, ID: s.ID, Mapping: s.Mapping,
			Buttons: append([]float64(nil), s.Buttons...),
			Axes:    append([]float64(nil), s.Axes...),
		}
	}
	return out
}

// PadStatesFromWire converts wire pads to states, copying their slices.
func PadStatesFromWire(pads []PadWire) []GamepadState {
	if len(pads) == 0 {
		return nil
	}
	out := make([]GamepadState, len(pads))
	for i, p := range pads {
		out[i] = GamepadState{
			Index: p.Index, ID: p.ID, Mapping: p.Mapping,
			Buttons: append([]float64(nil), p.Buttons...),
			Axes:    append([]float64(nil), p.Axes...),
		}
	}
	return out
}

// EncodePadMessage encodes m as JSON. It fails if m is larger than
// PadTestMaxMessage.
func EncodePadMessage(m PadMessage) ([]byte, error) {
	if m.Type == "" {
		return nil, errors.New("host: pad message has no type")
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if len(data) > PadTestMaxMessage {
		return nil, fmt.Errorf("host: pad message is %d bytes, limit %d", len(data), PadTestMaxMessage)
	}
	return data, nil
}

// DecodePadMessage decodes one message. It rejects unknown fields, trailing
// data, an unknown type and a message over PadTestMaxMessage.
func DecodePadMessage(data []byte) (PadMessage, error) {
	var m PadMessage
	if len(data) > PadTestMaxMessage {
		return m, fmt.Errorf("host: pad message is %d bytes, limit %d", len(data), PadTestMaxMessage)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return PadMessage{}, fmt.Errorf("host: bad pad message: %w", err)
	}
	// Nothing may follow the object, not even a stray closing brace.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return PadMessage{}, errors.New("host: trailing data after pad message")
	}
	switch m.Type {
	case PadMsgHello, PadMsgReady, PadMsgSet, PadMsgApplied, PadMsgRejected, PadMsgWatch, PadMsgProgress:
	default:
		return PadMessage{}, fmt.Errorf("host: unknown pad message type %q", m.Type)
	}
	return m, nil
}

// FormatPadTestEndpoint returns the value of the query field: "<port>.<token>".
// The token is ASCII letters and digits, so it holds no dot and needs no
// escaping.
func FormatPadTestEndpoint(port int, token string) string {
	return strconv.Itoa(port) + "." + token
}

// ParsePadTestEndpoint reads the value written by FormatPadTestEndpoint. The
// port is 1 to 65535 and the token is 16 to 128 ASCII letters and digits.
func ParsePadTestEndpoint(value string) (port int, token string, err error) {
	portText, token, ok := strings.Cut(value, ".")
	if !ok {
		return 0, "", errors.New("host: pad test endpoint is not <port>.<token>")
	}
	port, err = strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != portText {
		return 0, "", fmt.Errorf("host: pad test port %q is not 1-65535", portText)
	}
	if len(token) < 16 || len(token) > 128 {
		return 0, "", errors.New("host: pad test token must be 16 to 128 characters")
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return 0, "", errors.New("host: pad test token has a character outside letters and digits")
		}
	}
	return port, token, nil
}

// PadTestWebSocketURL returns the only URL a page may dial for port: a
// numeric loopback address and the fixed path.
func PadTestWebSocketURL(port int) string {
	return "ws://127.0.0.1:" + strconv.Itoa(port) + PadTestPath
}
