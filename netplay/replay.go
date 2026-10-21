package netplay

// ReplayCheck is a checksum of the state at the start of Frame.
type ReplayCheck struct {
	Frame uint32
	Sum   uint64
}

// Replay is a recorded match: the confirmed inputs of both players, the
// simulation identity they were played under, and checksums to verify a
// re-simulation. Marshal and Unmarshal give the versioned binary format.
type Replay struct {
	Identity  [32]byte
	InputSize int // bytes per encoded input
	Interval  int // checksum interval K
	Frames    int // number of frames recorded
	// Inputs holds Frames entries of 2*InputSize bytes: player 0's input,
	// then player 1's, for each frame in order.
	Inputs []byte
	Checks []ReplayCheck
}
