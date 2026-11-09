package audio

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"m31labs.dev/gosx/game"
)

// ErrVoiceDropped is returned by SFX.Play when every voice slot is held by
// an equal or higher priority sound, so the new sound was not started.
var ErrVoiceDropped = errors.New("audio: sfx voice limit reached")

// SFXConfig sets the pool's limits. A zero field takes its default.
type SFXConfig struct {
	// MaxVoices is the most one-shots playing at once. Default 32.
	MaxVoices int
	// MaxPerClip is the most simultaneous voices of a single clip, so a
	// machine gun does not use every slot. Default 8.
	MaxPerClip int
	// Bus is the bus voices play on unless the request names one. Default sfx.
	Bus string
}

// SFXRequest asks the pool to play one clip once.
type SFXRequest struct {
	// Playback names the clip and its volume, rate, bus and, for 3D audio,
	// Position, RefDistance, MaxDistance and RolloffFactor. Loop is ignored;
	// the pool plays one-shots. A nil Position plays the clip unpositioned.
	Playback game.AudioPlayback
	// Priority ranks the voice. When the pool is full, a new voice steals the
	// lowest priority voice if its priority is at least as high; among equals
	// it steals the oldest. Default 0.
	Priority int
}

// SFXVoice is one playing one-shot.
type SFXVoice struct {
	sfx      *SFX
	clip     game.AssetID
	priority int
	seq      uint64
	source   SourceNode
	chain    *sfxChain
	follow   func() (game.Vec3, bool)
	done     bool
}

// sfxChain is the per-voice gain (and optional panner) the pool reuses.
type sfxChain struct {
	gain   GainNode
	panner PannerNode
	dest   Node
}

// SFX is a pooled one-shot player with voice limits, priorities and optional
// 3D positioning. It decodes clips through a Player, so register clips with
// Player.LoadManifest or Player.RegisterBuffer first. The per-voice gain and
// panner nodes are pooled and reused; only the single-use source nodes the
// browser requires are created per play.
type SFX struct {
	mixer  *Mixer
	player *Player
	cfg    SFXConfig

	mu       sync.Mutex
	voices   []*SFXVoice
	seq      uint64
	freeGain []*sfxChain
	freePan  []*sfxChain
	created  int
	dropped  int
	stolen   int
}

// NewSFX builds a pool that plays through mixer's buses.
func NewSFX(mixer *Mixer, player *Player, cfg SFXConfig) *SFX {
	if cfg.MaxVoices <= 0 {
		cfg.MaxVoices = 32
	}
	if cfg.MaxPerClip <= 0 {
		cfg.MaxPerClip = 8
	}
	if cfg.Bus == "" {
		cfg.Bus = SFXBus
	}
	return &SFX{mixer: mixer, player: player, cfg: cfg}
}

// Play starts one voice. It returns ErrVoiceDropped when the pool is full of
// sounds that outrank the request, and ErrUnsupported when the request has a
// Position and the host cannot position sounds.
func (s *SFX) Play(req SFXRequest) (*SFXVoice, error) {
	pb := req.Playback
	s.player.mu.Lock()
	clip, ok := s.player.clips[pb.Clip]
	s.player.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("audio: unknown clip %q", pb.Clip)
	}
	spatial, _ := s.mixer.host.(SpatialHost)
	if pb.Position != nil && spatial == nil {
		return nil, ErrUnsupported
	}
	buffer, err := s.player.ensureBuffer(clip)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	if !s.admitLocked(pb.Clip, req.Priority) {
		s.dropped++
		s.mu.Unlock()
		return nil, ErrVoiceDropped
	}
	s.seq++
	v := &SFXVoice{sfx: s, clip: pb.Clip, priority: req.Priority, seq: s.seq}
	chain := s.takeChainLocked(pb.Position != nil, spatial)
	v.chain = chain
	s.voices = append(s.voices, v)
	s.mu.Unlock()

	host := s.mixer.host
	source := host.CreateSource(buffer)
	source.SetPlaybackRate(firstNonZero(pb.Rate, clip.Rate, 1))
	source.SetLoop(false)
	chain.gain.SetGain(firstNonZero(pb.Volume, clip.Volume, 1))

	busID := firstNonEmpty(pb.Bus, clip.Bus, s.cfg.Bus)
	dest := s.mixer.Bus(busID)
	if dest == nil {
		dest = s.mixer.Bus(s.cfg.Bus)
	}
	if dest == nil {
		dest = s.mixer.Bus(MasterBus)
	}
	out := dest.Input()
	if chain.panner != nil {
		_ = chain.panner.Configure("", pb.RefDistance, pb.MaxDistance, pb.RolloffFactor)
		_ = chain.panner.SetPosition(pb.Position.X, pb.Position.Y, pb.Position.Z)
		chain.panner.Connect(out)
	} else {
		chain.gain.Connect(out)
	}
	chain.dest = out
	v.source = source
	source.Connect(chain.gain)
	source.OnEnded(func() { v.finish() })
	source.Start(host.CurrentTime())
	return v, nil
}

// admitLocked makes room for one more voice of clip at priority, stealing the
// least important voice if needed. It reports false if nothing may be stolen.
func (s *SFX) admitLocked(clip game.AssetID, priority int) bool {
	// Per-clip limit first, so a flood of one clip cannot evict others.
	count := 0
	for _, v := range s.voices {
		if v.clip == clip {
			count++
		}
	}
	if count >= s.cfg.MaxPerClip {
		victim := s.pickVictimLocked(func(v *SFXVoice) bool { return v.clip == clip }, priority)
		if victim == nil {
			return false
		}
		s.stealLocked(victim)
	}
	if len(s.voices) >= s.cfg.MaxVoices {
		victim := s.pickVictimLocked(func(*SFXVoice) bool { return true }, priority)
		if victim == nil {
			return false
		}
		s.stealLocked(victim)
	}
	return true
}

// pickVictimLocked returns the lowest priority, then oldest, voice that match
// accepts and that priority outranks or equals; nil if none.
func (s *SFX) pickVictimLocked(match func(*SFXVoice) bool, priority int) *SFXVoice {
	var victim *SFXVoice
	for _, v := range s.voices {
		if !match(v) || v.priority > priority {
			continue
		}
		if victim == nil || v.priority < victim.priority || (v.priority == victim.priority && v.seq < victim.seq) {
			victim = v
		}
	}
	return victim
}

// stealLocked releases v now. The caller holds s.mu.
func (s *SFX) stealLocked(v *SFXVoice) {
	s.stolen++
	s.removeLocked(v)
	v.done = true
	src, chain := v.source, v.chain
	v.source, v.chain = nil, nil
	s.mu.Unlock()
	if src != nil {
		src.OnEnded(nil)
		src.Stop(s.mixer.host.CurrentTime())
		src.Disconnect()
	}
	s.mu.Lock()
	s.releaseChainLocked(chain)
}

func (s *SFX) removeLocked(v *SFXVoice) {
	for i, x := range s.voices {
		if x == v {
			s.voices = append(s.voices[:i], s.voices[i+1:]...)
			return
		}
	}
}

func (s *SFX) takeChainLocked(positional bool, spatial SpatialHost) *sfxChain {
	if positional {
		if n := len(s.freePan); n > 0 {
			c := s.freePan[n-1]
			s.freePan = s.freePan[:n-1]
			return c
		}
		g := s.mixer.host.CreateGain()
		p := spatial.CreatePanner()
		g.Connect(p)
		s.created++
		return &sfxChain{gain: g, panner: p}
	}
	if n := len(s.freeGain); n > 0 {
		c := s.freeGain[n-1]
		s.freeGain = s.freeGain[:n-1]
		return c
	}
	s.created++
	return &sfxChain{gain: s.mixer.host.CreateGain()}
}

func (s *SFX) releaseChainLocked(c *sfxChain) {
	if c == nil {
		return
	}
	if c.panner != nil {
		c.panner.Disconnect()
		s.freePan = append(s.freePan, c)
		return
	}
	c.gain.Disconnect()
	s.freeGain = append(s.freeGain, c)
}

// finish runs when v's source ends naturally.
func (v *SFXVoice) finish() {
	s := v.sfx
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.done {
		return
	}
	v.done = true
	s.removeLocked(v)
	src, chain := v.source, v.chain
	v.source, v.chain = nil, nil
	if src != nil {
		src.Disconnect()
	}
	s.releaseChainLocked(chain)
}

// Stop ends the voice now. Stopping a finished voice is a no-op.
func (v *SFXVoice) Stop() {
	if v == nil {
		return
	}
	s := v.sfx
	s.mu.Lock()
	if v.done {
		s.mu.Unlock()
		return
	}
	s.stealLocked(v)
	s.stolen--
	s.mu.Unlock()
}

// Active reports whether the voice is still playing.
func (v *SFXVoice) Active() bool {
	v.sfx.mu.Lock()
	defer v.sfx.mu.Unlock()
	return !v.done
}

// SetVolume changes the voice's own gain.
func (v *SFXVoice) SetVolume(volume float64) {
	v.sfx.mu.Lock()
	chain := v.chain
	v.sfx.mu.Unlock()
	if chain != nil {
		chain.gain.SetGain(volume)
	}
}

// SetPosition moves a positioned voice. It returns ErrUnsupported for a voice
// that was started without a Position.
func (v *SFXVoice) SetPosition(p game.Vec3) error {
	v.sfx.mu.Lock()
	chain := v.chain
	v.sfx.mu.Unlock()
	if chain == nil {
		return nil
	}
	if chain.panner == nil {
		return ErrUnsupported
	}
	return chain.panner.SetPosition(p.X, p.Y, p.Z)
}

// Follow makes the voice track a moving source. SFX.Update calls fn each
// frame and moves the voice to the point it returns; when fn reports false
// (the object is gone) the voice stops following and keeps its last position.
func (v *SFXVoice) Follow(fn func() (game.Vec3, bool)) {
	v.sfx.mu.Lock()
	v.follow = fn
	v.sfx.mu.Unlock()
}

// Update moves every following voice to its object's current position. Call
// it once per frame, before drawing, after the simulation step.
func (s *SFX) Update() {
	s.mu.Lock()
	type move struct {
		v *SFXVoice
		p game.Vec3
	}
	var moves []move
	for _, v := range s.voices {
		fn := v.follow
		if fn == nil {
			continue
		}
		s.mu.Unlock()
		p, ok := fn()
		s.mu.Lock()
		if !ok {
			v.follow = nil
			continue
		}
		moves = append(moves, move{v, p})
	}
	s.mu.Unlock()
	for _, m := range moves {
		_ = m.v.SetPosition(m.p)
	}
}

// SetListener places the shared listener, normally at the camera. Pass the
// camera's world position, forward direction and up vector each frame. It
// returns ErrUnsupported on a host that cannot position sounds.
func (s *SFX) SetListener(position, forward, up game.Vec3) error {
	spatial, ok := s.mixer.host.(SpatialHost)
	if !ok {
		return ErrUnsupported
	}
	return spatial.SetListener(
		[3]float64{position.X, position.Y, position.Z},
		[3]float64{forward.X, forward.Y, forward.Z},
		[3]float64{up.X, up.Y, up.Z},
	)
}

// SetListenerLookAt is SetListener for a camera given by eye and target.
func (s *SFX) SetListenerLookAt(eye, target, up game.Vec3) error {
	f := game.Vec3{X: target.X - eye.X, Y: target.Y - eye.Y, Z: target.Z - eye.Z}
	if n := math.Sqrt(f.X*f.X + f.Y*f.Y + f.Z*f.Z); n > 0 {
		f = game.Vec3{X: f.X / n, Y: f.Y / n, Z: f.Z / n}
	} else {
		f = game.Vec3{Z: -1}
	}
	return s.SetListener(eye, f, up)
}

// SFXStats counts pool activity.
type SFXStats struct {
	Active  int // voices playing now
	Pooled  int // idle pooled chains
	Created int // chains ever created
	Dropped int // plays refused by the limits
	Stolen  int // voices cut off to make room
}

// Stats returns a snapshot of pool counters.
func (s *SFX) Stats() SFXStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SFXStats{Active: len(s.voices), Pooled: len(s.freeGain) + len(s.freePan), Created: s.created, Dropped: s.dropped, Stolen: s.stolen}
}

// StopAll ends every voice.
func (s *SFX) StopAll() {
	s.mu.Lock()
	voices := append([]*SFXVoice(nil), s.voices...)
	s.mu.Unlock()
	for _, v := range voices {
		v.Stop()
	}
}
