//go:build js && wasm

package audio

import audiohost "m31labs.dev/gosx/game/audio/host"

// HostJS and WorkletNodeJS preserve the original browser audio entrypoints.
type HostJS = audiohost.HostJS
type WorkletNodeJS = audiohost.WorkletNodeJS

func NewHostJS() (*HostJS, error) { return audiohost.NewHostJS() }
func NewHostJSWithOptions(options HostOptions) (*HostJS, error) {
	return audiohost.NewHostJSWithOptions(options)
}
func BufferDuration(buffer Buffer) float64 { return audiohost.BufferDuration(buffer) }
