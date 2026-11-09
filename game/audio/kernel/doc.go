// Package kernel hosts a WASM audio kernel, such as Cicada's music engine, in
// an AudioWorklet and routes its output to a mixer bus.
//
// GoSX does not depend on any kernel. A kernel is a WebAssembly reactor that
// follows the gosx_audio ABI described in game/audio/worklet: it renders
// planar float32 audio, takes fixed-size command records and returns
// fixed-size message records. The kernel's authors supply a worklet.Backend
// (record sizes, opcodes, capabilities), the kernel module bytes and a data
// blob such as a compiled score; this package loads them, posts commands,
// returns the message buffer, and reports position and state.
//
// Start and its siblings are available only under GOOS=js GOARCH=wasm and
// must run in a goroutine, never inside a syscall/js callback. The message
// protocol itself (worklet.State, worklet.Event) builds on every platform and
// is unit-tested natively.
package kernel
