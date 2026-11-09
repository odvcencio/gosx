// Package speech builds spoken commentary for games the way classic sports
// games did: pre-recorded phrase fragments stitched together live.
//
// The package has four parts.
//
//   - A phrase grammar (Grammar) of templates with slots, weighted
//     alternatives and conditions. Compile turns it into an immutable
//     Compiled value that shares nothing mutable, so one Compiled can serve
//     many Planners.
//   - A Planner that turns game events into Utterances. It applies priority,
//     interruptibility, per-phrase and per-category cooldowns and a
//     recent-history window, and it picks among alternatives with a seeded
//     generator so the same seed and event stream give the same speech.
//   - Slot binding. A slot is filled from an event field by a bounded pool
//     of recorded fragments (for example a fixed surname list) or by the
//     number binder, which speaks 0-999 from about 30 clips.
//   - A Sequencer that lays an Utterance's fragments on a timeline in whole
//     sample frames, with short crossfades and gaps, picks the start, middle
//     or end variant of each clip by its position in the sentence, and cuts
//     the current line for a higher-priority one. Mix renders that timeline
//     to PCM (the reference implementation the browser path is checked
//     against) and VoicePlayer schedules it on the mixer's voice bus.
//
// Everything on the event path (Planner.Submit, Planner.Poll, Sequencer.Play, Sequencer.Interrupt)
// allocates nothing in steady state: scratch space is sized when the Planner
// and Sequencer are built.
//
// The package carries no game content. The recorded fragments come from the
// phrase manifest that "gosx voice render" produces; see docs/audio-speech.md.
package speech
