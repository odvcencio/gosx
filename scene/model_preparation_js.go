//go:build js && wasm

package scene

import (
	"context"
	"errors"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

// PrepareModels loads only the selected models and their material maps using
// this mount's backend and texture-variant scope. Enable Props.ModelPreparation
// on the mount and call from a goroutine. It
// returns after decoding and native map upload, or on failure, context cancel,
// Surface.Dispose, or renderer replacement. Shared fetches remain cache-owned.
func (s *Surface) PrepareModels(ctx context.Context, requests []ModelPreparation) (result ModelPreparationResult, err error) {
	defer surfaceRecover(&err)
	defer func() {
		if s != nil && s.disposed && err != nil {
			err = errors.New("scene: preparation surface is disposed")
		}
	}()
	if ctx == nil {
		return result, errors.New("scene: preparation context is required")
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	data, err := modelPreparationSnapshot(requests)
	if err != nil {
		return result, err
	}
	mount, err := s.element()
	if err != nil {
		return result, err
	}
	handle, err := s.renderer()
	if err != nil {
		return result, err
	}
	state := mount.Get("__gosxScene3DState")
	if !state.Truthy() {
		return result, errors.New("scene: preparation renderer unavailable")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-s.closed:
			cancel()
		case <-ctx.Done():
		}
	}()
	controller := js.Global().Get("AbortController").New()
	defer controller.Call("abort")
	stop, ownerLost := watchModelPreparation(mount, state, handle, cancel)
	defer stop()
	defer func() {
		if *ownerLost {
			err = errors.New("scene: preparation mount disposed or replaced")
		}
	}()
	options := map[string]any{"signal": controller.Get("signal"), "timeoutMS": 0}
	namespace := js.Global().Get("__gosx")
	if !namespace.Truthy() || !namespace.Get("scene3d").Truthy() {
		return result, errors.New("scene: preparation runtime unavailable")
	}
	if _, err = jsutil.AwaitPromiseContext(ctx, namespace.Get("scene3d").Call("whenReady", mount, options)); err != nil {
		return result, err
	}
	api := js.Global().Get("__gosx_scene3d_api")
	if !api.Truthy() {
		return result, errors.New("scene: preparation loader unavailable")
	}
	module, err := jsutil.AwaitPromiseContext(ctx, api.Call("loadPresentation", "model-preparation", "gosxScene3dModelPreparationUrl", ""))
	if err != nil {
		return result, err
	}
	if s.disposed || !mount.Get("__gosxScene3DHandle").Equal(handle) || !mount.Get("__gosxScene3DState").Equal(state) {
		return result, errors.New("scene: preparation mount disposed or replaced")
	}
	payload := js.Global().Get("JSON").Call("parse", string(data))
	value, err := jsutil.AwaitPromiseContext(ctx, module.Call("prepare", mount, payload, controller.Get("signal")))
	if err != nil {
		return result, err
	}
	result.Models, result.Textures = value.Get("models").Int(), value.Get("textures").Int()
	return result, nil
}

// Fence the entire operation, including command/feature loading before the
// optional preparation module is available. DOM/resource events never query GL.
func watchModelPreparation(mount, state, handle js.Value, cancel context.CancelFunc) (func(), *bool) {
	lost := false
	renderer := state.Get("_crowdRenderer")
	callback := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		invalid := !mount.Get("isConnected").Truthy() || !mount.Get("__gosxScene3DHandle").Equal(handle) || !mount.Get("__gosxScene3DState").Equal(state) || !state.Get("_crowdRenderer").Equal(renderer)
		if renderer.Truthy() {
			preparation := renderer.Get("texturePreparation")
			invalid = invalid || preparation.Truthy() && preparation.Get("generation").Get("disposed").Truthy()
		}
		if invalid {
			lost = true
			cancel()
		}
		return nil
	})
	observer := js.Undefined()
	if constructor := js.Global().Get("MutationObserver"); constructor.Type() == js.TypeFunction {
		observer = constructor.New(callback)
		observer.Call("observe", mount, map[string]any{"attributes": true, "attributeFilter": []any{"data-gosx-scene3d-command-ready", "data-gosx-scene3d-renderer"}})
		if document := mount.Get("ownerDocument"); document.Truthy() {
			observer.Call("observe", document, map[string]any{"childList": true, "subtree": true})
		}
	}
	canvas := mount.Call("querySelector", "canvas")
	listening := canvas.Truthy() && canvas.Get("addEventListener").Type() == js.TypeFunction
	if listening {
		canvas.Call("addEventListener", "gosx:scene3d:resource-ready", callback)
	}
	return func() {
		if observer.Truthy() {
			observer.Call("disconnect")
		}
		if listening {
			canvas.Call("removeEventListener", "gosx:scene3d:resource-ready", callback)
		}
		callback.Release()
	}, &lost
}
