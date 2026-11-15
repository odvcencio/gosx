//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"m31labs.dev/gosx/game/audio/speech"
)

// The page replays a speech timeline the way game/audio/speech/voice.Player
// schedules it: one buffer source per segment started at a whole-frame time
// with an atlas offset and duration, a per-segment gain with SetAt and linear
// ramps, and one gain per line for the interrupt fade. An OfflineAudioContext
// renders it, and the test compares the samples with speech.Mix, the
// reference the sequencer's tests use.
const speechOfflineHTML = `<!doctype html><meta charset="utf-8"><title>speech</title>
<script>
window.renderTimeline = async function(plan) {
  const out = {};
  try {
    const rate = plan.rate;
    const ctx = new OfflineAudioContext(1, plan.frames, rate);
    const atlas = ctx.createBuffer(1, plan.atlas.length, rate);
    atlas.getChannelData(0).set(Float32Array.from(plan.atlas));
    const lines = {};
    for (const id of plan.lines) {
      const g = ctx.createGain();
      g.connect(ctx.destination);
      lines[id] = g;
    }
    for (const c of plan.cuts) {
      const p = lines[c.line].gain;
      p.setValueAtTime(1, c.at / rate);
      p.linearRampToValueAtTime(0, (c.at + c.fade) / rate);
    }
    for (const s of plan.segs) {
      const src = ctx.createBufferSource();
      src.buffer = atlas;
      const g = ctx.createGain();
      src.connect(g); g.connect(lines[s.line]);
      const t0 = s.start / rate, t1 = (s.start + s.frames) / rate;
      if (s.fadeIn > 0) {
        g.gain.value = 0;
        g.gain.setValueAtTime(0, t0);
        g.gain.linearRampToValueAtTime(1, t0 + s.fadeIn / rate);
      }
      if (s.fadeOut > 0) {
        g.gain.setValueAtTime(1, t1 - s.fadeOut / rate);
        g.gain.linearRampToValueAtTime(0, t1);
      }
      src.start(t0, s.offset / rate, s.frames / rate);
      let stop = t1;
      for (const c of plan.cuts) if (c.line === s.line) stop = Math.min(stop, (c.at + c.fade) / rate);
      src.stop(stop);
    }
    const buf = await ctx.startRendering();
    out.samples = Array.from(buf.getChannelData(0));
  } catch (e) { out.error = String(e && e.message || e); }
  return JSON.stringify(out);
};
window.pageReady = true;
</script>`

func TestSpeechTimelineMatchesOfflineAudioContext(t *testing.T) {
	chrome := e2eChromePath(t)
	const rate = 48000

	// A synthetic racing announcer: 12 fragments, each a ramp so a wrong
	// offset or start frame shows up as a different value.
	g, err := speech.ParseGrammar([]byte(`{"pools":{"d":["okafor","vance","reyes"]},"phrases":[
	  {"id":"a","event":"a","variants":[{"say":"{d} takes the lead {~60} into turn {n:number}"}]},
	  {"id":"b","event":"b","priority":9,"variants":[{"say":"oh no {d}"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	lib := speech.NewLibrary(rate)
	if _, err := g.Compile(lib); err != nil {
		t.Fatal(err)
	}
	// Add the number field: grammar above uses {n:number}; compile interned words.
	const clipLen = 1500
	var atlas []float32
	for id := speech.FragID(1); int(id) <= lib.Len(); id++ {
		off := len(atlas)
		for i := 0; i < clipLen+int(id)*40; i++ {
			atlas = append(atlas, float32(i%97)/97*0.8)
		}
		n := len(atlas) - off
		data := atlas[off : off+n]
		for _, p := range []speech.Pos{speech.PosStart, speech.PosMid, speech.PosEnd} {
			lib.Set(id, p, speech.Clip{Frames: int32(n), Handle: offsetData{off, data}})
		}
	}
	mixData := &speech.Mix{}
	seq := speech.NewSequencer(lib, mixData, speech.SequencerConfig{CrossfadeMs: 6, CutFadeMs: 8})
	c, _ := g.Compile(lib)
	p := speech.NewPlanner(c, speech.PlannerConfig{Seed: 3})
	var ev speech.Event
	ev.Reset("a")
	ev.Str("d", "vance").Num("n", 4)
	d := p.Submit(&ev, 0, speech.Playing{})
	if d.Action != speech.Play || !seq.Apply(&d, 1000) {
		t.Fatalf("first line: %v", d.Action)
	}
	ev.Reset("b")
	ev.Str("d", "reyes")
	d = p.Submit(&ev, 0.3, seq.Playing(1000+9000))
	if d.Action != speech.Interrupt || !seq.Apply(&d, 1000+9000) {
		t.Fatalf("interrupt: %v", d.Action)
	}

	// The reference mix reads float32 slices; give it the clip data.
	ref := &speech.Mix{}
	for _, s := range mixData.Segments() {
		od := s.Clip.Handle.(offsetData)
		s.Clip.Handle = od.data
		ref.BeginLine(s.Line)
		ref.Schedule(s.Line, s.Segment)
	}
	for _, cut := range mixData.Cuts() {
		ref.CutLine(cut.Line, cut.At, cut.Fade)
	}
	var end int64
	type segJSON struct {
		Line    uint64 `json:"line"`
		Start   int64  `json:"start"`
		Frames  int32  `json:"frames"`
		Offset  int    `json:"offset"`
		FadeIn  int32  `json:"fadeIn"`
		FadeOut int32  `json:"fadeOut"`
	}
	plan := struct {
		Rate   int       `json:"rate"`
		Frames int64     `json:"frames"`
		Atlas  []float32 `json:"atlas"`
		Lines  []uint64  `json:"lines"`
		Cuts   []struct {
			Line uint64 `json:"line"`
			At   int64  `json:"at"`
			Fade int32  `json:"fade"`
		} `json:"cuts"`
		Segs []segJSON `json:"segs"`
	}{Rate: rate, Atlas: atlas}
	seen := map[uint64]bool{}
	for _, s := range mixData.Segments() {
		od := s.Clip.Handle.(offsetData)
		plan.Segs = append(plan.Segs, segJSON{s.Line, s.Start, s.Clip.Frames, od.off, s.FadeIn, s.FadeOut})
		if !seen[s.Line] {
			seen[s.Line] = true
			plan.Lines = append(plan.Lines, s.Line)
		}
		end = max(end, s.End())
	}
	for _, cut := range mixData.Cuts() {
		plan.Cuts = append(plan.Cuts, struct {
			Line uint64 `json:"line"`
			At   int64  `json:"at"`
			Fade int32  `json:"fade"`
		}{cut.Line, cut.At, cut.Fade})
	}
	plan.Frames = end + 2000
	if len(plan.Cuts) != 1 || len(plan.Lines) != 2 {
		t.Fatalf("plan has %d cuts and %d lines", len(plan.Cuts), len(plan.Lines))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(speechOfflineHTML))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	ctx, cancel := newChromeContext(t, chrome)
	defer cancel()
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.WaitReady("body")); err != nil {
		if browserUnavailable(err) {
			skipOrFailChrome(t, chromeRequired(), "chrome could not start in this environment: %v", err)
		}
		t.Fatalf("navigate: %v", err)
	}
	planJSON, _ := json.Marshal(plan)
	var raw string
	evalCtx, evalCancel := context.WithTimeout(ctx, 20*time.Second)
	defer evalCancel()
	if err := chromedp.Run(evalCtx, chromedp.Evaluate(`window.renderTimeline(`+string(planJSON)+`)`, &raw, awaitPromise)); err != nil {
		t.Fatalf("render: %v", err)
	}
	var res struct {
		Error   string    `json:"error"`
		Samples []float64 `json:"samples"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil || res.Error != "" {
		t.Fatalf("browser: %v %s", err, res.Error)
	}
	want := ref.Render(0, int(plan.Frames))
	if len(res.Samples) != len(want) {
		t.Fatalf("rendered %d frames, want %d", len(res.Samples), len(want))
	}
	var worst float64
	var worstAt int
	var energy float64
	for i := range want {
		d := math.Abs(res.Samples[i] - float64(want[i]))
		if d > worst {
			worst, worstAt = d, i
		}
		energy += math.Abs(float64(want[i]))
	}
	if energy < 100 {
		t.Fatal("the reference mix is silent; the test proves nothing")
	}
	// Sample-accurate: the browser and the reference agree to float32 precision
	// on every frame, including across crossfades and the interrupt fade.
	if worst > 1e-3 {
		t.Errorf("OfflineAudioContext differs from the reference by %g at frame %d (browser %g, reference %g)",
			worst, worstAt, res.Samples[worstAt], want[worstAt])
	}
}

// offsetData is a clip handle that remembers where its data sits in the atlas.
type offsetData struct {
	off  int
	data []float32
}
