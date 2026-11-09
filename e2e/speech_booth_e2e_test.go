//go:build e2e

package e2e

import (
	"github.com/chromedp/chromedp"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
	"m31labs.dev/gosx/game/audio/speech/voicerender"
)

// The recording booth in a real browser, with Chrome's fake microphone:
// getUserMedia, the AudioWorklet recorder, the take upload and the file on
// disk.
func TestRecordingBoothRecordsATake(t *testing.T) {
	chrome := e2eChromePath(t)
	m := &speech.RenderManifest{
		Voices: map[string]speech.Voice{"booth": {}},
		Phrases: []speech.RenderPhrase{
			{Key: "lit/green flag", Text: "green flag", Voice: "booth", Variants: []string{"start", "end"}},
		},
	}
	takes := &voicerender.Takes{Dir: t.TempDir()}
	b, err := voicerender.NewBooth(m, takes, "booth", nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(b.Handler())
	defer server.Close()

	// A synthetic microphone: a tone from an oscillator, delivered through
	// getUserMedia, so the test does not depend on a capture device.
	const fakeMic = `navigator.mediaDevices.getUserMedia = async () => {
	  const c = new AudioContext(); const d = c.createMediaStreamDestination();
	  const o = c.createOscillator(); o.frequency.value = 220; o.connect(d); o.start();
	  return d.stream;
	};`
	page := newBrowserPage(t, chrome, map[string]any{
		"autoplay-policy": "no-user-gesture-required",
	}, 900, 700, fakeMic, 60*time.Second)
	page.navigate(t, server.URL+"/?token="+b.Token()+"&voice=booth")
	page.waitFor(t, `document.getElementById('line').textContent === 'green flag'`, 10*time.Second, "the first prompt")
	var variant string
	page.eval(t, `document.getElementById('variant').textContent`, &variant)
	if variant != "start" {
		t.Errorf("first variant = %q, want start", variant)
	}
	// A trusted click: browsers start audio only after a real user gesture.
	if err := chromedp.Run(page.ctx, chromedp.Click("#record", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !page.pollFor(`document.getElementById('status').textContent === 'Recording...'`, 15*time.Second) {
		var st string
		page.eval(t, `document.getElementById('status').textContent`, &st)
		t.Fatalf("recording did not start; status %q; console: %s; errors: %v", st, page.Console(), page.PageErrors())
	}
	time.Sleep(900 * time.Millisecond)
	if err := chromedp.Run(page.ctx, chromedp.Click("#record", chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	page.waitFor(t, `document.getElementById('status').textContent.startsWith('Saved')`, 10*time.Second, "the take to save")

	path := filepath.Join(takes.Dir, "booth", "lit_green_flag.start.wav")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("take not on disk: %v", err)
	}
	a, err := loudness.ParseWAV(data)
	if err != nil {
		t.Fatal(err)
	}
	if a.Rate < 8000 || a.Seconds() < 0.5 {
		t.Errorf("take is %d Hz, %.2f s", a.Rate, a.Seconds())
	}
	// Skip moves on without a take.
	page.eval(t, `document.getElementById('next').click(), true`, new(bool))
	var next string
	page.eval(t, `document.getElementById('variant').textContent`, &next)
	if next != "end" {
		t.Errorf("after next the variant is %q, want end", next)
	}
	if errs := page.PageErrors(); len(errs) > 0 {
		t.Errorf("page errors: %v", errs)
	}
	if got := takes.Coverage(m); got.Have != 1 || got.Total != 2 {
		t.Errorf("coverage %d of %d", got.Have, got.Total)
	}
}
