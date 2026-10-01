package audio

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"m31labs.dev/gosx/game"
)

// ErrNoLoader is returned when a clip needs decoding but the Player has no
// ClipLoader configured.
var ErrNoLoader = errors.New("audio: no ClipLoader configured")

// ClipLoader fetches a clip's raw encoded bytes by URI.
type ClipLoader interface {
	Load(uri string) ([]byte, error)
}

// HTTPLoader loads clip bytes with net/http.Get. It works unmodified under
// GOOS=js/GOARCH=wasm, where the standard library's RoundTripper for that
// platform is implemented on top of the browser's fetch API.
type HTTPLoader struct {
	// Client selects the HTTP client. A nil Client uses http.DefaultClient.
	Client *http.Client
}

// Load implements ClipLoader.
func (l HTTPLoader) Load(uri string) ([]byte, error) {
	client := l.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(uri)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("audio: GET %s: status %d", uri, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

type activeVoice struct {
	source SourceNode
	gain   GainNode
}

// Player registers a game.AudioManifest's clips and buses, decodes clip
// bytes on demand (or eagerly for Preload clips via LoadManifest), and plays
// them through a BusGraph.
type Player struct {
	host   Host
	graph  *BusGraph
	loader ClipLoader

	mu         sync.Mutex
	clips      map[game.AssetID]game.AudioClip
	buffers    map[game.AssetID]Buffer
	playing    map[string]activeVoice
	nextHandle uint64
}

// NewPlayer creates a Player that decodes clips through host and plays them
// through graph. loader may be nil if every clip will be registered through
// RegisterBuffer instead of loaded by URI.
func NewPlayer(host Host, graph *BusGraph, loader ClipLoader) *Player {
	return &Player{
		host:    host,
		graph:   graph,
		loader:  loader,
		clips:   make(map[game.AssetID]game.AudioClip),
		buffers: make(map[game.AssetID]Buffer),
		playing: make(map[string]activeVoice),
	}
}

// LoadManifest registers every clip in manifest and decodes every clip
// marked Preload before returning. Call it from a goroutine if the caller
// does not want to block on decoding.
func (p *Player) LoadManifest(manifest game.AudioManifest) error {
	var firstErr error
	for _, clip := range manifest.Clips {
		p.mu.Lock()
		p.clips[clip.ID] = clip
		p.mu.Unlock()
		if clip.Preload {
			if _, err := p.ensureBuffer(clip); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// RegisterBuffer registers an already-decoded buffer for clip directly,
// skipping ClipLoader/DecodeAudioData. Useful for procedurally generated
// clips or tests.
func (p *Player) RegisterBuffer(clip game.AudioClip, buffer Buffer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clips[clip.ID] = clip
	p.buffers[clip.ID] = buffer
}

func (p *Player) ensureBuffer(clip game.AudioClip) (Buffer, error) {
	p.mu.Lock()
	if buffer, ok := p.buffers[clip.ID]; ok {
		p.mu.Unlock()
		return buffer, nil
	}
	p.mu.Unlock()
	if p.loader == nil {
		return nil, ErrNoLoader
	}
	data, err := p.loader.Load(clip.URI)
	if err != nil {
		return nil, err
	}
	buffer, err := p.host.DecodeAudioData(data)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.buffers[clip.ID] = buffer
	p.mu.Unlock()
	return buffer, nil
}

// Play starts playback immediately (at the host's current audio-clock time)
// and returns a handle Stop can use.
func (p *Player) Play(playback game.AudioPlayback) (string, error) {
	return p.ScheduleAt(playback, p.host.CurrentTime())
}

// ScheduleAt starts playback at host audio-clock time at, in the same units
// as Host.CurrentTime — useful for sample-accurate scheduling of the next
// note or beat ahead of when it needs to be audible.
func (p *Player) ScheduleAt(playback game.AudioPlayback, at float64) (string, error) {
	p.mu.Lock()
	clip, ok := p.clips[playback.Clip]
	p.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("audio: unknown clip %q", playback.Clip)
	}
	buffer, err := p.ensureBuffer(clip)
	if err != nil {
		return "", err
	}

	source := p.host.CreateSource(buffer)
	source.SetPlaybackRate(firstNonZero(playback.Rate, clip.Rate, 1))
	loop := playback.Loop || clip.Loop
	source.SetLoop(loop)

	gain := p.host.CreateGain()
	gain.SetGain(firstNonZero(playback.Volume, clip.Volume, 1))

	var dest Node = p.graph.Master()
	if busID := firstNonEmpty(playback.Bus, clip.Bus); busID != "" {
		if bus := p.graph.Bus(busID); bus != nil {
			dest = bus.gain
		}
	}
	source.Connect(gain)
	gain.Connect(dest)

	handle := playback.Handle
	if handle == "" {
		p.mu.Lock()
		p.nextHandle++
		handle = fmt.Sprintf("voice-%d", p.nextHandle)
		p.mu.Unlock()
	}

	p.mu.Lock()
	p.playing[handle] = activeVoice{source: source, gain: gain}
	p.mu.Unlock()

	source.OnEnded(func() {
		p.mu.Lock()
		delete(p.playing, handle)
		p.mu.Unlock()
		source.Disconnect()
		gain.Disconnect()
	})

	source.Start(at)
	return handle, nil
}

// Stop ends playback for handle immediately. Stopping an unknown or already-
// finished handle is a no-op.
func (p *Player) Stop(handle string) {
	p.mu.Lock()
	voice, ok := p.playing[handle]
	p.mu.Unlock()
	if !ok {
		return
	}
	voice.source.Stop(p.host.CurrentTime())
}

// StopAll ends every currently playing voice.
func (p *Player) StopAll() {
	p.mu.Lock()
	handles := make([]string, 0, len(p.playing))
	for handle := range p.playing {
		handles = append(handles, handle)
	}
	p.mu.Unlock()
	for _, handle := range handles {
		p.Stop(handle)
	}
}

// Playing reports how many voices are currently active.
func (p *Player) Playing() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.playing)
}

// Unlock resumes a suspended host audio context. Call it from inside a user
// gesture handler (a click or key handler) — every modern browser starts an
// AudioContext suspended until one fires.
func (p *Player) Unlock() error {
	if p.host.State() != "suspended" {
		return nil
	}
	return p.host.Resume()
}

func firstNonZero(values ...float64) float64 {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
