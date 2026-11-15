package voicerender

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// racingManifest is a small synthetic corpus: a racing announcer with a dozen
// lines in two packs.
func racingManifest() *speech.RenderManifest {
	m := &speech.RenderManifest{
		Voices: map[string]speech.Voice{
			"announcer": {Provider: "mock", VoiceID: "v1", Stability: 0.5},
			"colour":    {Provider: "mock", VoiceID: "v2"},
		},
		Packs: map[string]speech.PackSpec{
			"pa":    {Mode: speech.PackBundled, BudgetBytes: 600_000},
			"names": {Mode: speech.PackStream, BudgetBytes: 400_000},
		},
	}
	add := func(key, text, voice, pack string, variants ...string) {
		m.Phrases = append(m.Phrases, speech.RenderPhrase{Key: key, Text: text, Voice: voice, Pack: pack, Variants: variants})
	}
	add("lit/takes the lead", "takes the lead", "announcer", "pa")
	add("lit/and it is", "and it is", "announcer", "pa", "start", "mid")
	add("lit/into the final lap", "into the final lap", "announcer", "pa", "end")
	add("lit/oh no!", "oh no!", "announcer", "pa", "start")
	add("num/seven", "seven", "announcer", "pa")
	add("num/forty", "forty", "announcer", "pa")
	add("lit/what a move", "what a move", "colour", "pa", "end")
	add("lit/that is risky", "that is risky", "colour", "pa", "mid", "end")
	add("pool/driver/okafor", "Okafor", "announcer", "names")
	add("pool/driver/vance", "Vance", "announcer", "names")
	add("pool/driver/reyes", "Reyes", "announcer", "names", "end")
	add("pool/driver/lindqvist", "Lindqvist", "announcer", "names", "mid", "end")
	return m
}

func opts(t *testing.T, mock *Mock) Options {
	return Options{
		Manifest: racingManifest(), OutDir: filepath.Join(t.TempDir(), "out"), CacheDir: filepath.Join(t.TempDir(), "cache"),
		Providers: map[string]Provider{"mock": mock}, DefaultProvider: "mock",
	}
}

func TestRenderWritesPacksAndManifests(t *testing.T) {
	mock := &Mock{}
	o := opts(t, mock)
	res, err := Render(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Packs) != 2 || res.Packs[0].Name != "names" || res.Packs[1].Name != "pa" {
		t.Fatalf("packs = %+v", res.Packs)
	}
	want := len(o.Manifest.Requests())
	if got := int(mock.Calls.Load()); got != want || res.Calls != want {
		t.Errorf("provider calls = %d (result %d), want %d", got, res.Calls, want)
	}
	// Index and pack manifests round-trip and every clip lies inside its atlas.
	data, err := os.ReadFile(filepath.Join(o.OutDir, PackIndexFile))
	if err != nil {
		t.Fatal(err)
	}
	var idx speech.PackIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatal(err)
	}
	clips := 0
	for _, p := range idx.Packs {
		raw, err := os.ReadFile(filepath.Join(o.OutDir, p.Manifest))
		if err != nil {
			t.Fatal(err)
		}
		m, err := speech.ParseClipManifest(raw)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		clips += len(m.Clips)
		for _, a := range m.Atlases {
			if _, err := os.Stat(filepath.Join(o.OutDir, p.Name, a.File)); err != nil {
				t.Error(err)
			}
		}
		if p.Bytes == 0 || p.Bytes > p.BudgetBytes {
			t.Errorf("pack %s bytes %d budget %d", p.Name, p.Bytes, p.BudgetBytes)
		}
	}
	if clips != want {
		t.Errorf("clips = %d, want %d", clips, want)
	}
	if idx.Packs[0].Mode != speech.PackStream || idx.Packs[1].Mode != speech.PackBundled {
		t.Errorf("modes = %v %v", idx.Packs[0].Mode, idx.Packs[1].Mode)
	}
}

func TestRenderedClipsAreTrimmedAndLoud(t *testing.T) {
	o := opts(t, &Mock{})
	o.TargetLUFS, o.CeilingDBTP = -20, -2
	res, err := Render(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	pa := res.Packs[1]
	raw, _ := os.ReadFile(filepath.Join(o.OutDir, "pa", pa.Manifest.Atlases[0].File))
	atlas, err := loudness.ParseWAV(raw)
	if err != nil {
		t.Fatal(err)
	}
	if atlas.Rate != 48000 {
		t.Errorf("rate = %d", atlas.Rate)
	}
	for _, c := range pa.Manifest.Clips {
		if c.Atlas != 0 {
			continue
		}
		clip := loudness.Audio{Rate: atlas.Rate, Samples: atlas.Samples[c.Offset : c.Offset+c.Frames]}
		rep := loudness.Measure(clip)
		if math.Abs(rep.LUFS-(-20)) > 0.6 {
			t.Errorf("%s/%s loudness %.2f, want -20", c.Key, c.Variant, rep.LUFS)
		}
		if rep.TruePeakDB > -2+0.1 {
			t.Errorf("%s/%s true peak %.2f dBTP over the -2 ceiling", c.Key, c.Variant, rep.TruePeakDB)
		}
		// The mock adds 120 ms of lead silence and 200 ms of tail; trim leaves about 6 ms.
		lead := 0
		for lead < len(clip.Samples) && math.Abs(clip.Samples[lead]) < 0.003 {
			lead++
		}
		if float64(lead)/48000 > 0.02 {
			t.Errorf("%s/%s keeps %.0f ms of leading silence", c.Key, c.Variant, 1000*float64(lead)/48000)
		}
		if clip.Samples[0] != 0 {
			t.Errorf("%s/%s does not start on zero (click)", c.Key, c.Variant)
		}
	}
	if st := pa.Manifest.Voices["announcer"]; st.Clips == 0 || st.Characters == 0 {
		t.Errorf("voice stats = %+v", st)
	}
}

func TestRenderCacheMakesRerunFree(t *testing.T) {
	mock := &Mock{}
	o := opts(t, mock)
	first, err := Render(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	calls := mock.Calls.Load()
	o.OutDir = filepath.Join(t.TempDir(), "out2")
	second, err := Render(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if mock.Calls.Load() != calls || second.Calls != 0 {
		t.Errorf("rerun made %d new calls", mock.Calls.Load()-calls)
	}
	if second.CacheHits != first.Calls {
		t.Errorf("cache hits = %d, want %d", second.CacheHits, first.Calls)
	}
	// Output is byte-identical.
	for _, p := range first.Packs {
		for _, a := range p.Manifest.Atlases {
			x, _ := os.ReadFile(filepath.Join(filepath.Dir(o.OutDir), "out", p.Name, a.File))
			_ = x
		}
	}
	a, _ := os.ReadFile(filepath.Join(o.OutDir, "pa", ManifestFile))
	if !bytes.Contains(a, []byte(`"key": "lit/takes the lead"`)) {
		t.Error("manifest lacks a clip")
	}
	// Changing the loudness target does not re-bill.
	o.TargetLUFS = -18
	o.OutDir = filepath.Join(t.TempDir(), "out3")
	if _, err := Render(context.Background(), o); err != nil || mock.Calls.Load() != calls {
		t.Errorf("a loudness change called the provider (err %v)", err)
	}
	// Changing a voice setting does.
	o.Manifest.Voices["colour"] = speech.Voice{Provider: "mock", VoiceID: "v2", Stability: 0.9}
	if _, err := Render(context.Background(), o); err != nil || mock.Calls.Load() == calls {
		t.Errorf("a voice change did not re-render (err %v)", err)
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	read := func() map[string][]byte {
		o := opts(t, &Mock{})
		if _, err := Render(context.Background(), o); err != nil {
			t.Fatal(err)
		}
		out := map[string][]byte{}
		filepath.Walk(o.OutDir, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				rel, _ := filepath.Rel(o.OutDir, p)
				out[rel], _ = os.ReadFile(p)
			}
			return nil
		})
		return out
	}
	a, b := read(), read()
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("file sets differ: %d vs %d", len(a), len(b))
	}
	for k := range a {
		if !bytes.Equal(a[k], b[k]) {
			t.Errorf("%s differs between runs", k)
		}
	}
}

func TestRenderDedupesIdenticalText(t *testing.T) {
	mock := &Mock{}
	o := opts(t, mock)
	o.Manifest.Phrases = append(o.Manifest.Phrases, speech.RenderPhrase{Key: "lit/duplicate read", Text: "takes the lead", Voice: "announcer", Pack: "pa"})
	res, err := Render(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := int(mock.Calls.Load()), len(o.Manifest.Requests())-3; got != want {
		t.Errorf("calls = %d, want %d (three identical texts share one call)", got, want)
	}
	if res.CacheHits < 3 {
		t.Errorf("hits = %d", res.CacheHits)
	}
}

func TestRenderPackBudget(t *testing.T) {
	o := opts(t, &Mock{})
	o.Manifest.Packs["pa"] = speech.PackSpec{Mode: speech.PackBundled, BudgetBytes: 1000}
	res, err := Render(context.Background(), o)
	if !errors.Is(err, ErrOverBudget) {
		t.Fatalf("err = %v, want ErrOverBudget", err)
	}
	if !strings.Contains(err.Error(), "pa is") || strings.Contains(err.Error(), "names is") {
		t.Errorf("error should name only the pack over budget: %v", err)
	}
	if res == nil || len(res.Packs) != 2 {
		t.Error("an over-budget render should still return what it wrote")
	}
}

func TestRenderErrors(t *testing.T) {
	o := opts(t, &Mock{})
	o.Providers = map[string]Provider{}
	if _, err := Render(context.Background(), o); !errors.As(err, new(ErrNoProvider)) {
		t.Errorf("missing provider: %v", err)
	}
	o = opts(t, &Mock{Fail: map[string]error{"Reyes": errors.New("boom")}})
	if _, err := Render(context.Background(), o); err == nil || !strings.Contains(err.Error(), "reyes") {
		t.Errorf("provider failure should name the phrase: %v", err)
	}
	o = opts(t, &Mock{})
	o.Codec = "mp3"
	if _, err := Render(context.Background(), o); err == nil {
		t.Error("unknown codec accepted")
	}
}

type paidMock struct{ Mock }

func (*paidMock) Paid() bool   { return true }
func (*paidMock) Name() string { return "elevenlabs" }

func TestPaidProviderNeedsBudget(t *testing.T) {
	p := &paidMock{}
	o := opts(t, &p.Mock)
	o.Providers = map[string]Provider{"elevenlabs": p}
	o.DefaultProvider = "elevenlabs"
	for n, v := range o.Manifest.Voices {
		v.Provider = ""
		o.Manifest.Voices[n] = v
	}
	if _, err := Render(context.Background(), o); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("paid render without a budget: %v", err)
	}
	est := EstimateManifest(o.Manifest, EstimateOptions{DefaultProvider: "elevenlabs", Providers: o.Providers})
	o.MaxChars = est.Total.Billable - 1
	if _, err := Render(context.Background(), o); err == nil || !strings.Contains(err.Error(), "over the") {
		t.Fatalf("over budget: %v", err)
	}
	if p.Calls.Load() != 0 {
		t.Error("a refused render still called the provider")
	}
	o.MaxChars = est.Total.Billable
	if _, err := Render(context.Background(), o); err != nil {
		t.Fatalf("within budget: %v", err)
	}
}

func TestEstimateCountsCharactersPerVoice(t *testing.T) {
	m := racingManifest()
	est := EstimateManifest(m, EstimateOptions{DefaultProvider: "mock", PricePer1K: 0.30})
	byVoice := map[string]VoiceEstimate{}
	for _, v := range est.Voices {
		byVoice[v.Voice] = v
	}
	// colour: "what a move" end only -> "what a move." (12); "that is risky" mid ("that is risky", 13) and end (14).
	if got := byVoice["colour"].Characters; got != 12+13+14 {
		t.Errorf("colour characters = %d, want 39", got)
	}
	if byVoice["colour"].Requests != 3 || byVoice["colour"].Unique != 3 {
		t.Errorf("colour = %+v", byVoice["colour"])
	}
	// Free providers bill nothing.
	if est.Total.Billable != 0 || est.Cost != 0 {
		t.Errorf("mock billed %d chars, cost %v", est.Total.Billable, est.Cost)
	}
	var sum int
	for _, v := range est.Voices {
		sum += v.Characters
	}
	if sum != est.Total.Characters {
		t.Errorf("total %d != sum %d", est.Total.Characters, sum)
	}
	// With a paid provider, cached texts are not billable and price applies.
	for n, v := range m.Voices {
		v.Provider = ""
		m.Voices[n] = v
	}
	paid := EstimateManifest(m, EstimateOptions{DefaultProvider: "elevenlabs", PricePer1K: 0.30})
	if paid.Total.Billable != paid.Total.Characters || paid.Cost <= 0 {
		t.Errorf("paid estimate = %+v", paid)
	}
	want := float64(paid.Total.Characters) / 1000 * 0.30
	if math.Abs(paid.Cost-want) > 1e-9 {
		t.Errorf("cost = %v, want %v", paid.Cost, want)
	}
}

func TestEstimateDiscountsCache(t *testing.T) {
	mock := &Mock{}
	o := opts(t, mock)
	for n, v := range o.Manifest.Voices {
		v.Provider = "elevenlabs"
		o.Manifest.Voices[n] = v
	}
	// Seed the cache for one voice by rendering it with the mock under the elevenlabs fingerprint.
	pm := &paidMock{}
	o.Providers = map[string]Provider{"elevenlabs": pm}
	o.MaxChars = 1 << 20
	if _, err := Render(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	est := EstimateManifest(o.Manifest, EstimateOptions{DefaultProvider: "elevenlabs", Providers: o.Providers, CacheDir: o.CacheDir})
	if est.Total.Billable != 0 || est.Total.Cached != est.Total.Characters {
		t.Errorf("after a full render the estimate should bill nothing: %+v", est.Total)
	}
}

func TestPackSpecValidation(t *testing.T) {
	m := racingManifest()
	m.Phrases[0].Pack = "nope"
	if err := m.Validate(); err == nil {
		t.Error("undeclared pack accepted")
	}
	m = racingManifest()
	m.Packs["a/b"] = speech.PackSpec{}
	if err := m.Validate(); err == nil {
		t.Error("path-like pack name accepted")
	}
	m = racingManifest()
	m.Phrases = append(m.Phrases, speech.RenderPhrase{Key: "lit/Takes The Lead!", Text: "x", Voice: "announcer", Pack: "pa"})
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "stem") {
		t.Errorf("colliding stems: %v", err)
	}
}
