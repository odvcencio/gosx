//go:build js && wasm

package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/scene"
	"syscall/js"
)

// Scene3D is a command target scoped to a mounted GoSX engine. It never exposes
// the renderer's mutable handle or its internal object cache.
type Scene3D struct {
	owner Context
	mount js.Value
}

func (c Context) Scene3D(mount js.Value) Scene3D { return Scene3D{owner: c, mount: mount} }

func (s Scene3D) call(method string, args ...any) (value js.Value, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("Scene3D %s: %v", method, failure)
		}
	}()
	if !s.owner.IsCurrent() {
		return js.Undefined(), context.Canceled
	}
	all := []any{method, s.mount}
	all = append(all, args...)
	return s.owner.value.Call("scene3D", all...), nil
}

func (s Scene3D) async(ctx context.Context, method string, args ...any) error {
	if ctx == nil {
		return fmt.Errorf("Scene3D context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	controller := js.Global().Get("AbortController").New()
	defer controller.Call("abort")
	args = append(args, map[string]any{"signal": controller.Get("signal")})
	promise, err := s.call(method, args...)
	if err != nil {
		return err
	}
	_, err = jsutil.AwaitPromiseContext(ctx, promise)
	return err
}

// Ready waits for a mounted command renderer without polling from application code.
func (s Scene3D) Ready(ctx context.Context) error { return s.async(ctx, "whenReady") }

// Dispatch applies typed commands to this scene and awaits renderer acceptance.
func (s Scene3D) Dispatch(ctx context.Context, commands []scene.Command) error {
	if commands == nil {
		commands = []scene.Command{}
	}
	value, err := encodeEventDetail(commands)
	if err != nil {
		return err
	}
	return s.async(ctx, "dispatchCommands", value)
}

// Camera returns the effective mounted camera, including user orbit/zoom.
func (s Scene3D) Camera() (camera scene.IRCamera, err error) {
	value, err := s.call("getCamera")
	if err != nil {
		return camera, err
	}
	if !value.Truthy() {
		return camera, fmt.Errorf("Scene3D camera is not ready")
	}
	err = json.Unmarshal([]byte(js.Global().Get("JSON").Call("stringify", value).String()), &camera)
	return camera, err
}

// SetCamera updates both renderer and interactive camera controls coherently.
func (s Scene3D) SetCamera(ctx context.Context, camera any) error {
	value, err := encodeEventDetail(scene.SetCameraCommand(camera).Data)
	if err != nil {
		return err
	}
	return s.async(ctx, "setCamera", value)
}

// SetAnimationClock controls the scene clock without exposing renderer internals.
func (s Scene3D) SetAnimationClock(ctx context.Context, seconds float64, paused bool) error {
	return s.async(ctx, "setAnimationClock", map[string]any{"timeSeconds": seconds, "paused": paused})
}
