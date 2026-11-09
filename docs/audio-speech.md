# Speech: stitched commentary for games

GoSX can speak play-by-play the way classic sports games did: pre-recorded
fragments ("takes the lead", "Okafor", "forty", "seven") stitched into a
sentence when a game event happens. This page covers the runtime
(`game/audio/speech`), the recording tools (`gosx voice`) and a worked example.
It carries no game content; the example is a small racing announcer.

All speech plays on the mixer's `voice` bus (see [audio.md](audio.md)) and
ducks the `music` and `sfx` buses while a line is on air.

```text
game events ─► Planner ─► Utterance ─► Sequencer ─► Player ─► voice bus
 (fields)     grammar,    fragment     frame-exact   WebAudio   (ducks
              cooldowns   IDs          timeline      sources    music, sfx)
```

## The pieces

| Piece | Package | What it does |
|---|---|---|
| Grammar | `speech` | Templates with slots, weighted variants, conditions. |
| Planner | `speech` | Turns an `Event` into an `Utterance`: priority, interruptibility, cooldowns, a recent-history window, a seeded draw. |
| Library | `speech` | Maps fragment keys to recordings. |
| Sequencer | `speech` | Lays an utterance on a timeline in whole sample frames. |
| Player | `speech/voice` | Schedules the timeline on the voice bus. |
| Render tool | `gosx voice`, `speech/voicerender` | Records, imports, cleans and packs the clips. |

## 1. Write a grammar

A phrase answers one event kind. Each variant is a template:

```json
{
  "pools": {
    "driver": ["okafor", "vance", {"key": "reyes", "text": "Reyes"}],
    "corner": ["one", "hairpin", "chicane"]
  },
  "number_max": 99,
  "categories": {"chatter": {"cooldown": 4}},
  "phrases": [
    {"id": "lead.change", "event": "lead_change", "priority": 5, "cooldown": 3, "max_wait": 3,
     "variants": [
       {"say": "{driver} takes the lead", "weight": 3},
       {"say": "and it is {driver} out front now"},
       {"say": "{num:number} takes the lead", "when": [{"field": "driver", "op": "lacks"}]}
     ]},
    {"id": "lap.call", "event": "lap", "category": "chatter", "priority": 1, "cooldown": 8,
     "variants": [{"say": "lap {lap:number} of {total:number}"}]},
    {"id": "crash", "event": "crash", "priority": 9, "uninterruptible": true,
     "variants": [{"say": "oh no! {driver} is in the wall at the {corner}"}]}
  ]
}
```

Template syntax:

| Token | Meaning |
|---|---|
| `{driver}` | Fill from event field `driver` with the pool of the same name. |
| `{who:driver}` | Fill from field `who` with pool `driver`. |
| `{lap:number}` | Speak field `lap` as a number, 0 to `number_max` (at most 999). |
| `{corner?}` | Optional slot: skipped when unbound. |
| `{~150}` | Pause of 150 ms after the previous fragment. |

A variant whose slots cannot bind is not a candidate. That is how a team with
blank nameplates is called by number: the `lacks` variant above wins when the
event has no `driver` name. Conditions read the event's fields first, then the
planner's context (`planner.Context().Num("margin", 7)`). Ops are `==`, `!=`,
`>`, `>=`, `<`, `<=`, `has` and `lacks`; `has` means a non-empty string or a
non-zero number.

Compile it against a `Library`. Compiling interns every fragment the grammar
needs:

```go
g, _ := speech.ParseGrammar(grammarJSON)
lib := speech.NewLibrary(48000)
compiled, err := g.Compile(lib)
planner := speech.NewPlanner(compiled, speech.PlannerConfig{Seed: matchSeed})
```

## 2. Plan and play

```go
var ev speech.Event
ev.Reset("lead_change")
ev.Str("driver", "okafor")

d := planner.Submit(&ev, now, player.Playing())   // now: seconds, any steady clock
player.Say(&d)                                    // plays, interrupts, or does nothing
...
// each frame:
if d := planner.Poll(now, player.Playing()); d.Action != speech.Drop {
    player.Say(&d)
}
player.Update()
```

`Submit` returns one of:

| Action | Meaning |
|---|---|
| `Play` | The voice was idle. |
| `Interrupt` | The event outranks the line on air, and that line is interruptible. The old line fades out over 12 ms and the new one starts. |
| `Queued` | The voice is busy and the phrase has `max_wait`. `Poll` returns it when the voice frees up, or drops it after `max_wait` seconds. |
| `Drop` | `Reason` says why: no match, cooldown, unbound slot, busy, queue full. |

Selection uses a seeded generator, so one seed and one event stream give the
same speech on every machine. The last `History` (default 6) phrase variants
are kept out of the draw unless nothing else qualifies. A phrase's `cooldown`
and its category's `cooldown` are in the planner's clock seconds.

`Event` is a plain value with room for 12 fields, and `Planner.Submit`,
`Planner.Poll`, `Sequencer.Play` and `Sequencer.Interrupt` do not allocate in
steady state (tests check it with `testing.AllocsPerRun`). The browser still
creates one single-use `AudioBufferSourceNode` and gain node per fragment,
because the WebAudio API requires it.

### Pitch variants and timing

Each fragment can have three recordings: `start`, `mid` and `end`. The
sequencer picks by position: the first fragment of a sentence takes `start`,
the last takes `end`, the rest take `mid`, and a single fragment takes `end`.
A pause of 150 ms or more (`ClauseGapMs`) ends a clause, so words before it
take `end` and words after it take `start`. A missing variant falls back to
`mid`, then `end`, then `start`.

Fragments join with a crossfade (default 8 ms). With `GapMs` above zero they
leave that much silence instead. All times are whole sample frames; the
player converts them to audio-clock seconds only when it calls WebAudio.
`player.Status()` reports the phrase, the sounding fragment and the time
left.

## 3. Voice packs

Group phrases into packs in the render manifest and give each a size budget:

```json
"packs": {
  "pa":    {"mode": "bundled", "budget_bytes": 3000000},
  "booth": {"mode": "stream", "budget_bytes": 40000000},
  "names": {"mode": "stream", "budget_bytes": 8000000}
}
```

`bundled` packs ship with the game and load at boot. `stream` packs load on
demand, for example a booth voice or the names for one matchup. `gosx voice
render` fails with `ErrOverBudget` (naming the pack) when a pack's audio files
exceed its budget; it still writes the files so you can see the sizes. The
output is:

```text
out/speech.packs.json        index of packs
out/pa/speech.clips.json     one clip manifest per pack
out/pa/<voice>.wav|.ogg      one atlas per voice
```

Load at runtime:

```go
idx, _ := speech.ParsePackIndex(indexJSON)
for _, e := range idx.Bundled() {
    voice.LoadPack(host, lib, e, fetch)   // fetch(path) returns file bytes
}
// later, when the match is known:
e, _ := idx.Find("names")
voice.LoadPack(host, lib, e, fetch)
```

A size note for planning: 16-bit mono WAV at 48 kHz is 96 KB per second.
Use `--codec opus` (needs `ffmpeg`) for shipping, and render only the variants
a fragment needs (`"variants": ["end"]`).

## 4. Record the voices

The primary path is a person recording in a booth. `gosx voice fragments`
lists what the grammar needs:

```sh
gosx voice fragments --grammar grammar.json --voice announcer --pack pa > phrases.json
```

Wrap the output in a render manifest (voices, packs, optional variant
templates) and save it as `voices.json`:

```json
{
  "voices": {"announcer": {"provider": "takes"}},
  "packs": {"pa": {"mode": "bundled", "budget_bytes": 3000000}},
  "phrases": [
    {"key": "lit/takes the lead", "text": "takes the lead", "voice": "announcer", "pack": "pa"},
    {"key": "pool/driver/okafor", "text": "okafor", "voice": "announcer", "pack": "pa", "variants": ["end"]},
    {"key": "num/seven", "text": "seven", "voice": "announcer", "pack": "pa"}
  ]
}
```

### Prompter

```sh
gosx voice booth --manifest voices.json --takes takes --voice announcer --reference local
```

The command prints a `http://127.0.0.1:PORT/?token=...` address. Open it in
Chrome. The page shows one line at a time: the phrase key, its text, the
pitch variant to perform (start, middle or end, with a one-line direction), a
level meter, and a list of all lines. Keys: space records and stops, `r`
retakes, `p` plays the take back, `f` plays a reference read (a synthetic
voice, only when `--reference local`), `s` skips, `n` and `b` move. A take is
saved the moment you stop. Echo cancellation, noise suppression and automatic
gain are turned off so the take is the raw mic signal.

The server listens only on 127.0.0.1, requires the random token on every API
call, and stops when you press Ctrl-C.

### Takes recorded in a DAW

Either one file per phrase, named by its stem (the key lower-cased with other
characters turned into `_`, so `lit/takes the lead` is `lit_takes_the_lead`):

```sh
gosx voice import --manifest voices.json --takes takes --voice announcer --dir daw-export/
```

`lit_takes_the_lead.wav` serves every variant of that phrase;
`lit_takes_the_lead.end.wav` serves only the end variant.

Or one long recording read in manifest order, cut by pauses or by hand claps:

```sh
gosx voice import --manifest voices.json --takes takes --voice announcer \
    --file session.wav --slice silence   # a pause of about half a second between lines
gosx voice import ... --file session.wav --slice claps   # clap, read, clap, read
```

The importer cuts the file into spans and requires exactly as many spans as
there are unrecorded lines for that voice. On a mismatch it writes nothing and
says how many it found, so one missed or doubled line cannot shift every later
take. It does not recognize speech, so spoken IDs are not supported; use claps
or pauses.

### Coverage

```sh
gosx voice coverage --manifest voices.json --takes takes [--strict]
```

lists, per voice and pack, how many lines are recorded and which `key/variant`
pairs are missing. `--strict` exits non-zero when any is missing.

### Render

```sh
gosx voice render --manifest voices.json --takes takes --out dist/voice \
    --target-lufs -23 --ceiling-dbtp -1 [--allow-missing] [--codec opus]
```

Every clip, whether a person recorded it or a provider spoke it, goes through
the same steps: resample to 48 kHz, remove DC offset, repair isolated
one-sample pops, trim silence (below -50 dBFS, keeping 6 ms), gain to the
loudness target (ITU-R BS.1770-4 integrated loudness; a clip under 400 ms is
measured as one block), limit sample peaks, hold the true peak (4x
oversampled) at or below the ceiling, and fade the edges by 1.5 ms. The clip
manifest records mean loudness and maximum true peak per voice. A clip with a
sparse peak can end up quieter than the target because the limiter will not
raise the peak; the recorded loudness shows it.

`--allow-missing` renders a partial pack while recording is under way and
lists what was left out.

The de-clicker repairs single-sample pops only. It does not remove mouth
noise, breaths or room tone; those are for the editor.

## 5. Optional: synthetic voices

A voice can name another provider instead of `takes`: `local` (macOS `say`, or
`espeak-ng` or `espeak` where installed; for development only), `elevenlabs`,
or `mock` (tests). ElevenLabs suits a bounded set such as a surname pool.

- The key comes only from the `ELEVENLABS_API_KEY` environment variable, read
  when the command starts. It is never written to a file, a log, an error
  message or the output, and the shipped game contains no key and makes no
  calls.
- Rendering with a paid provider needs `--max-chars N`; the tool refuses when
  the characters it would send exceed N.
- Provider audio is cached under `--cache` (default `.voice-cache`, outside the
  shipped output) by the hash of provider, voice settings and text, so a rerun
  makes no calls. Changing the loudness target does not re-bill.

Estimate before spending. The tool does not know current prices, so pass your
own:

```sh
gosx voice estimate --manifest voices.json --provider elevenlabs --cache .voice-cache --price-per-1k 0.30
```

```text
voice      provider    requests unique characters cached billable
announcer  elevenlabs        22     22        185      0      185
total                        25     25        224      0      224
```

Characters are Unicode code points of the text sent, after the variant
templates add punctuation (`start` adds a comma, `end` a full stop; edit
`variants` in the manifest to change them). Whether those punctuation cues
give the intended rising, level and falling reads depends on the voice; listen
before you render a large library.

## What the tests cover

- Grammar, planner, binding, cooldowns, history, queue and interrupt rules:
  `go test ./game/audio/speech/`.
- Sequencer timing against a PCM reference, and zero allocations on the event
  path: `go test ./game/audio/speech/`.
- The voice player against a model of WebAudio scheduling:
  `go test ./game/audio/speech/voice/`.
- Loudness, true peak, limiter, trim, de-click, slicing, resampling:
  `go test ./game/audio/speech/loudness/`.
- Render, cache, packs and budgets, takes, import, coverage, the booth server,
  the estimator, and ElevenLabs against a local fake server:
  `go test ./game/audio/speech/voicerender/` (no paid call is ever made).
- Real-browser checks (Apple M4 Pro Chrome through `mac-gpu-run`):
  `go test -tags e2e -run 'TestSpeechTimeline|TestRecordingBooth' ./e2e/` renders
  a timeline in an `OfflineAudioContext` and compares it with the reference
  mix, and records a take through the booth page with `getUserMedia` and the
  AudioWorklet recorder.
