package voicerender

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// Output file names. Each pack writes ManifestFile in its own directory;
// PackIndexFile lists the packs.
const (
	ManifestFile  = "speech.clips.json"
	PackIndexFile = "speech.packs.json"
)

// RequestProvider is implemented by sources that supply a recording per
// request instead of speaking text: the recording-booth takes. Their audio is
// not cached (the files are the source), and a missing take is reported
// instead of fetched.
type RequestProvider interface {
	Provider
	// Take returns the recording for req, or ok=false when none exists.
	Take(req speech.Request) (a loudness.Audio, ok bool, err error)
}

// Options configures Render. A zero field takes its default.
type Options struct {
	Manifest *speech.RenderManifest
	// OutDir receives one directory per pack plus the pack index.
	OutDir string
	// CacheDir holds provider audio by content hash. Empty disables caching.
	CacheDir string
	// Providers are available by name; DefaultProvider serves voices that name none.
	Providers       map[string]Provider
	DefaultProvider string
	// Rate is the output sample rate. Default 48000.
	Rate int
	// TargetLUFS is the loudness each clip is gained to (a voice may override
	// it). Default -23.
	TargetLUFS float64
	// CeilingDBTP is the true-peak limit. Default -1.
	CeilingDBTP float64
	// TrimDB is the silence threshold in dBFS measured on the raw clip.
	// Default -50. PadMs is the quiet kept on each side. Default 6.
	TrimDB, PadMs float64
	// Codec is "wav" (default, 16-bit mono) or "opus" (ogg opus via ffmpeg,
	// 48000 Hz only; a 10 ms guard separates clips).
	Codec string
	// Jobs is the number of concurrent provider calls. Default 2.
	Jobs int
	// MaxChars caps billable characters for paid providers. Render refuses to
	// start when the estimate exceeds it, and refuses a paid provider when it
	// is zero.
	MaxChars int
	// AllowMissing lets a render succeed when some takes have not been
	// recorded yet; the clips are left out and listed in Result.Missing.
	AllowMissing bool
	// Progress receives one line per phrase variant. It never receives text
	// from the environment or request headers.
	Progress io.Writer
}

// PackResult describes one written pack.
type PackResult struct {
	Name     string
	Mode     string
	Manifest *speech.ClipManifest
	// Bytes is the total size of the pack's audio files.
	Bytes  int64
	Budget int64
}

// ErrOverBudget is returned (wrapped) when a pack exceeds its size budget.
var ErrOverBudget = errors.New("voicerender: pack over its size budget")

// Result summarizes a render.
type Result struct {
	Packs []PackResult
	// Calls counts provider calls; CacheHits counts requests served from the
	// cache (or by an identical text already in this run).
	Calls, CacheHits int
	// Missing lists "key/variant" of takes that were not recorded.
	Missing []string
	// Estimate is the pre-render character report.
	Estimate Estimate
}

type job struct {
	req   speech.Request
	fp    string
	voice speech.Voice
	prov  Provider
	audio loudness.Audio
}

// Render produces the clips for every request in the manifest and writes the
// packs to OutDir. It is deterministic: the same manifest, cache, takes and
// options produce byte-identical output. When a pack is over its budget,
// Render still writes everything and returns the result with an error that
// wraps ErrOverBudget.
func Render(ctx context.Context, o Options) (*Result, error) {
	if o.Manifest == nil {
		return nil, errors.New("voicerender: no manifest")
	}
	if err := o.Manifest.Validate(); err != nil {
		return nil, err
	}
	if o.Rate == 0 {
		o.Rate = 48000
	}
	if o.TargetLUFS == 0 {
		o.TargetLUFS = -23
	}
	if o.CeilingDBTP == 0 {
		o.CeilingDBTP = -1
	}
	if o.TrimDB == 0 {
		o.TrimDB = -50
	}
	if o.PadMs == 0 {
		o.PadMs = 6
	}
	if o.Codec == "" {
		o.Codec = "wav"
	}
	if o.Jobs <= 0 {
		o.Jobs = 2
	}
	if o.Codec != "wav" && o.Codec != "opus" {
		return nil, fmt.Errorf("voicerender: unknown codec %q (want wav or opus)", o.Codec)
	}
	if o.Codec == "opus" && o.Rate != 48000 {
		return nil, errors.New("voicerender: the opus codec needs rate 48000")
	}
	if o.OutDir == "" {
		return nil, errors.New("voicerender: no output directory")
	}
	c := cache{dir: o.CacheDir}

	// Resolve a provider per request. Text providers dedupe identical
	// syntheses; take providers answer per request.
	reqs := o.Manifest.Requests()
	jobs := make([]*job, 0, len(reqs))
	byFP := map[string]*job{}
	order := make([]*job, len(reqs)) // request index -> job (nil: take missing)
	var missing []string
	for i, r := range reqs {
		v := o.Manifest.Voices[r.Voice]
		name := v.Provider
		if name == "" {
			name = o.DefaultProvider
		}
		p, ok := o.Providers[name]
		if !ok {
			return nil, ErrNoProvider{Voice: r.Voice, Provider: name}
		}
		if rp, ok := p.(RequestProvider); ok {
			a, have, err := rp.Take(r)
			if err != nil {
				return nil, fmt.Errorf("voicerender: take %s/%s: %w", r.Key, r.Variant, err)
			}
			if !have {
				missing = append(missing, r.Key+"/"+r.Variant)
				continue
			}
			order[i] = &job{req: r, voice: v, prov: p, audio: a}
			continue
		}
		fp := fingerprint(p.Name(), v, r.Text)
		if j, ok := byFP[fp]; ok {
			order[i] = j
			continue
		}
		j := &job{req: r, fp: fp, voice: v, prov: p}
		byFP[fp] = j
		jobs = append(jobs, j)
		order[i] = j
	}
	if len(missing) > 0 && !o.AllowMissing {
		return nil, fmt.Errorf("voicerender: %d takes are not recorded (first: %s); record them or allow a partial render", len(missing), missing[0])
	}

	est := EstimateManifest(o.Manifest, EstimateOptions{DefaultProvider: o.DefaultProvider, Providers: o.Providers, CacheDir: o.CacheDir})
	if est.Total.Billable > 0 {
		if o.MaxChars <= 0 {
			return nil, fmt.Errorf("voicerender: this render would send %d billable characters to a paid provider; set a MaxChars budget to allow it", est.Total.Billable)
		}
		if est.Total.Billable > o.MaxChars {
			return nil, fmt.Errorf("voicerender: this render needs %d billable characters, over the %d budget", est.Total.Billable, o.MaxChars)
		}
	}

	var calls, hits atomic.Int64
	var progMu sync.Mutex
	say := func(format string, args ...any) {
		if o.Progress != nil {
			progMu.Lock()
			fmt.Fprintf(o.Progress, format+"\n", args...)
			progMu.Unlock()
		}
	}

	// Fetch: cache first, then the provider, o.Jobs at a time.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sem := make(chan struct{}, o.Jobs)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	textReqs := 0
	for _, j := range jobs {
		textReqs++
		if a, ok := c.get(j.fp); ok {
			j.audio = a
			hits.Add(1)
			say("cache  %s/%s", j.req.Key, j.req.Variant)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(j *job) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil {
				return
			}
			a, err := j.prov.Synthesize(ctx, j.voice, j.req.Text)
			if err == nil && len(a.Samples) == 0 {
				err = errors.New("provider returned no audio")
			}
			if err != nil {
				errOnce.Do(func() {
					firstErr = fmt.Errorf("voicerender: %s/%s: %w", j.req.Key, j.req.Variant, err)
					cancel()
				})
				return
			}
			calls.Add(1)
			if perr := c.put(j.fp, a); perr != nil {
				errOnce.Do(func() { firstErr = perr; cancel() })
				return
			}
			j.audio = a
			say("render %s/%s", j.req.Key, j.req.Variant)
		}(j)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Requests folded into an identical earlier synthesis count as hits.
	for i := range reqs {
		if order[i] != nil && order[i].fp != "" && order[i].req != reqs[i] {
			hits.Add(1)
		}
	}

	// Clean each request's audio into a clip, then pack per pack and voice.
	type clip struct {
		req   speech.Request
		audio loudness.Audio
		rep   loudness.Report
	}
	type packKey struct{ pack, voice string }
	groups := map[packKey][]clip{}
	for i, r := range reqs {
		j := order[i]
		if j == nil {
			continue
		}
		target := o.TargetLUFS
		if v := o.Manifest.Voices[r.Voice]; v.TargetLUFS != nil {
			target = *v.TargetLUFS
		}
		a, rep, err := ProcessClip(j.audio, ProcessOptions{Rate: o.Rate, TrimDB: o.TrimDB, PadMs: o.PadMs, TargetLUFS: target, CeilingDBTP: o.CeilingDBTP})
		if err != nil {
			return nil, fmt.Errorf("voicerender: %s/%s: %w", r.Key, r.Variant, err)
		}
		k := packKey{r.Pack, r.Voice}
		groups[k] = append(groups[k], clip{req: r, audio: a, rep: rep})
	}

	packNames := map[string]bool{}
	for k := range groups {
		packNames[k.pack] = true
	}
	for n := range o.Manifest.Packs {
		packNames[n] = true
	}
	sorted := make([]string, 0, len(packNames))
	for n := range packNames {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	guard := 0
	if o.Codec == "opus" {
		guard = o.Rate / 100
	}
	res := &Result{Calls: int(calls.Load()), CacheHits: int(hits.Load()), Missing: missing, Estimate: est}
	index := speech.PackIndex{Version: 1}
	var overBudget []string
	for _, pack := range sorted {
		spec := o.Manifest.Packs[pack]
		mode := spec.Mode
		if mode == "" {
			mode = speech.PackBundled
		}
		dir := filepath.Join(o.OutDir, pack)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		man := &speech.ClipManifest{Version: 1, Rate: o.Rate, Voices: map[string]speech.VoiceStats{}}
		voices := []string{}
		for k := range groups {
			if k.pack == pack {
				voices = append(voices, k.voice)
			}
		}
		sort.Strings(voices)
		var bytes int64
		for _, voice := range voices {
			clips := groups[packKey{pack, voice}]
			var atlas []float64
			idx := len(man.Atlases)
			st := speech.VoiceStats{MaxTruePeakDBTP: -200}
			var lufsSum float64
			for _, cl := range clips {
				atlas = append(atlas, make([]float64, guard)...)
				man.Clips = append(man.Clips, speech.ClipEntry{Key: cl.req.Key, Variant: cl.req.Variant, Atlas: idx, Offset: len(atlas), Frames: len(cl.audio.Samples)})
				atlas = append(atlas, cl.audio.Samples...)
				st.Clips++
				st.Characters += len([]rune(cl.req.Text))
				lufsSum += cl.rep.LUFS
				st.MaxTruePeakDBTP = math.Max(st.MaxTruePeakDBTP, cl.rep.TruePeakDB)
			}
			atlas = append(atlas, make([]float64, guard)...)
			st.MeanLUFS = math.Round(lufsSum/float64(len(clips))*100) / 100
			st.MaxTruePeakDBTP = math.Round(st.MaxTruePeakDBTP*100) / 100
			man.Voices[voice] = st

			wav := loudness.WAV16(loudness.Audio{Rate: o.Rate, Samples: atlas})
			file, data := voice+".wav", wav
			if o.Codec == "opus" {
				var err error
				if data, err = encodeOpus(ctx, wav); err != nil {
					return nil, err
				}
				file = voice + ".ogg"
			}
			if err := writeFile(filepath.Join(dir, file), data); err != nil {
				return nil, err
			}
			bytes += int64(len(data))
			sum := sha256.Sum256(data)
			man.Atlases = append(man.Atlases, speech.Atlas{File: file, Voice: voice, Frames: len(atlas), SHA256: hex.EncodeToString(sum[:])})
		}
		b, err := json.MarshalIndent(man, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := writeFile(filepath.Join(dir, ManifestFile), append(b, '\n')); err != nil {
			return nil, err
		}
		res.Packs = append(res.Packs, PackResult{Name: pack, Mode: mode, Manifest: man, Bytes: bytes, Budget: spec.BudgetBytes})
		index.Packs = append(index.Packs, speech.PackEntry{Name: pack, Mode: mode, Manifest: pack + "/" + ManifestFile, Bytes: bytes, BudgetBytes: spec.BudgetBytes, Clips: len(man.Clips)})
		if spec.BudgetBytes > 0 && bytes > spec.BudgetBytes {
			overBudget = append(overBudget, fmt.Sprintf("%s is %d bytes, budget %d", pack, bytes, spec.BudgetBytes))
		}
	}
	b, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeFile(filepath.Join(o.OutDir, PackIndexFile), append(b, '\n')); err != nil {
		return nil, err
	}
	if len(overBudget) > 0 {
		return res, fmt.Errorf("%w: %s", ErrOverBudget, strings.Join(overBudget, "; "))
	}
	return res, nil
}

func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// encodeOpus compresses a mono 48 kHz WAV to ogg opus with ffmpeg.
func encodeOpus(ctx context.Context, wav []byte) ([]byte, error) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("voicerender: the opus codec needs ffmpeg on PATH")
	}
	cmd := exec.CommandContext(ctx, ff, "-v", "error", "-f", "wav", "-i", "pipe:0", "-c:a", "libopus", "-b:a", "32k", "-application", "voip", "-f", "ogg", "pipe:1")
	cmd.Stdin = bytesReader(wav)
	var out, stderr limitedBuffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("voicerender: ffmpeg failed: %w: %.200s", err, stderr.b)
	}
	return out.b, nil
}
