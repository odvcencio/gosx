package audio

import (
	"errors"
	"testing"

	"m31labs.dev/gosx/game"
)

type fakePanner struct {
	*fakeNode
	pos       [3]float64
	positions int
	model     string
	ref, max  float64
	rolloff   float64
}

func (p *fakePanner) SetPosition(x, y, z float64) error {
	p.pos = [3]float64{x, y, z}
	p.positions++
	return nil
}
func (p *fakePanner) Configure(model string, ref, max, rolloff float64) error {
	p.model, p.ref, p.max, p.rolloff = model, ref, max, rolloff
	return nil
}

type spatialHost struct {
	*fakeHost
	sources                  []*fakeSource
	panners                  []*fakePanner
	listenerPos, fwd, upSeen [3]float64
}

func (h *spatialHost) CreateSource(b Buffer) SourceNode {
	s := h.fakeHost.CreateSource(b).(*fakeSource)
	h.sources = append(h.sources, s)
	return s
}
func (h *spatialHost) CreatePanner() PannerNode {
	p := &fakePanner{fakeNode: &fakeNode{name: "panner"}}
	h.panners = append(h.panners, p)
	return p
}
func (h *spatialHost) SetListener(pos, fwd, up [3]float64) error {
	h.listenerPos, h.fwd, h.upSeen = pos, fwd, up
	return nil
}

func newSFX(t *testing.T, cfg SFXConfig, clips ...string) (*SFX, *spatialHost, *Mixer) {
	t.Helper()
	host := &spatialHost{fakeHost: newFakeHost()}
	mixer := NewMixer(host)
	player := NewPlayer(host, mixer.Graph(), nil)
	for _, id := range clips {
		player.RegisterBuffer(game.AudioClip{ID: game.AssetID(id)}, id)
	}
	return NewSFX(mixer, player, cfg), host, mixer
}

func TestSFXPlaysOnSFXBusByDefault(t *testing.T) {
	sfx, host, mixer := newSFX(t, SFXConfig{}, "hit")
	v, err := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "hit", Volume: 0.5}})
	if err != nil {
		t.Fatal(err)
	}
	src := host.sources[0]
	if len(src.started) != 1 {
		t.Fatal("source not started")
	}
	// source -> gain -> sfx bus
	gainNode := src.connectedTo[0]
	if len(gainNode.connectedTo) != 1 || gainNode.connectedTo[0] != mixer.Bus(SFXBus).gain.(*fakeGain).fakeNode {
		t.Fatal("voice is not routed to the sfx bus")
	}
	if !v.Active() {
		t.Fatal("voice not active")
	}
	src.fire()
	if v.Active() {
		t.Fatal("voice still active after end")
	}
}

func TestSFXReusesPooledNodes(t *testing.T) {
	sfx, host, _ := newSFX(t, SFXConfig{}, "hit")
	for i := 0; i < 50; i++ {
		if _, err := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "hit"}}); err != nil {
			t.Fatal(err)
		}
		host.sources[len(host.sources)-1].fire()
	}
	st := sfx.Stats()
	if st.Created != 1 {
		t.Fatalf("created %d gain chains for 50 sequential plays, want 1", st.Created)
	}
	if st.Pooled != 1 || st.Active != 0 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestSFXPerClipLimitStealsOldest(t *testing.T) {
	sfx, host, _ := newSFX(t, SFXConfig{MaxPerClip: 2}, "shot")
	var vs []*SFXVoice
	for i := 0; i < 3; i++ {
		v, err := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "shot"}})
		if err != nil {
			t.Fatal(err)
		}
		vs = append(vs, v)
	}
	if vs[0].Active() || !vs[1].Active() || !vs[2].Active() {
		t.Fatal("per-clip limit should steal the oldest voice")
	}
	if len(host.sources[0].stopped) != 1 {
		t.Fatal("stolen source was not stopped")
	}
	if sfx.Stats().Active != 2 {
		t.Fatalf("active = %d", sfx.Stats().Active)
	}
}

func TestSFXPriorityProtectsImportantVoices(t *testing.T) {
	sfx, _, _ := newSFX(t, SFXConfig{MaxVoices: 2, MaxPerClip: 9}, "a", "b", "c")
	hi, _ := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "a"}, Priority: 5})
	lo, _ := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "b"}, Priority: 1})

	// A low priority request cannot displace either voice.
	if _, err := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "c"}, Priority: 0}); !errors.Is(err, ErrVoiceDropped) {
		t.Fatalf("err = %v, want ErrVoiceDropped", err)
	}
	// A mid priority request takes the low priority voice, not the high one.
	mid, err := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "c"}, Priority: 3})
	if err != nil {
		t.Fatal(err)
	}
	if lo.Active() || !hi.Active() || !mid.Active() {
		t.Fatal("wrong voice was stolen")
	}
	st := sfx.Stats()
	if st.Dropped != 1 || st.Stolen != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestSFXPositionalVoiceUsesPannerAndFollowsObject(t *testing.T) {
	sfx, host, _ := newSFX(t, SFXConfig{}, "boom")
	pos := game.Vec3{X: 1, Y: 2, Z: 3}
	v, err := sfx.Play(SFXRequest{Playback: game.AudioAt(pos, game.AudioPlayback{Clip: "boom", RefDistance: 2, MaxDistance: 50, RolloffFactor: 1.5})})
	if err != nil {
		t.Fatal(err)
	}
	p := host.panners[0]
	if p.pos != [3]float64{1, 2, 3} || p.ref != 2 || p.max != 50 || p.rolloff != 1.5 {
		t.Fatalf("panner = %+v", p)
	}

	obj := game.Vec3{X: 10}
	alive := true
	v.Follow(func() (game.Vec3, bool) { return obj, alive })
	sfx.Update()
	if p.pos[0] != 10 {
		t.Fatalf("panner x = %v, want 10", p.pos[0])
	}
	obj.X = 20
	sfx.Update()
	if p.pos[0] != 20 {
		t.Fatalf("panner x = %v, want 20", p.pos[0])
	}
	alive = false
	obj.X = 99
	sfx.Update()
	sfx.Update()
	if p.pos[0] != 20 {
		t.Fatalf("voice kept following a dead object: x = %v", p.pos[0])
	}
}

func TestSFXPositionalNeedsSpatialHost(t *testing.T) {
	host := newFakeHost()
	mixer := NewMixer(host)
	player := NewPlayer(host, mixer.Graph(), nil)
	player.RegisterBuffer(game.AudioClip{ID: "x"}, "x")
	sfx := NewSFX(mixer, player, SFXConfig{})
	_, err := sfx.Play(SFXRequest{Playback: game.AudioAt(game.Vec3{}, game.AudioPlayback{Clip: "x"})})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v, want ErrUnsupported", err)
	}
	if err := sfx.SetListener(game.Vec3{}, game.Vec3{Z: -1}, game.Vec3{Y: 1}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("listener err = %v", err)
	}
}

func TestSFXListenerLookAtNormalizesForward(t *testing.T) {
	sfx, host, _ := newSFX(t, SFXConfig{})
	if err := sfx.SetListenerLookAt(game.Vec3{X: 1}, game.Vec3{X: 1, Z: -5}, game.Vec3{Y: 1}); err != nil {
		t.Fatal(err)
	}
	if host.listenerPos != [3]float64{1, 0, 0} || host.fwd != [3]float64{0, 0, -1} || host.upSeen != [3]float64{0, 1, 0} {
		t.Fatalf("listener = %v %v %v", host.listenerPos, host.fwd, host.upSeen)
	}
}

func TestSFXStopAndUnknownClip(t *testing.T) {
	sfx, _, _ := newSFX(t, SFXConfig{}, "a")
	v, _ := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "a"}})
	v.Stop()
	v.Stop()
	if v.Active() || sfx.Stats().Active != 0 {
		t.Fatal("voice not stopped")
	}
	if _, err := sfx.Play(SFXRequest{Playback: game.AudioPlayback{Clip: "missing"}}); err == nil {
		t.Fatal("expected error for unknown clip")
	}
	// Stop() is not counted as a steal.
	if sfx.Stats().Stolen != 0 {
		t.Fatalf("stolen = %d", sfx.Stats().Stolen)
	}
}
