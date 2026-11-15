package voicerender

import (
	"bytes"
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// tone is a stand-in for a spoken line: a tone of the given length with the
// mic noise floor, DC offset and a pop that a real recording would carry.
func tone(rate int, hz, amp, seconds float64) loudness.Audio {
	n := int(seconds * float64(rate))
	a := loudness.Audio{Rate: rate, Samples: make([]float64, n)}
	for i := range a.Samples {
		env := math.Min(1, math.Min(float64(i)/200, float64(n-i)/200))
		a.Samples[i] = amp * env * math.Sin(2*math.Pi*hz*float64(i)/float64(rate))
	}
	return a
}

func withSilence(parts ...loudness.Audio) loudness.Audio {
	out := loudness.Audio{Rate: parts[0].Rate}
	for _, p := range parts {
		out.Samples = append(out.Samples, p.Samples...)
	}
	return out
}

func quiet(rate int, seconds float64) loudness.Audio {
	return loudness.Audio{Rate: rate, Samples: make([]float64, int(seconds*float64(rate)))}
}

func takesManifest() *speech.RenderManifest {
	m := &speech.RenderManifest{
		Voices: map[string]speech.Voice{"booth": {Provider: "takes"}},
		Packs:  map[string]speech.PackSpec{"pa": {BudgetBytes: 400_000}},
	}
	for _, p := range []struct{ key, text string }{
		{"lit/green flag", "green flag"}, {"lit/into turn one", "into turn one"}, {"num/seven", "seven"}, {"lit/and the checkers fall", "and the checkers fall"},
	} {
		m.Phrases = append(m.Phrases, speech.RenderPhrase{Key: p.key, Text: p.text, Voice: "booth", Pack: "pa", Variants: []string{"mid", "end"}})
	}
	return m
}

func TestTakesRenderThroughSamePipeline(t *testing.T) {
	m := takesManifest()
	tk := &Takes{Dir: t.TempDir()}
	for i, r := range m.Requests() {
		a := tone(44100, 200+float64(i)*20, 0.05+0.05*float64(i%3), 0.4)
		a = withSilence(quiet(44100, 0.3), a, quiet(44100, 0.5))
		for j := range a.Samples { // DC offset a cheap interface leaves, over the whole recording
			a.Samples[j] += 0.03
		}
		if err := tk.Save(r, a); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "out")
	res, err := Render(context.Background(), Options{Manifest: m, OutDir: out, Providers: map[string]Provider{"takes": tk}, TargetLUFS: -20, CeilingDBTP: -1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Calls != 0 || len(res.Missing) != 0 {
		t.Errorf("calls %d missing %v", res.Calls, res.Missing)
	}
	pk := res.Packs[0]
	raw, _ := os.ReadFile(filepath.Join(out, "pa", pk.Manifest.Atlases[0].File))
	atlas, _ := loudness.ParseWAV(raw)
	for _, c := range pk.Manifest.Clips {
		clip := loudness.Audio{Rate: atlas.Rate, Samples: atlas.Samples[c.Offset : c.Offset+c.Frames]}
		rep := loudness.Measure(clip)
		if math.Abs(rep.LUFS+20) > 0.6 || rep.TruePeakDB > -0.9 {
			t.Errorf("%s/%s: %.2f LUFS %.2f dBTP", c.Key, c.Variant, rep.LUFS, rep.TruePeakDB)
		}
		if clip.Rate != 48000 || float64(c.Frames)/48000 > 0.45 {
			t.Errorf("%s/%s: %d frames; silence was not trimmed", c.Key, c.Variant, c.Frames)
		}
		var sum float64
		for _, v := range clip.Samples {
			sum += v
		}
		if math.Abs(sum/float64(len(clip.Samples))) > 0.005 {
			t.Errorf("%s/%s keeps a DC offset", c.Key, c.Variant)
		}
	}
}

func TestMissingTakesBlockOrAllowPartial(t *testing.T) {
	m := takesManifest()
	tk := &Takes{Dir: t.TempDir()}
	reqs := m.Requests()
	for _, r := range reqs[:3] {
		tk.Save(r, withSilence(quiet(48000, 0.1), tone(48000, 300, 0.2, 0.3), quiet(48000, 0.1)))
	}
	o := Options{Manifest: m, OutDir: filepath.Join(t.TempDir(), "o"), Providers: map[string]Provider{"takes": tk}}
	if _, err := Render(context.Background(), o); err == nil || !strings.Contains(err.Error(), "not recorded") {
		t.Fatalf("strict render: %v", err)
	}
	o.AllowMissing = true
	res, err := Render(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Missing) != len(reqs)-3 || len(res.Packs[0].Manifest.Clips) != 3 {
		t.Errorf("missing %d, clips %d", len(res.Missing), len(res.Packs[0].Manifest.Clips))
	}
}

func TestSharedReadServesEveryVariant(t *testing.T) {
	m := takesManifest()
	tk := &Takes{Dir: t.TempDir()}
	dir := filepath.Join(t.TempDir(), "daw")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "Lit_Green_Flag.WAV"), loudness.WAV16(tone(44100, 300, 0.3, 0.3)), 0o644)
	os.WriteFile(filepath.Join(dir, "num_seven.end.wav"), loudness.WAV16(tone(44100, 300, 0.3, 0.3)), 0o644)
	os.WriteFile(filepath.Join(dir, "stray.wav"), loudness.WAV16(tone(44100, 300, 0.3, 0.3)), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	rep, err := tk.ImportDir(m, "booth", dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(rep.Imported, ",") != "lit/green flag/*,num/seven/end" || strings.Join(rep.Unmatched, ",") != "stray.wav" {
		t.Errorf("import report = %+v", rep)
	}
	cov := tk.Coverage(m)
	// green flag: both variants via the shared read; seven: only end.
	if cov.Have != 3 || cov.Total != 8 {
		t.Errorf("coverage = %d of %d", cov.Have, cov.Total)
	}
	if !strings.Contains(cov.String(), "missing num/seven/mid") || strings.Contains(cov.String(), "missing lit/green flag") {
		t.Errorf("coverage text:\n%s", cov)
	}
	if _, err := tk.ImportDir(m, "nobody", dir); err == nil {
		t.Error("unknown voice accepted")
	}
}

func TestImportLongBySilenceAndClaps(t *testing.T) {
	m := takesManifest()
	for _, mode := range []SliceMode{SliceSilence, SliceClaps} {
		tk := &Takes{Dir: t.TempDir()}
		reqs := tk.Pending(m, "booth")
		var long loudness.Audio
		clap := loudness.Audio{Rate: 48000, Samples: make([]float64, 200)}
		for i := range clap.Samples {
			clap.Samples[i] = 0.97 * math.Pow(-1, float64(i)) * math.Exp(-float64(i)/60)
		}
		long.Rate = 48000
		for i := range reqs {
			part := tone(48000, 250+float64(i)*10, 0.2, 0.35+0.02*float64(i))
			if mode == SliceClaps {
				long = withSilence(long, clap, quiet(48000, 0.25), part, quiet(48000, 0.25))
			} else {
				long = withSilence(long, quiet(48000, 0.6), part)
			}
		}
		long = withSilence(long, quiet(48000, 0.6))
		if err := tk.ImportLong(long, reqs, mode); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if got := tk.Coverage(m); got.MissingCount != 0 {
			t.Errorf("%s: %d takes still missing", mode, got.MissingCount)
		}
		// Each take is the right line: lengths grow with the index.
		var prev int
		for i, r := range reqs {
			a, ok, err := tk.Take(r)
			if err != nil || !ok {
				t.Fatalf("%s take %d: %v %v", mode, i, ok, err)
			}
			if i > 0 && len(a.Samples) < prev {
				t.Errorf("%s: take %d shorter than the one before; order is wrong", mode, i)
			}
			prev = len(a.Samples)
		}
		// A count mismatch writes nothing.
		tk2 := &Takes{Dir: t.TempDir()}
		if err := tk2.ImportLong(long, reqs[:len(reqs)-1], mode); err == nil || !strings.Contains(err.Error(), "expected") {
			t.Errorf("%s: mismatch: %v", mode, err)
		}
		if tk2.Coverage(m).Have != 0 {
			t.Errorf("%s: a failed import wrote takes", mode)
		}
	}
}

func TestBoothServesScriptAndStoresTakes(t *testing.T) {
	m := takesManifest()
	tk := &Takes{Dir: t.TempDir()}
	b, err := NewBooth(m, tk, "", &Mock{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(b.Handler())
	defer srv.Close()
	do := func(method, path, token string, body []byte) (*http.Response, []byte) {
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
		if token != "" {
			req.Header.Set("X-Booth-Token", token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, data
	}
	for _, p := range []string{"/", "/booth.js", "/recorder-worklet.js"} {
		if r, body := do("GET", p, "", nil); r.StatusCode != 200 || len(body) < 100 {
			t.Errorf("%s: %d, %d bytes", p, r.StatusCode, len(body))
		}
	}
	if r, _ := do("GET", "/api/state", "", nil); r.StatusCode != http.StatusForbidden {
		t.Errorf("state without a token: %d", r.StatusCode)
	}
	if r, _ := do("GET", "/api/state", "wrong", nil); r.StatusCode != http.StatusForbidden {
		t.Errorf("state with a wrong token: %d", r.StatusCode)
	}
	r, body := do("GET", "/api/state", b.Token(), nil)
	if r.StatusCode != 200 || !strings.Contains(string(body), `"text":"green flag"`) || !strings.Contains(string(body), `"have":false`) {
		t.Fatalf("state: %d %s", r.StatusCode, body)
	}
	if !strings.Contains(string(body), `"variant":"end"`) {
		t.Error("state lacks the variant")
	}
	// Post a take for line 1 (green flag / end).
	wav := loudness.WAV16(tone(48000, 300, 0.3, 0.3))
	if r, _ := do("POST", "/api/take?index=1", "", wav); r.StatusCode != http.StatusForbidden {
		t.Error("take accepted without a token")
	}
	if r, _ := do("POST", "/api/take?index=1", b.Token(), wav); r.StatusCode != http.StatusNoContent {
		t.Fatalf("take: %d", r.StatusCode)
	}
	if r, _ := do("POST", "/api/take?index=99", b.Token(), wav); r.StatusCode != http.StatusNotFound {
		t.Errorf("take for a bad index: %d", r.StatusCode)
	}
	if r, _ := do("POST", "/api/take?index=2", b.Token(), []byte("not a wav")); r.StatusCode != http.StatusBadRequest {
		t.Errorf("garbage take: %d", r.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(tk.Dir, "booth", "lit_green_flag.end.wav")); err != nil {
		t.Errorf("take not stored under its stem: %v", err)
	}
	_, body = do("GET", "/api/state", b.Token(), nil)
	if !strings.Contains(string(body), `"have":true`) {
		t.Error("state does not show the new take")
	}
	// Reference read.
	if r, body := do("GET", "/api/ref?index=0", b.Token(), nil); r.StatusCode != 200 || !bytes.HasPrefix(body, []byte("RIFF")) {
		t.Errorf("reference: %d", r.StatusCode)
	}
	b2, _ := NewBooth(m, tk, "booth", nil)
	srv2 := httptest.NewServer(b2.Handler())
	defer srv2.Close()
	req, _ := http.NewRequest("GET", srv2.URL+"/api/ref?index=0", nil)
	req.Header.Set("X-Booth-Token", b2.Token())
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != 404 {
		t.Errorf("reference without a provider: %d", resp.StatusCode)
	}
	resp.Body.Close()
	m.Voices["other"] = speech.Voice{}
	if _, err := NewBooth(m, tk, "", nil); err == nil {
		t.Error("two voices and no --voice accepted")
	}
}

func TestBoothListensOnLoopbackOnly(t *testing.T) {
	b, _ := NewBooth(takesManifest(), &Takes{Dir: t.TempDir()}, "", nil)
	ln, err := b.Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !strings.HasPrefix(ln.Addr().String(), "127.0.0.1:") {
		t.Errorf("listening on %s", ln.Addr())
	}
}

func TestBoothPageCarriesRecorderAndMeter(t *testing.T) {
	for _, want := range []string{"getUserMedia", "audioWorklet.addModule", "echoCancellation: false", "retake", "meter"} {
		if !strings.Contains(string(boothJS)+string(boothIndex), want) {
			t.Errorf("booth page lacks %q", want)
		}
	}
	if !strings.Contains(string(boothWorklet), "registerProcessor") {
		t.Error("worklet does not register a processor")
	}
}
