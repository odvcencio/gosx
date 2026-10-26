package signal

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const goodSDP = "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\n"

func TestRoundTrip(t *testing.T) {
	msgs := []Message{
		&Hello{Session: "match-1"},
		&Config{Session: "match-1", ICEServers: []ICEServer{
			{URLs: []string{"stun:stun.example.org:3478"}},
			{URLs: []string{"turn:turn.example.org:3478?transport=udp"}, Username: "u", Credential: "c"},
		}, FallbackAfterMs: 4000},
		&Offer{Session: "match-1", SDP: goodSDP},
		&Answer{Session: "match-1", SDP: goodSDP},
		&Candidate{Session: "match-1", Candidate: "candidate:1 1 udp 2130706431 10.0.0.1 5000 typ host", SDPMid: "0", SDPMLineIndex: 0, UsernameFragment: "abcd"},
		&Candidate{Session: "match-1"}, // end of candidates
		&Failure{Session: "match-1", Code: CodeUnavailable, Message: "relay full"},
	}
	for _, m := range msgs {
		ev, data, err := Marshal(m)
		if err != nil {
			t.Fatalf("%T marshal: %v", m, err)
		}
		if ev != m.Event() {
			t.Fatalf("%T event %q want %q", m, ev, m.Event())
		}
		got, err := Parse(ev, data)
		if err != nil {
			t.Fatalf("%T parse %s: %v", m, data, err)
		}
		a, _ := json.Marshal(m)
		b, _ := json.Marshal(got)
		if !bytes.Equal(a, b) {
			t.Fatalf("%T round trip: %s != %s", m, a, b)
		}
	}
}

func TestParseRejects(t *testing.T) {
	big := strings.Repeat("a=x\r\n", MaxSDPBytes/5+10)
	cases := []struct {
		name, event, data string
	}{
		{"unknown event", "netplay.nope", `{}`},
		{"not json", EventOffer, `nope`},
		{"array", EventOffer, `[]`},
		{"empty session", EventOffer, `{"session":"","sdp":"v=0\r\n"}`},
		{"bad session chars", EventOffer, `{"session":"a b","sdp":"v=0\r\n"}`},
		{"long session", EventOffer, `{"session":"` + strings.Repeat("a", MaxSessionLen+1) + `","sdp":"v=0\r\n"}`},
		{"sdp missing v=0", EventOffer, `{"session":"s","sdp":"hello"}`},
		{"sdp empty", EventAnswer, `{"session":"s","sdp":""}`},
		{"sdp too big", EventOffer, mustJSON(&Offer{Session: "s", SDP: "v=0\r\n" + big})},
		{"sdp nul", EventOffer, `{"session":"s","sdp":"v=0\r\n\u0000"}`},
		{"candidate no prefix", EventCandidate, `{"session":"s","candidate":"hello"}`},
		{"candidate newline", EventCandidate, `{"session":"s","candidate":"candidate:1\r\nx"}`},
		{"candidate too long", EventCandidate, mustJSON(&Candidate{Session: "s", Candidate: "candidate:" + strings.Repeat("1", MaxCandidateBytes)})},
		{"mline negative", EventCandidate, `{"session":"s","candidate":"candidate:1","sdpMLineIndex":-1}`},
		{"mline huge", EventCandidate, `{"session":"s","candidate":"candidate:1","sdpMLineIndex":99999}`},
		{"mid too long", EventCandidate, mustJSON(&Candidate{Session: "s", Candidate: "candidate:1", SDPMid: strings.Repeat("m", MaxMidLen+1)})},
		{"config no scheme", EventConfig, `{"session":"s","iceServers":[{"urls":["http://x"]}]}`},
		{"config no urls", EventConfig, `{"session":"s","iceServers":[{"urls":[]}]}`},
		{"config too many servers", EventConfig, mustJSON(&Config{Session: "s", ICEServers: manyServers(MaxICEServers + 1)})},
		{"config long credential", EventConfig, mustJSON(&Config{Session: "s", ICEServers: []ICEServer{{URLs: []string{"turn:x"}, Credential: strings.Repeat("c", MaxCredentialLen+1)}}})},
		{"config negative timeout", EventConfig, `{"session":"s","fallbackAfterMs":-1}`},
		{"failure bad code", EventFailure, `{"session":"s","code":"boom"}`},
		{"failure long message", EventFailure, mustJSON(&Failure{Session: "s", Code: CodeInternal, Message: strings.Repeat("m", MaxMessageText+1)})},
		{"oversize envelope", EventHello, `{"session":"s","pad":"` + strings.Repeat("p", MaxMessageBytes) + `"}`},
	}
	for _, c := range cases {
		if m, err := Parse(c.event, []byte(c.data)); err == nil {
			t.Errorf("%s: accepted as %#v", c.name, m)
		} else if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error %v does not wrap ErrInvalid", c.name, err)
		}
	}
}

func TestMarshalValidates(t *testing.T) {
	if _, _, err := Marshal(&Offer{Session: "s", SDP: "junk"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Marshal accepted invalid offer: %v", err)
	}
}

func TestUnknownFieldsIgnored(t *testing.T) {
	m, err := Parse(EventHello, []byte(`{"session":"s","future":1}`))
	if err != nil || m.(*Hello).Session != "s" {
		t.Fatalf("got %v %v", m, err)
	}
}

func FuzzParse(f *testing.F) {
	f.Add(EventOffer, []byte(`{"session":"s","sdp":"v=0\r\n"}`))
	f.Add(EventCandidate, []byte(`{"session":"s","candidate":"candidate:1"}`))
	f.Add(EventConfig, []byte(`{"session":"s","iceServers":[{"urls":["stun:x"]}]}`))
	f.Fuzz(func(t *testing.T, event string, data []byte) {
		m, err := Parse(event, data)
		if err != nil {
			return
		}
		if err := m.Validate(); err != nil {
			t.Fatalf("Parse accepted %T that fails Validate: %v", m, err)
		}
		if len(data) > MaxMessageBytes {
			t.Fatalf("accepted %d bytes", len(data))
		}
	})
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func manyServers(n int) []ICEServer {
	s := make([]ICEServer, n)
	for i := range s {
		s[i] = ICEServer{URLs: []string{"stun:x"}}
	}
	return s
}
