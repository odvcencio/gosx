// Package signal defines the WebRTC signaling messages that netplay peers
// exchange over a GoSX hub WebSocket.
//
// The browser sends an offer and the relay answers it. Both sides then trade
// ICE candidates. Each message is one hub text frame whose envelope is
// {"event": <Event...>, "data": <message>}. The package uses only the standard
// library, so browser (GOOS=js) code, the relay and tests share one definition.
//
// Every inbound message is untrusted. Parse bounds its size, decodes it and
// validates every field before it returns a value.
package signal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Hub event names.
const (
	EventHello     = "netplay.hello"     // client -> relay: ask for ICE config
	EventConfig    = "netplay.config"    // relay -> client: ICE servers and timing
	EventOffer     = "netplay.offer"     // client -> relay
	EventAnswer    = "netplay.answer"    // relay -> client
	EventCandidate = "netplay.candidate" // both directions
	EventFailure   = "netplay.failure"   // relay -> client: WebRTC will not work
)

// Size and count limits. They keep a hostile peer from making the relay hold
// or parse large inputs.
const (
	MaxMessageBytes   = 24 * 1024 // whole data payload of one event
	MaxSDPBytes       = 16 * 1024
	MaxSDPLines       = 256
	MaxCandidateBytes = 1024
	MaxMidLen         = 64
	MaxFragmentLen    = 256
	MaxSessionLen     = 64
	MaxMLineIndex     = 255
	MaxICEServers     = 8
	MaxICEURLs        = 4
	MaxICEURLLen      = 256
	MaxCredentialLen  = 512
	MaxMessageText    = 200
	MaxFallbackMs     = 60_000
)

// Failure codes.
const (
	CodeUnavailable = "unavailable" // relay has no WebRTC for this peer
	CodeBadRequest  = "bad-request"
	CodeBusy        = "busy"
	CodeInternal    = "internal"
)

// ErrInvalid wraps every validation failure.
var ErrInvalid = errors.New("netplay/signal: invalid message")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Message is one signaling message.
type Message interface {
	// Event returns the hub event name that carries the message.
	Event() string
	// Validate checks every field against the package limits.
	Validate() error
}

// Hello asks the relay for ICE configuration for a session.
type Hello struct {
	Session string `json:"session"`
}

// ICEServer is one STUN or TURN server.
type ICEServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// Config is the relay's reply to Hello.
type Config struct {
	Session    string      `json:"session"`
	ICEServers []ICEServer `json:"iceServers,omitempty"`
	// FallbackAfterMs is how long the client waits for the data channel to open
	// before it uses the WebSocket fallback. Zero lets the client choose.
	FallbackAfterMs int `json:"fallbackAfterMs,omitempty"`
}

// Offer carries the browser's session description.
type Offer struct {
	Session string `json:"session"`
	SDP     string `json:"sdp"`
}

// Answer carries the relay's session description.
type Answer struct {
	Session string `json:"session"`
	SDP     string `json:"sdp"`
}

// Candidate carries one ICE candidate. An empty Candidate marks the end of
// candidates.
type Candidate struct {
	Session          string `json:"session"`
	Candidate        string `json:"candidate"`
	SDPMid           string `json:"sdpMid,omitempty"`
	SDPMLineIndex    int    `json:"sdpMLineIndex"`
	UsernameFragment string `json:"usernameFragment,omitempty"`
}

// Failure tells the client that the relay cannot serve WebRTC for the session,
// so it should switch to the fallback now instead of waiting for the timeout.
type Failure struct {
	Session string `json:"session"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

func (*Hello) Event() string     { return EventHello }
func (*Config) Event() string    { return EventConfig }
func (*Offer) Event() string     { return EventOffer }
func (*Answer) Event() string    { return EventAnswer }
func (*Candidate) Event() string { return EventCandidate }
func (*Failure) Event() string   { return EventFailure }

// Parse decodes and validates the data of one hub event. It returns a pointer
// to the message type for event, or an error that wraps ErrInvalid.
func Parse(event string, data []byte) (Message, error) {
	if len(data) > MaxMessageBytes {
		return nil, invalid("%s payload is %d bytes, limit %d", event, len(data), MaxMessageBytes)
	}
	var m Message
	switch event {
	case EventHello:
		m = new(Hello)
	case EventConfig:
		m = new(Config)
	case EventOffer:
		m = new(Offer)
	case EventAnswer:
		m = new(Answer)
	case EventCandidate:
		m = new(Candidate)
	case EventFailure:
		m = new(Failure)
	default:
		return nil, invalid("unknown event %q", event)
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, invalid("%s: %v", event, err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return m, nil
}

// Marshal validates m and returns its event name and JSON data.
func Marshal(m Message) (event string, data []byte, err error) {
	if err := m.Validate(); err != nil {
		return "", nil, err
	}
	data, err = json.Marshal(m)
	if err != nil {
		return "", nil, err
	}
	if len(data) > MaxMessageBytes {
		return "", nil, invalid("%s payload is %d bytes, limit %d", m.Event(), len(data), MaxMessageBytes)
	}
	return m.Event(), data, nil
}

func checkSession(s string) error {
	if s == "" || len(s) > MaxSessionLen {
		return invalid("session must be 1 to %d bytes", MaxSessionLen)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.'
		if !ok {
			return invalid("session has a character outside [A-Za-z0-9._-]")
		}
	}
	return nil
}

func checkText(name, s string, max int, allowNewline bool) error {
	if len(s) > max {
		return invalid("%s is %d bytes, limit %d", name, len(s), max)
	}
	if !utf8.ValidString(s) {
		return invalid("%s is not valid UTF-8", name)
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0 || c < 0x20 && !(allowNewline && (c == '\r' || c == '\n')) || c == 0x7f {
			return invalid("%s has a control character", name)
		}
	}
	return nil
}

func checkSDP(sdp string) error {
	if sdp == "" {
		return invalid("sdp is empty")
	}
	if err := checkText("sdp", sdp, MaxSDPBytes, true); err != nil {
		return err
	}
	if !strings.HasPrefix(sdp, "v=0") {
		return invalid("sdp must start with v=0")
	}
	if n := bytes.Count([]byte(sdp), []byte("\n")); n > MaxSDPLines {
		return invalid("sdp has %d lines, limit %d", n, MaxSDPLines)
	}
	return nil
}

// Validate implements Message.
func (m *Hello) Validate() error { return checkSession(m.Session) }

// Validate implements Message.
func (m *Config) Validate() error {
	if err := checkSession(m.Session); err != nil {
		return err
	}
	if m.FallbackAfterMs < 0 || m.FallbackAfterMs > MaxFallbackMs {
		return invalid("fallbackAfterMs must be 0 to %d", MaxFallbackMs)
	}
	if len(m.ICEServers) > MaxICEServers {
		return invalid("%d ICE servers, limit %d", len(m.ICEServers), MaxICEServers)
	}
	for i := range m.ICEServers {
		if err := m.ICEServers[i].Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks one ICE server entry.
func (s *ICEServer) Validate() error {
	if len(s.URLs) == 0 || len(s.URLs) > MaxICEURLs {
		return invalid("an ICE server needs 1 to %d urls", MaxICEURLs)
	}
	for _, u := range s.URLs {
		if err := checkText("ICE url", u, MaxICEURLLen, false); err != nil {
			return err
		}
		if !strings.HasPrefix(u, "stun:") && !strings.HasPrefix(u, "stuns:") &&
			!strings.HasPrefix(u, "turn:") && !strings.HasPrefix(u, "turns:") {
			return invalid("ICE url scheme must be stun, stuns, turn or turns")
		}
	}
	if err := checkText("ICE username", s.Username, MaxCredentialLen, false); err != nil {
		return err
	}
	return checkText("ICE credential", s.Credential, MaxCredentialLen, false)
}

// Validate implements Message.
func (m *Offer) Validate() error {
	if err := checkSession(m.Session); err != nil {
		return err
	}
	return checkSDP(m.SDP)
}

// Validate implements Message.
func (m *Answer) Validate() error {
	if err := checkSession(m.Session); err != nil {
		return err
	}
	return checkSDP(m.SDP)
}

// Validate implements Message.
func (m *Candidate) Validate() error {
	if err := checkSession(m.Session); err != nil {
		return err
	}
	if m.Candidate != "" && !strings.HasPrefix(m.Candidate, "candidate:") {
		return invalid("candidate must start with \"candidate:\"")
	}
	if err := checkText("candidate", m.Candidate, MaxCandidateBytes, false); err != nil {
		return err
	}
	if err := checkText("sdpMid", m.SDPMid, MaxMidLen, false); err != nil {
		return err
	}
	if err := checkText("usernameFragment", m.UsernameFragment, MaxFragmentLen, false); err != nil {
		return err
	}
	if m.SDPMLineIndex < 0 || m.SDPMLineIndex > MaxMLineIndex {
		return invalid("sdpMLineIndex must be 0 to %d", MaxMLineIndex)
	}
	return nil
}

// Validate implements Message.
func (m *Failure) Validate() error {
	if err := checkSession(m.Session); err != nil {
		return err
	}
	switch m.Code {
	case CodeUnavailable, CodeBadRequest, CodeBusy, CodeInternal:
	default:
		return invalid("unknown failure code %q", m.Code)
	}
	return checkText("message", m.Message, MaxMessageText, false)
}
