package host

// GamepadSource abstracts navigator.getGamepads(). NavigatorSource is the real
// browser implementation; tests and native code supply a fake.
type GamepadSource interface {
	// Gamepads returns the currently connected pads. An empty or disconnected
	// slot is simply omitted, matching how the browser leaves gaps in
	// navigator.getGamepads() when a pad in the middle of the array
	// disconnects.
	Gamepads() []GamepadState
}

// GamepadState is one gamepad's snapshot for a single poll.
type GamepadState struct {
	// Index is the browser-assigned gamepad slot. It is stable for a given
	// physical pad for the life of the connection.
	Index int
	// ID is the browser's device string, e.g.
	// "Xbox Wireless Controller (STANDARD GAMEPAD Vendor: 045e Product: 0b13)".
	ID string
	// Mapping is "standard" for a pad the browser normalizes to the W3C
	// standard gamepad layout, or "" otherwise.
	Mapping string
	// Buttons holds each button's analog value in [0,1]. A purely digital
	// button reports 0 or 1.
	Buttons []float64
	// Axes holds each axis value in [-1,1]. The standard mapping places the
	// left stick at axes 0 (x) and 1 (y), and the right stick at 2 (x) and 3
	// (y).
	Axes []float64
}
