package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/game/audio/speech"
)

const voiceTestGrammar = `{
  "pools": {"driver": ["okafor", "vance"]},
  "phrases": [
    {"id": "lead", "event": "lead", "variants": [{"say": "{driver} takes the lead"}]}
  ]
}`

func runVoice(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := runVoiceCommand(context.Background(), args, &out)
	return out.String(), err
}

func TestVoiceCommandsEndToEndWithMockProvider(t *testing.T) {
	dir := t.TempDir()
	gpath := filepath.Join(dir, "grammar.json")
	os.WriteFile(gpath, []byte(voiceTestGrammar), 0o644)

	frag, err := runVoice(t, "fragments", "--grammar", gpath, "--voice", "pa", "--pack", "pa")
	if err != nil {
		t.Fatal(err)
	}
	var phrases struct {
		Phrases []speech.RenderPhrase `json:"phrases"`
	}
	if err := json.Unmarshal([]byte(frag), &phrases); err != nil || len(phrases.Phrases) != 3 {
		t.Fatalf("fragments: %v\n%s", err, frag)
	}

	m := speech.RenderManifest{
		Voices:  map[string]speech.Voice{"pa": {Provider: "mock", VoiceID: "x"}},
		Packs:   map[string]speech.PackSpec{"pa": {BudgetBytes: 2_000_000}},
		Phrases: phrases.Phrases,
	}
	mpath := filepath.Join(dir, "manifest.json")
	b, _ := json.Marshal(m)
	os.WriteFile(mpath, b, 0o644)

	est, err := runVoice(t, "estimate", "--manifest", mpath, "--price-per-1k", "0.3")
	if err != nil || !strings.Contains(est, "total") {
		t.Fatalf("estimate: %v\n%s", err, est)
	}

	out := filepath.Join(dir, "out")
	res, err := runVoice(t, "render", "--manifest", mpath, "--out", out, "--cache", filepath.Join(dir, "cache"), "--quiet")
	if err != nil {
		t.Fatalf("render: %v\n%s", err, res)
	}
	if !strings.Contains(res, "pack pa") || !strings.Contains(res, "9 provider calls") {
		t.Errorf("render output:\n%s", res)
	}
	res, err = runVoice(t, "render", "--manifest", mpath, "--out", out, "--cache", filepath.Join(dir, "cache"), "--quiet")
	if err != nil || !strings.Contains(res, "0 provider calls, 9 cache hits") {
		t.Errorf("rerun: %v\n%s", err, res)
	}
	if _, err := os.Stat(filepath.Join(out, "speech.packs.json")); err != nil {
		t.Error(err)
	}

	cov, err := runVoice(t, "coverage", "--manifest", mpath, "--takes", filepath.Join(dir, "takes"), "--strict")
	if err == nil || !strings.Contains(cov, "0 of 9 recorded") {
		t.Errorf("strict coverage should fail on an empty takes dir: %v\n%s", err, cov)
	}
}

func TestVoiceCommandErrors(t *testing.T) {
	if _, err := runVoice(t, "bogus"); err == nil {
		t.Error("unknown subcommand accepted")
	}
	if _, err := runVoice(t, "render"); err == nil {
		t.Error("render without a manifest accepted")
	}
	if out, err := runVoice(t); err != nil || !strings.Contains(out, "gosx voice") {
		t.Errorf("usage: %v", err)
	}
	dir := t.TempDir()
	mpath := filepath.Join(dir, "m.json")
	os.WriteFile(mpath, []byte(`{"voices":{"pa":{"provider":"elevenlabs","voice_id":"v"}},"phrases":[{"key":"lit/hi","text":"hi","voice":"pa"}]}`), 0o644)
	t.Setenv("ELEVENLABS_API_KEY", "")
	_, err := runVoice(t, "render", "--manifest", mpath, "--out", filepath.Join(dir, "o"))
	if err == nil || !strings.Contains(err.Error(), "ELEVENLABS_API_KEY") {
		t.Errorf("missing key: %v", err)
	}
}
