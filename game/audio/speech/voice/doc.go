// Package voice plays speech.Sequencer timelines on the mixer's voice bus.
//
// Player is a speech.Sink. Each segment becomes one single-use buffer source
// (the browser requires one per playback) feeding its own gain node, which
// carries the fade-in and fade-out ramps; the segment gains feed one gain node
// per line, which carries the interrupt fade; the line gains feed the voice
// bus. Start times are whole sample frames converted to audio-clock seconds,
// so a line's timing does not depend on when the game loop happens to run.
//
// While a line is on air Player ducks the buses in Config.Duck (music and sfx
// by default, which is where crowd noise lives) and lets them recover when the
// line ends. Call Update once per frame to do that and to release finished
// nodes.
//
// LoadClips fills a speech.Library from the rendered clip manifest: one decoded
// atlas per voice, with each clip a region of it.
package voice
