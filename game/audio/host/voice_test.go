package audiohost

import "testing"

type voiceTestNode struct{ disconnects int }

func (*voiceTestNode) Connect(Node)  {}
func (n *voiceTestNode) Disconnect() { n.disconnects++ }

type voiceTestSource struct {
	voiceTestNode
	ended func()
	stops []float64
}

func (*voiceTestSource) Start(float64)       {}
func (s *voiceTestSource) Stop(at float64)   { s.stops = append(s.stops, at) }
func (s *voiceTestSource) OnEnded(fn func()) { s.ended = fn }

func TestVoiceKeepsScheduledTailAndReleasesOnce(t *testing.T) {
	source, gain, pan := &voiceTestSource{}, &voiceTestNode{}, &voiceTestNode{}
	ends := 0
	voice := NewVoice(source, func() { ends++ }, gain, pan)
	voice.Stop(12.59)
	if source.ended == nil || source.disconnects != 0 || len(source.stops) != 1 || source.stops[0] != 12.59 {
		t.Fatal("scheduled fade tail destroyed")
	}
	source.ended()
	voice.Close()
	voice.Stop(20)
	if source.ended != nil || source.disconnects != 1 || gain.disconnects != 1 || pan.disconnects != 1 || ends != 1 || len(source.stops) != 1 {
		t.Fatal("voice lifetime not released exactly once")
	}
}

func TestVoiceCloseWithoutEnded(t *testing.T) {
	source, gain := &voiceTestSource{}, &voiceTestNode{}
	voice := NewVoice(source, nil, gain)
	voice.Stop(0)
	voice.Close()
	voice.Close()
	if source.ended != nil || source.disconnects != 1 || gain.disconnects != 1 {
		t.Fatal("suspended source retained resources")
	}
}
