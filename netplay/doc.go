// Package netplay is a transport-agnostic rollback netcode engine and relay
// for deterministic two-player simulations.
//
// A [Game] is a deterministic simulation. A [Session] runs one peer: it
// predicts the remote player's input, rolls back when a prediction was wrong,
// and keeps both peers' confirmed state identical. A [Relay] forwards packets,
// verifies checksums with a native copy of the game and repairs a desync.
// An [EventLedger] reconciles presentation effects after a rollback.
//
// The package uses only the standard library and does no I/O of its own:
// callers supply a [Transport] and a clock.
package netplay
