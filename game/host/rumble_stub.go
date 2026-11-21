//go:build !(js && wasm)

package host

// RumbleGamepad reports false outside a browser: there is no gamepad to shake.
func RumbleGamepad(int, Rumble) bool { return false }

// StopRumble reports false outside a browser.
func StopRumble(int) bool { return false }
