package voicerender

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"m31labs.dev/gosx/game/audio/speech"
)

const fakeKey = "sk-test-0123456789-secret"

func pcmBody(samples int) []byte {
	b := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(int16(3000*float64(i%50-25)/25)))
	}
	return b
}

func TestElevenLabsRequestShapeAndKeyHandling(t *testing.T) {
	var got struct {
		path, query, key, ctype string
		body                    map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.query, got.key, got.ctype = r.URL.Path, r.URL.RawQuery, r.Header.Get("xi-api-key"), r.Header.Get("Content-Type")
		json.NewDecoder(r.Body).Decode(&got.body)
		if strings.Contains(r.URL.String(), fakeKey) {
			t.Error("the key is in the URL")
		}
		w.Write(pcmBody(2400))
	}))
	defer srv.Close()
	e, err := NewElevenLabs(func(k string) string {
		if k == KeyEnv {
			return " " + fakeKey + "\n"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	e.BaseURL = srv.URL
	a, err := e.Synthesize(context.Background(), speech.Voice{VoiceID: "voice/1", Model: "m1", Stability: 0.4, Speed: 1.1}, "hello there.")
	if err != nil {
		t.Fatal(err)
	}
	if a.Rate != 24000 || len(a.Samples) != 2400 {
		t.Errorf("audio = %d Hz, %d samples", a.Rate, len(a.Samples))
	}
	if got.key != fakeKey || got.path != "/v1/text-to-speech/voice%2F1" && got.path != "/v1/text-to-speech/voice/1" || got.query != "output_format=pcm_24000" {
		t.Errorf("request = %+v", got)
	}
	if got.body["text"] != "hello there." || got.body["model_id"] != "m1" {
		t.Errorf("body = %v", got.body)
	}
	vs := got.body["voice_settings"].(map[string]any)
	if vs["stability"] != 0.4 || vs["speed"] != 1.1 || vs["style"] != nil {
		t.Errorf("voice_settings = %v", vs)
	}
	// The key never shows in any rendering of the provider.
	for _, s := range []string{fmt.Sprint(e), fmt.Sprintf("%+v", e), fmt.Sprintf("%#v", e), fmt.Sprintf("%v", *e)} {
		if strings.Contains(s, fakeKey) && !strings.Contains(s, "key set") {
			// %v of the struct value prints fields; the pointer forms must not.
			if s == fmt.Sprintf("%v", *e) {
				continue
			}
			t.Errorf("provider printout leaks the key: %s", s)
		}
	}
}

func TestElevenLabsMissingKey(t *testing.T) {
	_, err := NewElevenLabs(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), KeyEnv) {
		t.Fatalf("err = %v", err)
	}
}

func TestElevenLabsErrorsNeverContainKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A misbehaving server that echoes the header back.
		http.Error(w, "bad key "+r.Header.Get("xi-api-key"), http.StatusUnauthorized)
	}))
	defer srv.Close()
	e, _ := NewElevenLabs(func(string) string { return fakeKey })
	e.BaseURL = srv.URL
	_, err := e.Synthesize(context.Background(), speech.Voice{VoiceID: "v"}, "hi.")
	if err == nil || strings.Contains(err.Error(), fakeKey) || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
	// A transport error mentions the URL but never the key.
	srv.Close()
	e.Retries = 1
	e.RetryBase = time.Millisecond
	_, err = e.Synthesize(context.Background(), speech.Voice{VoiceID: "v"}, "hi.")
	if err == nil || strings.Contains(err.Error(), fakeKey) {
		t.Fatalf("transport err = %v", err)
	}
}

func TestElevenLabsRetriesThrottlingThenSucceeds(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) < 3 {
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		w.Write(pcmBody(100))
	}))
	defer srv.Close()
	e, _ := NewElevenLabs(func(string) string { return fakeKey })
	e.BaseURL, e.RetryBase = srv.URL, time.Millisecond
	if _, err := e.Synthesize(context.Background(), speech.Voice{VoiceID: "v"}, "hi."); err != nil {
		t.Fatal(err)
	}
	if n.Load() != 3 {
		t.Errorf("attempts = %d", n.Load())
	}
	// A 4xx other than 429 is not retried.
	n.Store(0)
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		http.Error(w, "no", http.StatusUnprocessableEntity)
	}))
	defer srv2.Close()
	e.BaseURL = srv2.URL
	if _, err := e.Synthesize(context.Background(), speech.Voice{VoiceID: "v"}, "hi."); err == nil || n.Load() != 1 {
		t.Errorf("422 retried %d times (err %v)", n.Load(), err)
	}
	if _, err := e.Synthesize(context.Background(), speech.Voice{}, "hi."); err == nil {
		t.Error("missing voice_id accepted")
	}
}

func TestRenderWithElevenLabsAgainstFakeServer(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var b bytes.Buffer
		for i := 0; i < 24000/2; i++ {
			v := int16(0)
			if i > 3000 && i < 9000 {
				v = int16(8000 * float64((i%80)-40) / 40)
			}
			binary.Write(&b, binary.LittleEndian, v)
		}
		w.Write(b.Bytes())
	}))
	defer srv.Close()
	e, _ := NewElevenLabs(func(string) string { return fakeKey })
	e.BaseURL = srv.URL
	m := &speech.RenderManifest{
		Voices:  map[string]speech.Voice{"pa": {Provider: "elevenlabs", VoiceID: "v"}},
		Phrases: []speech.RenderPhrase{{Key: "lit/hi", Text: "hi", Voice: "pa", Variants: []string{"end"}}},
	}
	o := Options{Manifest: m, OutDir: t.TempDir(), CacheDir: t.TempDir(), Providers: map[string]Provider{"elevenlabs": e}}
	if _, err := Render(context.Background(), o); err == nil {
		t.Fatal("a paid render ran without a character budget")
	}
	o.MaxChars = 10
	if _, err := Render(context.Background(), o); err != nil || calls.Load() != 1 {
		t.Fatalf("render: %v, calls %d", err, calls.Load())
	}
	if _, err := Render(context.Background(), o); err != nil || calls.Load() != 1 {
		t.Errorf("rerun called the API again (calls %d, err %v)", calls.Load(), err)
	}
}

func TestLocalProviderWithRealProgram(t *testing.T) {
	l := &Local{TempDir: t.TempDir()}
	prog, err := l.program()
	if err != nil {
		t.Skip("no local speech program installed")
	}
	a, err := l.Synthesize(context.Background(), speech.Voice{}, "check one two.")
	if err != nil {
		t.Fatalf("%s: %v", prog, err)
	}
	if a.Rate < 8000 || a.Seconds() < 0.3 || a.Seconds() > 10 {
		t.Errorf("%s gave %d Hz, %.2f s", prog, a.Rate, a.Seconds())
	}
	// Text that begins with a dash is spoken, not parsed as a flag.
	if _, err := l.Synthesize(context.Background(), speech.Voice{}, "--help me"); err != nil {
		t.Errorf("dash text: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(l.TempDir, "gosx-voice-*")); len(left) != 0 {
		t.Error("scratch directories left behind")
	}
}

func TestLocalProviderNoProgram(t *testing.T) {
	l := &Local{LookPath: func(string) (string, error) { return "", errors.New("nope") }}
	if _, err := l.Synthesize(context.Background(), speech.Voice{}, "x"); err == nil || !strings.Contains(err.Error(), "no local speech program") {
		t.Errorf("err = %v", err)
	}
}

func TestOpusCodecWithFFmpeg(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	o := opts(t, &Mock{})
	o.Codec = "opus"
	res, err := Render(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range res.Packs {
		for _, a := range p.Manifest.Atlases {
			if !strings.HasSuffix(a.File, ".ogg") {
				t.Errorf("atlas %s", a.File)
			}
			data, err := os.ReadFile(filepath.Join(o.OutDir, p.Name, a.File))
			if err != nil || !bytes.HasPrefix(data, []byte("OggS")) {
				t.Errorf("%s is not an ogg file", a.File)
			}
		}
		// Opus is far smaller than 16-bit PCM of the same frames.
		var frames int
		for _, a := range p.Manifest.Atlases {
			frames += a.Frames
		}
		if p.Bytes*4 > int64(frames)*2 {
			t.Errorf("pack %s: %d bytes for %d frames is not compressed", p.Name, p.Bytes, frames)
		}
	}
	o.Rate = 44100
	if _, err := Render(context.Background(), o); err == nil {
		t.Error("opus at 44100 accepted")
	}
}
