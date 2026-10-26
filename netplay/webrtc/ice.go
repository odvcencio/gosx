package webrtc

import (
	pion "github.com/pion/webrtc/v4"

	"m31labs.dev/gosx/netplay/signal"
)

// pionServers converts signaling ICE servers to pion's type.
func pionServers(in []signal.ICEServer) []pion.ICEServer {
	out := make([]pion.ICEServer, 0, len(in))
	for _, s := range in {
		out = append(out, pion.ICEServer{URLs: append([]string(nil), s.URLs...), Username: s.Username, Credential: s.Credential})
	}
	return out
}

// newAPI builds a pion API. configure may adjust the setting engine (port
// range, NAT mapping, loopback candidates in tests); it may be nil.
func newAPI(configure func(*pion.SettingEngine)) *pion.API {
	var se pion.SettingEngine
	if configure != nil {
		configure(&se)
	}
	return pion.NewAPI(pion.WithSettingEngine(se))
}

func candidateMessage(session string, c *pion.ICECandidate) *signal.Candidate {
	if c == nil {
		return &signal.Candidate{Session: session} // end of candidates
	}
	init := c.ToJSON()
	m := &signal.Candidate{Session: session, Candidate: init.Candidate, UsernameFragment: ""}
	if init.SDPMid != nil {
		m.SDPMid = *init.SDPMid
	}
	if init.SDPMLineIndex != nil {
		m.SDPMLineIndex = int(*init.SDPMLineIndex)
	}
	return m
}

func candidateInit(m *signal.Candidate) pion.ICECandidateInit {
	mid, idx := m.SDPMid, uint16(m.SDPMLineIndex)
	init := pion.ICECandidateInit{Candidate: m.Candidate, SDPMid: &mid, SDPMLineIndex: &idx}
	if m.UsernameFragment != "" {
		uf := m.UsernameFragment
		init.UsernameFragment = &uf
	}
	return init
}
