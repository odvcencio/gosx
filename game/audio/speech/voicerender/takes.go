package voicerender

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
)

// Takes is a directory of recorded takes, one WAV per phrase variant:
//
//	<Dir>/<voice>/<stem>.<variant>.wav   e.g. announcer/lit_takes_the_lead.end.wav
//	<Dir>/<voice>/<stem>.wav             one read used for every variant that has no file of its own
//
// where <stem> is speech.Stem(key). As a provider named "takes" it feeds
// Render with real recordings; the booth, the importers and a DAW export all
// write into it.
type Takes struct{ Dir string }

// Name implements Provider.
func (t *Takes) Name() string { return "takes" }

// Synthesize implements Provider. Takes cannot speak text; Render uses Take.
func (t *Takes) Synthesize(context.Context, speech.Voice, string) (loudness.Audio, error) {
	return loudness.Audio{}, errors.New("voicerender: takes hold recordings; they cannot speak text")
}

func (t *Takes) path(voice, key, variant string) string {
	stem := speech.Stem(key)
	if variant == "" {
		return filepath.Join(t.Dir, voice, stem+".wav")
	}
	return filepath.Join(t.Dir, voice, stem+"."+variant+".wav")
}

// Take implements RequestProvider.
func (t *Takes) Take(req speech.Request) (loudness.Audio, bool, error) {
	for _, p := range []string{t.path(req.Voice, req.Key, req.Variant), t.path(req.Voice, req.Key, "")} {
		data, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return loudness.Audio{}, false, err
		}
		a, err := loudness.ParseWAV(data)
		if err != nil {
			return loudness.Audio{}, false, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		return a, true, nil
	}
	return loudness.Audio{}, false, nil
}

// Has reports whether a take exists for req (its own file or the shared read).
func (t *Takes) Has(req speech.Request) bool {
	for _, p := range []string{t.path(req.Voice, req.Key, req.Variant), t.path(req.Voice, req.Key, "")} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// Save stores a take for one variant, replacing an earlier one. The audio is
// kept at its recorded rate, as 16-bit mono.
func (t *Takes) Save(req speech.Request, a loudness.Audio) error {
	if len(a.Samples) == 0 {
		return errors.New("voicerender: empty take")
	}
	p := t.path(req.Voice, req.Key, req.Variant)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return writeFile(p, loudness.WAV16(a))
}

// Pending lists the requests for voice (empty: every voice) that have no take
// yet, in manifest order.
func (t *Takes) Pending(m *speech.RenderManifest, voice string) []speech.Request {
	var out []speech.Request
	for _, r := range m.Requests() {
		if (voice == "" || r.Voice == voice) && !t.Has(r) {
			out = append(out, r)
		}
	}
	return out
}

// ImportReport says what an import did.
type ImportReport struct {
	Imported  []string // "key/variant"
	Unmatched []string // files that matched no request
}

// ImportDir takes one WAV per phrase from src, named by stem: stem.wav for a
// read that serves every variant, or stem.variant.wav. Only requests of voice
// are matched. Nothing is written when the directory holds no usable file.
func (t *Takes) ImportDir(m *speech.RenderManifest, voice, src string) (ImportReport, error) {
	var rep ImportReport
	if _, ok := m.Voices[voice]; !ok {
		return rep, fmt.Errorf("voicerender: unknown voice %q", voice)
	}
	byName := map[string]speech.Request{} // "stem" or "stem.variant" -> request
	stems := map[string]string{}          // stem -> key
	for _, r := range m.Requests() {
		if r.Voice != voice {
			continue
		}
		byName[speech.Stem(r.Key)+"."+r.Variant] = r
		stems[speech.Stem(r.Key)] = r.Key
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return rep, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.EqualFold(filepath.Ext(name), ".wav") {
			continue
		}
		base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			return rep, err
		}
		a, err := loudness.ParseWAV(data)
		if err != nil {
			return rep, fmt.Errorf("%s: %w", name, err)
		}
		if r, ok := byName[base]; ok {
			if err := t.Save(r, a); err != nil {
				return rep, err
			}
			rep.Imported = append(rep.Imported, r.Key+"/"+r.Variant)
			continue
		}
		if key, ok := stems[base]; ok {
			// A bare stem serves every variant that has no file of its own.
			if err := os.MkdirAll(filepath.Join(t.Dir, voice), 0o755); err != nil {
				return rep, err
			}
			if err := writeFile(t.path(voice, key, ""), loudness.WAV16(a)); err != nil {
				return rep, err
			}
			rep.Imported = append(rep.Imported, key+"/*")
			continue
		}
		rep.Unmatched = append(rep.Unmatched, name)
	}
	sort.Strings(rep.Imported)
	sort.Strings(rep.Unmatched)
	return rep, nil
}

// SliceMode selects how ImportLong finds take boundaries.
type SliceMode string

const (
	// SliceSilence cuts at pauses. The reader leaves a clear pause (about half
	// a second) between lines.
	SliceSilence SliceMode = "silence"
	// SliceClaps cuts at hand claps: clap, read the line, clap, read the next.
	SliceClaps SliceMode = "claps"
)

// ImportLong cuts one long recording into takes for reqs, which must be the
// lines in the order they were read. The number of slices must equal
// len(reqs); otherwise ImportLong writes nothing and says how many it found,
// so a missed or doubled line is caught before it shifts every later take.
func (t *Takes) ImportLong(a loudness.Audio, reqs []speech.Request, mode SliceMode) error {
	var spans []loudness.Span
	switch mode {
	case SliceSilence, "":
		spans = loudness.SliceBySilence(a, -45, 350, 60)
	case SliceClaps:
		spans = loudness.SliceByClaps(a, -6, 60)
	default:
		return fmt.Errorf("voicerender: unknown slice mode %q", mode)
	}
	if len(spans) != len(reqs) {
		return fmt.Errorf("voicerender: found %d takes in the recording but expected %d; check for a missed, doubled or merged line (nothing was imported)", len(spans), len(reqs))
	}
	for i, sp := range spans {
		if err := t.Save(reqs[i], loudness.Cut(a, sp)); err != nil {
			return err
		}
	}
	return nil
}

// CoverageLine counts one voice and pack.
type CoverageLine struct {
	Voice, Pack string
	Total, Have int
	// Missing lists "key/variant" of the unrecorded takes.
	Missing []string
}

// CoverageReport is the booth's to-do list.
type CoverageReport struct {
	Lines        []CoverageLine
	Total, Have  int
	MissingCount int
}

// Coverage reports which phrases and variants have a take.
func (t *Takes) Coverage(m *speech.RenderManifest) CoverageReport {
	type k struct{ voice, pack string }
	by := map[k]*CoverageLine{}
	var rep CoverageReport
	for _, r := range m.Requests() {
		l := by[k{r.Voice, r.Pack}]
		if l == nil {
			l = &CoverageLine{Voice: r.Voice, Pack: r.Pack}
			by[k{r.Voice, r.Pack}] = l
		}
		l.Total++
		rep.Total++
		if t.Has(r) {
			l.Have++
			rep.Have++
		} else {
			l.Missing = append(l.Missing, r.Key+"/"+r.Variant)
		}
	}
	for _, l := range by {
		rep.Lines = append(rep.Lines, *l)
	}
	sort.Slice(rep.Lines, func(i, j int) bool {
		if rep.Lines[i].Voice != rep.Lines[j].Voice {
			return rep.Lines[i].Voice < rep.Lines[j].Voice
		}
		return rep.Lines[i].Pack < rep.Lines[j].Pack
	})
	rep.MissingCount = rep.Total - rep.Have
	return rep
}

// String formats the report for a terminal.
func (r CoverageReport) String() string {
	var b strings.Builder
	for _, l := range r.Lines {
		fmt.Fprintf(&b, "%-16s %-16s %d/%d recorded\n", l.Voice, l.Pack, l.Have, l.Total)
		for _, m := range l.Missing {
			fmt.Fprintf(&b, "    missing %s\n", m)
		}
	}
	fmt.Fprintf(&b, "total: %d of %d recorded, %d missing\n", r.Have, r.Total, r.MissingCount)
	return b.String()
}
