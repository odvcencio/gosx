// Package loop owns the browser's requestAnimationFrame driver so a game
// does not have to call js.FuncOf/requestAnimationFrame itself.
//
// Driver reads consecutive frame timestamps from a FrameSource, converts
// each gap into a delta, and calls game.Runtime.Step, then a render callback
// carrying the frame's interpolation alpha — the fixed-step accumulator
// itself already lives in game.Runtime/game.Clock; Driver is only the part
// that was missing: the loop that keeps calling it, hooked to the actual
// browser clock.
//
// While the host reports the tab hidden, Driver keeps scheduling frames (so
// it notices when the tab becomes visible again) but skips Step and the
// render callback, so game time does not advance and no stale delta is fed
// in once the tab returns.
package loop
