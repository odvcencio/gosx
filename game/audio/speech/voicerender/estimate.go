package voicerender

import (
	"sort"
	"unicode/utf8"

	"m31labs.dev/gosx/game/audio/speech"
)

// VoiceEstimate is the character count one voice costs to render.
type VoiceEstimate struct {
	Voice    string `json:"voice"`
	Provider string `json:"provider"`
	// Requests counts phrase variants; Unique counts distinct texts, which is
	// what the renderer actually sends (it sends each once).
	Requests int `json:"requests"`
	Unique   int `json:"unique"`
	// Characters is the sum of text lengths in characters (Unicode code
	// points) over unique texts. Cached is the part already in the cache;
	// Billable is Characters - Cached, and is zero for free providers.
	Characters int `json:"characters"`
	Cached     int `json:"cached"`
	Billable   int `json:"billable"`
}

// Estimate is the report for a whole manifest.
type Estimate struct {
	Voices []VoiceEstimate `json:"voices"`
	Total  VoiceEstimate   `json:"total"`
	// Cost is Billable/1000 * PricePer1K when a price was given, else 0.
	Cost float64 `json:"cost,omitempty"`
}

// EstimateOptions configures Estimate.
type EstimateOptions struct {
	// DefaultProvider names the provider for voices that set none.
	DefaultProvider string
	// Providers lets the estimator ask whether a provider bills. Optional;
	// "elevenlabs" is treated as paid when absent.
	Providers map[string]Provider
	// CacheDir, when set, discounts texts already cached.
	CacheDir string
	// PricePer1K is the price per 1000 characters, in the caller's currency.
	// The tool does not know current pricing; pass the figure you pay.
	PricePer1K float64
}

// EstimateManifest counts the characters each voice would send. It makes no
// calls and reads only the cache directory.
func EstimateManifest(m *speech.RenderManifest, opt EstimateOptions) Estimate {
	c := cache{dir: opt.CacheDir}
	type seenKey struct{ voice, fp string }
	seen := map[seenKey]bool{}
	byVoice := map[string]*VoiceEstimate{}
	for _, r := range m.Requests() {
		v := m.Voices[r.Voice]
		prov := v.Provider
		if prov == "" {
			prov = opt.DefaultProvider
		}
		e := byVoice[r.Voice]
		if e == nil {
			e = &VoiceEstimate{Voice: r.Voice, Provider: prov}
			byVoice[r.Voice] = e
		}
		e.Requests++
		fp := fingerprint(prov, v, r.Text)
		k := seenKey{r.Voice, fp}
		if seen[k] {
			continue
		}
		seen[k] = true
		n := utf8.RuneCountInString(r.Text)
		e.Unique++
		e.Characters += n
		if c.has(fp) {
			e.Cached += n
		}
	}
	var out Estimate
	names := make([]string, 0, len(byVoice))
	for n := range byVoice {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		e := byVoice[n]
		paid := e.Provider == "elevenlabs"
		if p, ok := opt.Providers[e.Provider]; ok {
			paid = isPaid(p)
		}
		if paid {
			e.Billable = e.Characters - e.Cached
		}
		out.Voices = append(out.Voices, *e)
		out.Total.Requests += e.Requests
		out.Total.Unique += e.Unique
		out.Total.Characters += e.Characters
		out.Total.Cached += e.Cached
		out.Total.Billable += e.Billable
	}
	out.Total.Voice = "total"
	if opt.PricePer1K > 0 {
		out.Cost = float64(out.Total.Billable) / 1000 * opt.PricePer1K
	}
	return out
}
