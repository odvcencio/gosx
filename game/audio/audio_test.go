package audio

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"m31labs.dev/gosx/game"
)

// fakeNode/fakeGain/fakeCompressor/fakeSource/fakeHost implement Host purely
// in memory, so BusGraph and Player's wiring and bookkeeping are testable
// without a browser.

type fakeNode struct {
	name        string
	connectedTo []*fakeNode
	disconnects int
}

func (n *fakeNode) Connect(dst Node) {
	if raw, ok := dst.(interface{ raw() *fakeNode }); ok {
		n.connectedTo = append(n.connectedTo, raw.raw())
	}
}
func (n *fakeNode) Disconnect()    { n.disconnects++ }
func (n *fakeNode) raw() *fakeNode { return n }

type fakeGain struct {
	*fakeNode
	gain float64
}

func (g *fakeGain) SetGain(value float64) { g.gain = value }

type fakeCompressor struct {
	*fakeNode
	threshold, ratio, attack, release float64
}

func (c *fakeCompressor) Configure(threshold, ratio, attack, release float64) {
	c.threshold, c.ratio, c.attack, c.release = threshold, ratio, attack, release
}

type fakeSource struct {
	*fakeNode
	buffer  Buffer
	loop    bool
	rate    float64
	started []float64
	stopped []float64
	onEnded func()
}

func (s *fakeSource) SetLoop(loop bool)            { s.loop = loop }
func (s *fakeSource) SetPlaybackRate(rate float64) { s.rate = rate }
func (s *fakeSource) Start(at float64)             { s.started = append(s.started, at) }
func (s *fakeSource) Stop(at float64) {
	s.stopped = append(s.stopped, at)
	if s.onEnded != nil {
		fn := s.onEnded
		s.onEnded = nil
		fn()
	}
}
func (s *fakeSource) OnEnded(fn func()) { s.onEnded = fn }

// fire simulates the buffer reaching its natural end.
func (s *fakeSource) fire() {
	if s.onEnded != nil {
		fn := s.onEnded
		s.onEnded = nil
		fn()
	}
}

type fakeHost struct {
	now         float64
	state       string
	resumeCalls int
	resumeErr   error
	decodeErr   error
	nodes       int
	lastSource  *fakeSource
	destination *fakeNode
}

func newFakeHost() *fakeHost {
	return &fakeHost{state: "suspended", destination: &fakeNode{name: "destination"}}
}

func (h *fakeHost) CurrentTime() float64 { return h.now }
func (h *fakeHost) State() string        { return h.state }
func (h *fakeHost) Resume() error {
	h.resumeCalls++
	if h.resumeErr != nil {
		return h.resumeErr
	}
	h.state = "running"
	return nil
}
func (h *fakeHost) CreateGain() GainNode {
	h.nodes++
	return &fakeGain{fakeNode: &fakeNode{name: "gain"}, gain: 1}
}
func (h *fakeHost) CreateCompressor() CompressorNode {
	h.nodes++
	return &fakeCompressor{fakeNode: &fakeNode{name: "compressor"}}
}
func (h *fakeHost) CreateSource(buffer Buffer) SourceNode {
	h.nodes++
	s := &fakeSource{fakeNode: &fakeNode{name: "source"}, buffer: buffer, rate: 1}
	h.lastSource = s
	return s
}
func (h *fakeHost) Destination() Node { return h.destination }
func (h *fakeHost) DecodeAudioData(data []byte) (Buffer, error) {
	if h.decodeErr != nil {
		return nil, h.decodeErr
	}
	return string(data), nil
}

func TestBusGraphConnectsMasterThroughCompressorToDestination(t *testing.T) {
	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{MasterVolume: 0.8})
	master := graph.Master().(*fakeGain)
	if master.gain != 0.8 {
		t.Fatalf("master gain = %v, want 0.8", master.gain)
	}
	if len(master.connectedTo) != 1 {
		t.Fatalf("master connections = %d, want 1 (compressor)", len(master.connectedTo))
	}
}

func TestBusGraphChildBusConnectsToParentBus(t *testing.T) {
	host := newFakeHost()
	manifest := game.AudioManifest{
		Buses: []game.AudioBus{
			{ID: "music", Volume: 0.5},
			{ID: "music.layer", Parent: "music", Volume: 0.9},
		},
	}
	graph := NewBusGraph(host, manifest)
	music := graph.Bus("music")
	layer := graph.Bus("music.layer")
	if music == nil || layer == nil {
		t.Fatal("expected both buses to exist")
	}
	if music.Volume() != 0.5 {
		t.Fatalf("music volume = %v, want 0.5", music.Volume())
	}
	layerGain := layer.gain.(*fakeGain)
	if len(layerGain.connectedTo) != 1 || layerGain.connectedTo[0] != music.gain.(*fakeGain).fakeNode {
		t.Fatalf("expected music.layer to connect directly to music's gain node")
	}
}

func TestBusGraphUnknownParentFallsBackToMaster(t *testing.T) {
	host := newFakeHost()
	manifest := game.AudioManifest{Buses: []game.AudioBus{{ID: "sfx", Parent: "does-not-exist"}}}
	graph := NewBusGraph(host, manifest)
	sfx := graph.Bus("sfx")
	sfxGain := sfx.gain.(*fakeGain)
	masterNode := graph.Master().(*fakeGain).fakeNode
	if len(sfxGain.connectedTo) != 1 || sfxGain.connectedTo[0] != masterNode {
		t.Fatal("expected sfx to fall back to connecting to master")
	}
}

func TestBusSetVolumeAndSetMutedInteract(t *testing.T) {
	host := newFakeHost()
	manifest := game.AudioManifest{Buses: []game.AudioBus{{ID: "sfx", Volume: 0.6}}}
	graph := NewBusGraph(host, manifest)
	bus := graph.Bus("sfx")
	gain := bus.gain.(*fakeGain)
	if gain.gain != 0.6 {
		t.Fatalf("initial gain = %v, want 0.6", gain.gain)
	}
	bus.SetMuted(true)
	if gain.gain != 0 {
		t.Fatalf("muted gain = %v, want 0", gain.gain)
	}
	bus.SetVolume(0.9)
	if gain.gain != 0 {
		t.Fatalf("gain while still muted = %v, want 0", gain.gain)
	}
	bus.SetMuted(false)
	if gain.gain != 0.9 {
		t.Fatalf("unmuted gain = %v, want 0.9 (remembered volume)", gain.gain)
	}
}

func TestPlayerLoadManifestPreloadsAndDecodesOnce(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte("clip-bytes"))
	}))
	defer server.Close()

	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{})
	player := NewPlayer(host, graph, HTTPLoader{})

	manifest := game.AudioManifest{Clips: []game.AudioClip{
		{ID: "boom", URI: server.URL, Preload: true},
	}}
	if err := player.LoadManifest(manifest); err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}

	if _, err := player.Play(game.AudioPlayback{Clip: "boom"}); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests after Play = %d, want 1 (buffer should be cached)", requests)
	}
}

func TestPlayerPlayUnknownClipErrors(t *testing.T) {
	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{})
	player := NewPlayer(host, graph, HTTPLoader{})
	if _, err := player.Play(game.AudioPlayback{Clip: "missing"}); err == nil {
		t.Fatal("expected an error for an unregistered clip")
	}
}

func TestPlayerPlayWithoutLoaderErrorsGracefully(t *testing.T) {
	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{})
	player := NewPlayer(host, graph, nil)
	// Register the clip without a decoded buffer: ensureBuffer must then
	// fall through to the (nil) loader and report ErrNoLoader instead of
	// panicking.
	player.clips["needs-loader"] = game.AudioClip{ID: "needs-loader", URI: "http://example.invalid/clip.wav"}
	if _, err := player.Play(game.AudioPlayback{Clip: "needs-loader"}); !errors.Is(err, ErrNoLoader) {
		t.Fatalf("Play err = %v, want ErrNoLoader", err)
	}
}

func TestPlayerPlayConnectsThroughNamedBus(t *testing.T) {
	host := newFakeHost()
	manifest := game.AudioManifest{Buses: []game.AudioBus{{ID: "sfx", Volume: 1}}}
	graph := NewBusGraph(host, manifest)
	player := NewPlayer(host, graph, nil)
	player.RegisterBuffer(game.AudioClip{ID: "hit", Bus: "sfx"}, "decoded-hit")

	handle, err := player.Play(game.AudioPlayback{Clip: "hit"})
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if handle == "" {
		t.Fatal("expected a non-empty handle")
	}
	if host.lastSource.buffer != "decoded-hit" {
		t.Fatalf("source buffer = %v, want decoded-hit", host.lastSource.buffer)
	}
	if len(host.lastSource.started) != 1 {
		t.Fatalf("expected Start to be called once, got %d", len(host.lastSource.started))
	}
	if player.Playing() != 1 {
		t.Fatalf("Playing() = %d, want 1", player.Playing())
	}
}

func TestPlayerVoiceCleansUpOnEnded(t *testing.T) {
	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{})
	player := NewPlayer(host, graph, nil)
	player.RegisterBuffer(game.AudioClip{ID: "hit"}, "buf")
	if _, err := player.Play(game.AudioPlayback{Clip: "hit"}); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if player.Playing() != 1 {
		t.Fatalf("Playing() before end = %d, want 1", player.Playing())
	}
	host.lastSource.fire()
	if player.Playing() != 0 {
		t.Fatalf("Playing() after natural end = %d, want 0", player.Playing())
	}
	if host.lastSource.disconnects == 0 {
		t.Fatal("expected the source to disconnect on end")
	}
}

func TestPlayerStopEndsVoiceAndRemovesIt(t *testing.T) {
	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{})
	player := NewPlayer(host, graph, nil)
	player.RegisterBuffer(game.AudioClip{ID: "hit"}, "buf")
	handle, _ := player.Play(game.AudioPlayback{Clip: "hit"})
	player.Stop(handle)
	if player.Playing() != 0 {
		t.Fatalf("Playing() after Stop = %d, want 0", player.Playing())
	}
	if len(host.lastSource.stopped) != 1 {
		t.Fatalf("expected Stop to be called on the source once, got %d", len(host.lastSource.stopped))
	}
}

func TestPlayerStopAllStopsEveryVoice(t *testing.T) {
	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{})
	player := NewPlayer(host, graph, nil)
	player.RegisterBuffer(game.AudioClip{ID: "hit"}, "buf")
	for i := 0; i < 3; i++ {
		if _, err := player.Play(game.AudioPlayback{Clip: "hit"}); err != nil {
			t.Fatalf("Play %d: %v", i, err)
		}
	}
	if player.Playing() != 3 {
		t.Fatalf("Playing() = %d, want 3", player.Playing())
	}
	player.StopAll()
	if player.Playing() != 0 {
		t.Fatalf("Playing() after StopAll = %d, want 0", player.Playing())
	}
}

func TestPlayerUnlockResumesOnlyWhenSuspended(t *testing.T) {
	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{})
	player := NewPlayer(host, graph, nil)

	if err := player.Unlock(); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if host.resumeCalls != 1 {
		t.Fatalf("resumeCalls = %d, want 1", host.resumeCalls)
	}
	// Already running: Unlock must not call Resume again.
	if err := player.Unlock(); err != nil {
		t.Fatalf("second Unlock: %v", err)
	}
	if host.resumeCalls != 1 {
		t.Fatalf("resumeCalls after second Unlock = %d, want 1", host.resumeCalls)
	}
}

func TestPlaybackVolumeAndRateFallBackToClipDefaults(t *testing.T) {
	host := newFakeHost()
	graph := NewBusGraph(host, game.AudioManifest{})
	player := NewPlayer(host, graph, nil)
	player.RegisterBuffer(game.AudioClip{ID: "hit", Volume: 0.4, Rate: 1.5}, "buf")

	if _, err := player.Play(game.AudioPlayback{Clip: "hit"}); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if host.lastSource.rate != 1.5 {
		t.Fatalf("rate = %v, want 1.5 from clip default", host.lastSource.rate)
	}

	if _, err := player.Play(game.AudioPlayback{Clip: "hit", Rate: 2}); err != nil {
		t.Fatalf("Play with explicit rate: %v", err)
	}
	if host.lastSource.rate != 2 {
		t.Fatalf("rate = %v, want 2 (playback override)", host.lastSource.rate)
	}
}

func TestHTTPLoaderNonOKStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	if _, err := (HTTPLoader{}).Load(server.URL); err == nil {
		t.Fatal("expected a non-200 status to error")
	}
}
