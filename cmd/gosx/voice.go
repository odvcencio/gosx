package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"m31labs.dev/gosx/game/audio/speech"
	"m31labs.dev/gosx/game/audio/speech/loudness"
	"m31labs.dev/gosx/game/audio/speech/voicerender"
)

func cmdVoice() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := runVoiceCommand(ctx, os.Args[2:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "voice error: %v\n", err)
		os.Exit(1)
	}
}

func voiceUsage(w io.Writer) {
	fmt.Fprint(w, `gosx voice - Build commentary voice clips for a game

Usage:
  gosx voice fragments --grammar g.json --voice name [--pack p]
                         list the phrases a grammar needs, as render-manifest JSON
  gosx voice booth --manifest m.json --takes dir [--voice v] [--reference local] [--port n]
                         open a recording prompter on 127.0.0.1 for one speaker
  gosx voice import --manifest m.json --takes dir --voice v (--dir d | --file f.wav) [--slice silence|claps]
                         bring in takes recorded in a DAW
  gosx voice coverage --manifest m.json --takes dir [--strict]
                         list the phrases and variants still unrecorded
  gosx voice estimate --manifest m.json [--provider p] [--cache dir] [--price-per-1k n] [--json]
                         count characters per voice; makes no calls
  gosx voice render --manifest m.json --out dir [--takes dir] [--cache dir] [--provider p]
                         trim, normalize and pack clips into voice packs

render flags: --rate 48000 --target-lufs -23 --ceiling-dbtp -1 --codec wav|opus
  --jobs 2 --max-chars n (required for ElevenLabs) --allow-missing

Providers: takes (recorded files), local (say or espeak-ng, for development),
elevenlabs (key from the ELEVENLABS_API_KEY environment variable only), mock.
The manifest's per-voice "provider" wins over --provider.
See docs/audio-speech.md.
`)
}

func runVoiceCommand(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) || args[0] == "help" {
		voiceUsage(stdout)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "fragments":
		return voiceFragments(rest, stdout)
	case "booth":
		return voiceBooth(ctx, rest, stdout)
	case "import":
		return voiceImport(rest, stdout)
	case "coverage":
		return voiceCoverage(rest, stdout)
	case "estimate":
		return voiceEstimate(rest, stdout)
	case "render":
		return voiceRender(ctx, rest, stdout)
	}
	return fmt.Errorf("unknown voice subcommand %q\nrun 'gosx voice help' for usage", sub)
}

func loadRenderManifest(path string) (*speech.RenderManifest, error) {
	if path == "" {
		return nil, fmt.Errorf("--manifest is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return speech.ParseRenderManifest(data)
}

func voiceFragments(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("voice fragments", flag.ContinueOnError)
	grammar := fs.String("grammar", "", "grammar JSON")
	voice := fs.String("voice", "", "voice name for every phrase")
	pack := fs.String("pack", "", "pack name for every phrase")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *grammar == "" || *voice == "" {
		return fmt.Errorf("--grammar and --voice are required")
	}
	data, err := os.ReadFile(*grammar)
	if err != nil {
		return err
	}
	g, err := speech.ParseGrammar(data)
	if err != nil {
		return err
	}
	c, err := g.Compile(speech.NewLibrary(48000))
	if err != nil {
		return err
	}
	phrases := c.RenderPhrases(*voice)
	for i := range phrases {
		phrases[i].Pack = *pack
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"phrases": phrases})
}

func voiceBooth(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("voice booth", flag.ContinueOnError)
	manifest := fs.String("manifest", "", "render manifest")
	takes := fs.String("takes", "", "takes directory")
	voice := fs.String("voice", "", "speaker (a voice in the manifest)")
	ref := fs.String("reference", "none", "reference read provider: local or none")
	port := fs.Int("port", 0, "port on 127.0.0.1 (0 picks one)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := loadRenderManifest(*manifest)
	if err != nil {
		return err
	}
	if *takes == "" {
		return fmt.Errorf("--takes is required")
	}
	var refProv voicerender.Provider
	switch *ref {
	case "none":
	case "local":
		refProv = &voicerender.Local{}
	default:
		return fmt.Errorf("--reference must be local or none")
	}
	b, err := voicerender.NewBooth(m, &voicerender.Takes{Dir: *takes}, *voice, refProv)
	if err != nil {
		return err
	}
	ln, err := b.Listen(*port)
	if err != nil {
		return err
	}
	srv := &boothServer{ln: ln, handler: b.Handler()}
	url := fmt.Sprintf("http://%s/?token=%s", ln.Addr(), b.Token())
	if *voice != "" {
		url += "&voice=" + *voice
	}
	fmt.Fprintf(stdout, "Recording booth: open %s\nTakes go to %s. Press Ctrl-C when you are done.\n", url, *takes)
	return srv.serve(ctx)
}

func voiceImport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("voice import", flag.ContinueOnError)
	manifest := fs.String("manifest", "", "render manifest")
	takes := fs.String("takes", "", "takes directory")
	voice := fs.String("voice", "", "voice the takes belong to")
	dir := fs.String("dir", "", "directory of one WAV per phrase, named by stem")
	file := fs.String("file", "", "one long WAV read in manifest order")
	slice := fs.String("slice", "silence", "how to cut --file: silence or claps")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := loadRenderManifest(*manifest)
	if err != nil {
		return err
	}
	if *takes == "" || *voice == "" || (*dir == "") == (*file == "") {
		return fmt.Errorf("--takes, --voice and exactly one of --dir or --file are required")
	}
	t := &voicerender.Takes{Dir: *takes}
	if *dir != "" {
		rep, err := t.ImportDir(m, *voice, *dir)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "imported %d takes\n", len(rep.Imported))
		for _, u := range rep.Unmatched {
			fmt.Fprintf(stdout, "no phrase matches %s\n", u)
		}
		return nil
	}
	data, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	a, err := loudness.ParseWAV(data)
	if err != nil {
		return err
	}
	reqs := t.Pending(m, *voice)
	if err := t.ImportLong(a, reqs, voicerender.SliceMode(*slice)); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "imported %d takes in manifest order\n", len(reqs))
	return nil
}

func voiceCoverage(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("voice coverage", flag.ContinueOnError)
	manifest := fs.String("manifest", "", "render manifest")
	takes := fs.String("takes", "", "takes directory")
	strict := fs.Bool("strict", false, "exit with an error when anything is missing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := loadRenderManifest(*manifest)
	if err != nil {
		return err
	}
	rep := (&voicerender.Takes{Dir: *takes}).Coverage(m)
	fmt.Fprint(stdout, rep.String())
	if *strict && rep.MissingCount > 0 {
		return fmt.Errorf("%d takes missing", rep.MissingCount)
	}
	return nil
}

func voiceEstimate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("voice estimate", flag.ContinueOnError)
	manifest := fs.String("manifest", "", "render manifest")
	provider := fs.String("provider", "", "provider for voices that name none")
	cacheDir := fs.String("cache", "", "cache directory (cached text is not billable)")
	price := fs.Float64("price-per-1k", 0, "price per 1000 characters, in your currency")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := loadRenderManifest(*manifest)
	if err != nil {
		return err
	}
	est := voicerender.EstimateManifest(m, voicerender.EstimateOptions{DefaultProvider: *provider, CacheDir: *cacheDir, PricePer1K: *price})
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(est)
	}
	fmt.Fprintf(stdout, "%-16s %-12s %8s %8s %10s %8s %9s\n", "voice", "provider", "requests", "unique", "characters", "cached", "billable")
	for _, v := range append(est.Voices, est.Total) {
		fmt.Fprintf(stdout, "%-16s %-12s %8d %8d %10d %8d %9d\n", v.Voice, v.Provider, v.Requests, v.Unique, v.Characters, v.Cached, v.Billable)
	}
	if *price > 0 {
		fmt.Fprintf(stdout, "estimated cost: %.2f at %.4f per 1000 characters (check your plan's current price)\n", est.Cost, *price)
	}
	return nil
}

func voiceRender(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("voice render", flag.ContinueOnError)
	manifest := fs.String("manifest", "", "render manifest")
	out := fs.String("out", "", "output directory")
	takes := fs.String("takes", "", "takes directory (enables the takes provider)")
	cacheDir := fs.String("cache", ".voice-cache", "provider audio cache; keep it out of the shipped game")
	provider := fs.String("provider", "", "provider for voices that name none (default: takes if --takes is set, else local)")
	rate := fs.Int("rate", 48000, "output sample rate")
	lufs := fs.Float64("target-lufs", -23, "loudness target in LUFS")
	ceil := fs.Float64("ceiling-dbtp", -1, "true-peak ceiling in dBTP")
	codec := fs.String("codec", "wav", "wav or opus (ffmpeg)")
	jobs := fs.Int("jobs", 2, "concurrent provider calls")
	maxChars := fs.Int("max-chars", 0, "billable character budget; required for a paid provider")
	allow := fs.Bool("allow-missing", false, "render a partial pack when takes are missing")
	quiet := fs.Bool("quiet", false, "no per-phrase progress")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, err := loadRenderManifest(*manifest)
	if err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("--out is required")
	}
	def := *provider
	if def == "" {
		def = "local"
		if *takes != "" {
			def = "takes"
		}
	}
	used := map[string]bool{def: false}
	for _, v := range m.Voices {
		if v.Provider != "" {
			used[v.Provider] = true
		}
	}
	// Only providers a voice uses are built, so a missing ElevenLabs key
	// does not block a local or takes render.
	provs := map[string]voicerender.Provider{"mock": &voicerender.Mock{}, "local": &voicerender.Local{}}
	if *takes != "" {
		provs["takes"] = &voicerender.Takes{Dir: *takes}
	}
	for n := range used {
		if n == "elevenlabs" {
			e, err := voicerender.NewElevenLabs(os.Getenv)
			if err != nil {
				return err
			}
			provs["elevenlabs"] = e
		}
	}
	o := voicerender.Options{
		Manifest: m, OutDir: *out, CacheDir: *cacheDir, Providers: provs, DefaultProvider: def,
		Rate: *rate, TargetLUFS: *lufs, CeilingDBTP: *ceil, Codec: *codec, Jobs: *jobs, MaxChars: *maxChars, AllowMissing: *allow,
	}
	if !*quiet {
		o.Progress = stdout
	}
	res, err := voicerender.Render(ctx, o)
	if res != nil {
		for _, p := range res.Packs {
			budget := "no budget"
			if p.Budget > 0 {
				budget = fmt.Sprintf("budget %d", p.Budget)
			}
			fmt.Fprintf(stdout, "pack %-12s %-8s %3d clips %9d bytes (%s)\n", p.Name, p.Mode, len(p.Manifest.Clips), p.Bytes, budget)
		}
		fmt.Fprintf(stdout, "%d provider calls, %d cache hits, %d takes missing; index %s\n", res.Calls, res.CacheHits, len(res.Missing), filepath.Join(*out, voicerender.PackIndexFile))
	}
	return err
}
