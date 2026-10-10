// Package audiohost exposes typed browser audio nodes and lifecycle controls
// without importing the game runtime or manifest player. It supports decoded
// buffers, scheduled envelopes, stereo panning, feedback delay graphs, bounded
// loops, procedural oscillators and worklets. Musical scheduling, asset loading
// and voice admission remain application-owned.
//
// Call Resume from the user gesture itself. Promise-waiting methods such as
// DecodeAudioData, Suspend and Close must run in a goroutine when called from
// a browser event handler. Disconnecting a source releases its ended callback;
// Close also releases all owned source and worklet handlers.
package audiohost
