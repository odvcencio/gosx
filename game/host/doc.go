// Package host provides typed browser gamepad snapshots and animation-frame
// ownership without depending on game.Runtime, server hubs, or the compiler.
//
// Presentation-only WASM engines can use these adapters directly. Games using
// game.Input or game.Runtime can continue using game/gamepad and game/loop;
// their existing host types are aliases of the types in this package.
//
// Browser implementations are available on js/wasm. The interfaces and gamepad
// state are platform-independent so application control policy can be tested
// with ordinary Go values. FrameSourceJS releases callbacks after delivery or
// cancellation; a mounted engine must cancel its pending request on disposal.
package host
