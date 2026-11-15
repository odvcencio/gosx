package voicerender

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
)

//go:embed boothui/index.html
var boothIndex []byte

//go:embed boothui/booth.js
var boothJS []byte

//go:embed boothui/recorder-worklet.js
var boothWorklet []byte

const maxTakeBytes = 32 << 20

// Booth is a recording prompter: a small web page that walks a speaker
// through a render manifest one line at a time and stores each take in Takes.
// It records in the browser with getUserMedia and an AudioWorklet, with
// echo cancellation, noise suppression and automatic gain turned off.
//
// The server is for one person on one machine: Serve binds a loopback
// address, every API call must carry the random token, and the process runs
// only as long as the command that started it.
type Booth struct {
	Manifest *speech.RenderManifest
	Takes    *Takes
	// Voice is the speaker being recorded; empty means the only voice.
	Voice string
	// Reference, when set, speaks each line on request so the performer can
	// hear the intended phrasing. Any Provider works (the local one is free).
	Reference Provider
	token     string
}

// NewBooth validates the manifest and picks a token.
func NewBooth(m *speech.RenderManifest, t *Takes, voice string, ref Provider) (*Booth, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if voice == "" {
		if len(m.Voices) != 1 {
			return nil, errBoothVoice
		}
		for n := range m.Voices {
			voice = n
		}
	}
	if _, ok := m.Voices[voice]; !ok {
		return nil, errBoothVoice
	}
	var tok [16]byte
	if _, err := rand.Read(tok[:]); err != nil {
		return nil, err
	}
	return &Booth{Manifest: m, Takes: t, Voice: voice, Reference: ref, token: hex.EncodeToString(tok[:])}, nil
}

type boothError string

func (e boothError) Error() string { return string(e) }

const errBoothVoice = boothError("voicerender: name a voice from the manifest (--voice)")

// Token is the secret the page must send. Put it in the URL you open.
func (b *Booth) Token() string { return b.token }

// Listen opens a loopback listener on port (0 picks a free one).
func (b *Booth) Listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

func (b *Booth) requests() []speech.Request {
	var out []speech.Request
	for _, r := range b.Manifest.Requests() {
		if r.Voice == b.Voice {
			out = append(out, r)
		}
	}
	return out
}

// Handler serves the page and its API.
func (b *Booth) Handler() http.Handler {
	mux := http.NewServeMux()
	static := func(ctype string, body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Cache-Control", "no-store")
			w.Write(body)
		}
	}
	mux.HandleFunc("GET /{$}", static("text/html; charset=utf-8", boothIndex))
	mux.HandleFunc("GET /booth.js", static("text/javascript; charset=utf-8", boothJS))
	mux.HandleFunc("GET /recorder-worklet.js", static("text/javascript; charset=utf-8", boothWorklet))
	mux.HandleFunc("GET /api/state", b.guard(b.state))
	mux.HandleFunc("GET /api/ref", b.guard(b.ref))
	mux.HandleFunc("POST /api/take", b.guard(b.take))
	return mux
}

func (b *Booth) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Booth-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(b.token)) != 1 {
			http.Error(w, "bad token", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

type boothLine struct {
	Index   int    `json:"index"`
	Key     string `json:"key"`
	Variant string `json:"variant"`
	Text    string `json:"text"`
	Pack    string `json:"pack"`
	Have    bool   `json:"have"`
}

func (b *Booth) state(w http.ResponseWriter, r *http.Request) {
	reqs := b.requests()
	lines := make([]boothLine, len(reqs))
	for i, q := range reqs {
		// The reader performs the phrase text, not the render template (which
		// only adds punctuation for a text-to-speech voice).
		text := q.Text
		for _, p := range b.Manifest.Phrases {
			if p.Key == q.Key {
				text = p.Text
			}
		}
		lines[i] = boothLine{Index: i, Key: q.Key, Variant: q.Variant, Text: text, Pack: q.Pack, Have: b.Takes.Has(q)}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"voice": b.Voice, "lines": lines, "reference": b.Reference != nil})
}

func (b *Booth) lookup(r *http.Request) (speech.Request, bool) {
	i, err := strconv.Atoi(r.URL.Query().Get("index"))
	reqs := b.requests()
	if err != nil || i < 0 || i >= len(reqs) {
		return speech.Request{}, false
	}
	return reqs[i], true
}

func (b *Booth) ref(w http.ResponseWriter, r *http.Request) {
	q, ok := b.lookup(r)
	if !ok || b.Reference == nil {
		http.NotFound(w, r)
		return
	}
	a, err := b.Reference.Synthesize(r.Context(), b.Manifest.Voices[b.Voice], q.Text)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Write(loudness.WAV16(a))
}

func (b *Booth) take(w http.ResponseWriter, r *http.Request) {
	q, ok := b.lookup(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTakeBytes))
	if err != nil {
		http.Error(w, "take too large or unreadable", http.StatusRequestEntityTooLarge)
		return
	}
	a, err := loudness.ParseWAV(body)
	if err != nil || len(a.Samples) == 0 {
		http.Error(w, "the take is not a usable WAV", http.StatusBadRequest)
		return
	}
	if err := b.Takes.Save(q, a); err != nil {
		http.Error(w, "could not save the take", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
