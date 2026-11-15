// Package voicerender turns a speech.RenderManifest into voice clips: it asks
// a text-to-speech Provider for each phrase variant, trims silence,
// normalizes loudness, packs the clips into one atlas per voice and writes
// the clip manifest the runtime loads. A content-hash cache makes reruns
// free.
//
// This package belongs to build tooling. It uses net/http and os/exec and must
// not be imported by a game's js/wasm build; the runtime side is package
// speech and speech/voice.
package voicerender

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// Provider turns text into speech. Implementations must be safe for
// concurrent use.
type Provider interface {
	// Name identifies the provider in manifests, caches and logs.
	Name() string
	// Synthesize speaks text with voice and returns mono PCM at the
	// provider's native rate.
	Synthesize(ctx context.Context, v speech.Voice, text string) (loudness.Audio, error)
}

// Paid is implemented by providers that bill per call. The renderer refuses
// to spend without an explicit character budget.
type Paid interface{ Paid() bool }

func isPaid(p Provider) bool {
	pp, ok := p.(Paid)
	return ok && pp.Paid()
}

// fingerprint is the cache key for one synthesis: provider, every voice
// setting that changes the audio, and the text. Credentials never enter it.
func fingerprint(provider string, v speech.Voice, text string) string {
	v.TargetLUFS = nil // a loudness target is applied after the cache
	v.Provider = ""
	b, _ := json.Marshal(v)
	h := sha256.New()
	fmt.Fprintf(h, "gosx-voice/1\x00%s\x00%s\x00%s", provider, b, text)
	return hex.EncodeToString(h.Sum(nil))
}

// Mock is a deterministic provider for tests and CI. It speaks no words: it
// returns a tone whose length follows the text length and whose pitch follows
// the text hash and its final punctuation, with silence on both ends so trim
// has work to do. It never touches the network.
type Mock struct {
	Rate int // default 24000
	// Calls counts Synthesize calls, so tests can prove a cache hit made none.
	Calls atomic.Int64
	// Fail, when set, makes Synthesize return it for texts containing the key.
	Fail map[string]error
}

// Name implements Provider.
func (m *Mock) Name() string { return "mock" }

// Synthesize implements Provider.
func (m *Mock) Synthesize(ctx context.Context, v speech.Voice, text string) (loudness.Audio, error) {
	m.Calls.Add(1)
	if err := ctx.Err(); err != nil {
		return loudness.Audio{}, err
	}
	for k, err := range m.Fail {
		if len(k) > 0 && contains(text, k) {
			return loudness.Audio{}, err
		}
	}
	rate := m.Rate
	if rate == 0 {
		rate = 24000
	}
	sum := sha256.Sum256([]byte(v.VoiceID + "|" + text))
	base := 160 + float64(sum[0])
	runes := []rune(text)
	body := 0.02 * float64(len(runes))
	lead, tail := int(0.12*float64(rate)), int(0.2*float64(rate))
	n := int(body*float64(rate)) + 1
	out := loudness.Audio{Rate: rate, Samples: make([]float64, lead+n+tail)}
	end := runes[len(runes)-1]
	glide := 0.0
	switch end {
	case '.':
		glide = -0.25
	case ',':
		glide = 0.15
	}
	phase := 0.0
	amp := 0.25 * (1 + float64(sum[1]%8)/16)
	for i := 0; i < n; i++ {
		x := float64(i) / float64(n)
		f := base * (1 + glide*x)
		phase += 2 * math.Pi * f / float64(rate)
		env := math.Min(1, math.Min(x*20, (1-x)*20))
		out.Samples[lead+i] = amp * env * math.Sin(phase)
	}
	return out, nil
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ErrNoProvider reports a voice whose provider was not configured.
type ErrNoProvider struct{ Voice, Provider string }

func (e ErrNoProvider) Error() string {
	return fmt.Sprintf("voicerender: voice %q needs provider %q, which is not available", e.Voice, e.Provider)
}
