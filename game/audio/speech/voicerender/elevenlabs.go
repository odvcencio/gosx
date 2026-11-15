package voicerender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// KeyEnv is the only place the ElevenLabs key is read from.
const KeyEnv = "ELEVENLABS_API_KEY"

// ElevenLabs is the ElevenLabs text-to-speech provider. It reads its key from
// the ELEVENLABS_API_KEY environment variable when constructed, holds it only
// in memory, sends it only in the xi-api-key header, and scrubs it from every
// error message. The shipped game never contains it.
type ElevenLabs struct {
	// BaseURL defaults to https://api.elevenlabs.io. Tests point it at httptest.
	BaseURL string
	// Format is the output_format query value. Default pcm_24000, raw signed
	// 16-bit little-endian mono, which needs no decoder.
	Format string
	// Client defaults to a client with a 60 s timeout.
	Client *http.Client
	// Retries is how many times to retry 429 and 5xx answers. Default 3.
	Retries int
	// RetryBase is the first backoff, doubled each retry. Default 1 s.
	RetryBase time.Duration

	key string
}

// NewElevenLabs reads the key through getenv (os.Getenv in the tool). It
// fails, without naming any value, when the variable is empty.
func NewElevenLabs(getenv func(string) string) (*ElevenLabs, error) {
	key := strings.TrimSpace(getenv(KeyEnv))
	if key == "" {
		return nil, fmt.Errorf("voicerender: %s is not set; export it in your shell (it is never read from a file)", KeyEnv)
	}
	return &ElevenLabs{key: key}, nil
}

// Name implements Provider.
func (e *ElevenLabs) Name() string { return "elevenlabs" }

// Paid implements Paid.
func (e *ElevenLabs) Paid() bool { return true }

// String never includes the key.
func (e *ElevenLabs) String() string { return "elevenlabs(key set)" }

// GoString never includes the key.
func (e *ElevenLabs) GoString() string { return e.String() }

type elSettings struct {
	Stability  *float64 `json:"stability,omitempty"`
	Similarity *float64 `json:"similarity_boost,omitempty"`
	Style      *float64 `json:"style,omitempty"`
	Speed      *float64 `json:"speed,omitempty"`
}

type elBody struct {
	Text     string      `json:"text"`
	Model    string      `json:"model_id,omitempty"`
	Settings *elSettings `json:"voice_settings,omitempty"`
}

// Synthesize implements Provider.
func (e *ElevenLabs) Synthesize(ctx context.Context, v speech.Voice, text string) (loudness.Audio, error) {
	if v.VoiceID == "" {
		return loudness.Audio{}, errors.New("voicerender: elevenlabs voice needs a voice_id")
	}
	format := e.Format
	if format == "" {
		format = "pcm_24000"
	}
	var rate int
	if _, err := fmt.Sscanf(format, "pcm_%d", &rate); err != nil || rate <= 0 {
		return loudness.Audio{}, fmt.Errorf("voicerender: elevenlabs format %q is not a pcm_<rate> format", format)
	}
	body := elBody{Text: text, Model: v.Model}
	if body.Model == "" {
		body.Model = "eleven_multilingual_v2"
	}
	opt := func(x float64) *float64 {
		if x == 0 {
			return nil
		}
		return &x
	}
	if v.Stability != 0 || v.Similarity != 0 || v.Style != 0 || v.Speed != 0 {
		body.Settings = &elSettings{Stability: opt(v.Stability), Similarity: opt(v.Similarity), Style: opt(v.Style), Speed: opt(v.Speed)}
	}
	payload, _ := json.Marshal(body)
	base := e.BaseURL
	if base == "" {
		base = "https://api.elevenlabs.io"
	}
	endpoint := strings.TrimRight(base, "/") + "/v1/text-to-speech/" + url.PathEscape(v.VoiceID) + "?output_format=" + url.QueryEscape(format)
	client := e.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	retries, backoff := e.Retries, e.RetryBase
	if retries == 0 {
		retries = 3
	}
	if backoff == 0 {
		backoff = time.Second
	}
	var lastErr error
	for attempt := 0; attempt <= retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return loudness.Audio{}, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return loudness.Audio{}, e.scrub(err)
		}
		req.Header.Set("xi-api-key", e.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "audio/*")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = e.scrub(err)
			if ctx.Err() != nil {
				return loudness.Audio{}, ctx.Err()
			}
			continue
		}
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK && readErr == nil:
			if len(data) < 2 {
				return loudness.Audio{}, errors.New("voicerender: elevenlabs returned no audio")
			}
			return loudness.PCM16(data, rate), nil
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("voicerender: elevenlabs answered %d: %s", resp.StatusCode, e.snippet(data))
		case resp.StatusCode != http.StatusOK:
			return loudness.Audio{}, fmt.Errorf("voicerender: elevenlabs answered %d: %s", resp.StatusCode, e.snippet(data))
		default:
			lastErr = e.scrub(readErr)
		}
	}
	return loudness.Audio{}, lastErr
}

func (e *ElevenLabs) snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return strings.ReplaceAll(s, e.key, "[redacted]")
}

// scrub removes the key from an error's text.
func (e *ElevenLabs) scrub(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, e.key) {
		return err
	}
	return errors.New(strings.ReplaceAll(msg, e.key, "[redacted]"))
}
