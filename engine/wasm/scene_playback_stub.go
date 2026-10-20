//go:build !js || !wasm

package wasm

import (
	"context"
	"m31labs.dev/gosx/scene"
)

func NextFrame(context.Context) error { return ErrUnsupported }
func (s Scene3D) PlayFrames(context.Context, FramePlaybackOptions, func(float64, bool) []scene.Command) error {
	return ErrUnsupported
}
func (s Scene3D) TransitionCamera(context.Context, scene.IRCamera, CameraTransitionOptions) error {
	return ErrUnsupported
}
