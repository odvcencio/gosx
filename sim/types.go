// Package sim provides server-authoritative game simulation over gosx hubs.
// Games implement the Simulation interface and a Runner drives the simulation
// at a fixed tick rate, collecting player inputs and broadcasting state.
//
// The Runner never calls the Simulation concurrently with itself. It supports
// pause, speed, catch-up, input queues, pluggable broadcasters and replay logs
// with an initial snapshot; see Options and the package README.
//
// A js/wasm build of sim holds only what a browser client needs: Input,
// Simulation and CheckedRestorer (aliases of package simcore), the replay log
// types, SortedPlayerIDs and EachInput, and the binary snapshot codec
// (DecodeBinarySnapshot). The Runner, Options and broadcasters need the hub
// server and build only for other platforms, so a client that imports sim
// links no hub, net/http or crypto/tls.
package sim

import "m31labs.dev/gosx/sim/simcore"

// Input holds raw per-client input data for a single tick. It is
// simcore.Input, which browser builds use without linking the hub.
type Input = simcore.Input

// Simulation is the interface a game must implement to be driven by a Runner.
// It is simcore.Simulation; see that type for the method contract.
type Simulation = simcore.Simulation

// CheckedRestorer is the optional part of a Simulation that reports a snapshot
// it cannot apply. It is simcore.CheckedRestorer. Runner.RestoreSnapshot calls
// it in place of Restore. game.Runtime implements it.
type CheckedRestorer = simcore.CheckedRestorer

// StateEncoding controls how Runner places Simulation state bytes into hub
// payloads.
type StateEncoding string

const (
	// StateEncodingBytes preserves the historical behavior: []byte state fields
	// are JSON-encoded by encoding/json, which produces base64 strings.
	StateEncodingBytes StateEncoding = "bytes"
	// StateEncodingJSON embeds valid JSON state bytes directly as JSON values.
	// Invalid JSON falls back to StateEncodingBytes behavior.
	StateEncodingJSON StateEncoding = "json"
)

// RingDisabled turns off the rollback snapshot ring when set as Options.RingSize.
const RingDisabled = -1

func cloneInput(input Input) Input {
	if input.Data == nil {
		return Input{}
	}
	data := make([]byte, len(input.Data))
	copy(data, input.Data)
	return Input{Data: data}
}

func cloneInputs(inputs map[string]Input) map[string]Input {
	if len(inputs) == 0 {
		return map[string]Input{}
	}
	out := make(map[string]Input, len(inputs))
	for playerID, input := range inputs {
		out[playerID] = cloneInput(input)
	}
	return out
}
