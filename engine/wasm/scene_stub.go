//go:build !js || !wasm

package wasm

import (
	"context"
	"m31labs.dev/gosx/scene"
)

type Scene3D struct{}

func (Context) Scene3D(any) Scene3D                             { return Scene3D{} }
func (Scene3D) Ready(context.Context) error                     { return ErrUnsupported }
func (Scene3D) Dispatch(context.Context, []scene.Command) error { return ErrUnsupported }
func (Scene3D) Camera() (scene.IRCamera, error)                 { return scene.IRCamera{}, ErrUnsupported }
func (Scene3D) SetCamera(context.Context, any) error            { return ErrUnsupported }

func (Scene3D) SetAnimationClock(context.Context, float64, bool) error { return ErrUnsupported }
