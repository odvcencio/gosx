//go:build js && wasm

package scene

import (
	"context"
	"errors"
	"strings"
	"syscall/js"
	"testing"
)

func preparationFixture(t *testing.T) js.Value {
	t.Helper()
	f := surfaceFixture(t)
	setup := js.Global().Get("Function").New("f", `
const old = Object.getOwnPropertyDescriptor(globalThis, '__gosx_scene3d_api');
const oldObserver = Object.getOwnPropertyDescriptor(globalThis, 'MutationObserver');
f.observers = [];
globalThis.MutationObserver = class { constructor(callback) { this.callback=callback; f.observers.push(this); } observe() {} disconnect() { this.closed=true; } };
f.renderer = { texturePreparation: { generation: { disposed:false } } };
f.mount.__gosxScene3DState = { _crowdRenderer: f.renderer };
f.mount.ownerDocument = {};
f.listeners = new Map();
f.canvas.addEventListener = (name, callback) => f.listeners.set(name,callback);
f.canvas.removeEventListener = (name, callback) => { if(f.listeners.get(name)===callback) f.listeners.delete(name); };
f.invalidate = kind => {
 if(kind==='removal') f.mount.isConnected=false;
 if(kind==='replacement') f.mount.__gosxScene3DHandle={};
 if(kind==='renderer') f.renderer.texturePreparation.generation.disposed=true;
 if(kind==='renderer') f.listeners.get('gosx:scene3d:resource-ready')(); else f.observers[0].callback();
};
f.calls = []; f.readyCalls = 0; f.phase = 'maps';
f.pending = () => new Promise((resolve, reject) => { f.resolve = resolve; f.reject = reject; });
f.module = { prepare(mount, requests, signal) {
 f.calls.push({mount, requests, signal}); f.signal = signal; return f.pending();
} };
globalThis.__gosx.scene3d = { whenReady(mount, options) {
 f.readyCalls++; f.signal=options.signal;
 return f.phase === 'ready' ? f.pending() : Promise.resolve(true);
} };
globalThis.__gosx_scene3d_api = { loadPresentation(kind, key, fallback) {
 f.kind = kind; f.key = key; f.fallback = fallback;
 return f.phase === 'chunk' ? f.pending() : Promise.resolve(f.module);
} };
f.restorePreparation = () => { if(old) Object.defineProperty(globalThis,'__gosx_scene3d_api',old); else delete globalThis.__gosx_scene3d_api; if(oldObserver) Object.defineProperty(globalThis,'MutationObserver',oldObserver); else delete globalThis.MutationObserver; };
`)
	setup.Invoke(f)
	t.Cleanup(func() { f.Call("restorePreparation") })
	return f
}

func TestSurfaceModelPreparationSnapshotsAndLoadsOptionalModule(t *testing.T) {
	f := preparationFixture(t)
	s := NewSurface("stage")
	defer s.Dispose()
	requests := []ModelPreparation{{ID: "hero", Src: "/hero.glb"}, {ID: "enemy", Src: "/enemy.glb"}}
	type outcome struct {
		result ModelPreparationResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() { r, e := s.PrepareModels(context.Background(), requests); done <- outcome{r, e} }()
	awaitScene(t, func() bool { return f.Get("calls").Length() == 1 })
	requests[0].Src = "/changed.glb"
	call := f.Get("calls").Index(0)
	if !call.Get("mount").Equal(f.Get("mount")) || call.Get("requests").Index(0).Get("src").String() != "/hero.glb" {
		t.Fatal("request lost mount ownership or snapshot")
	}
	if f.Get("kind").String() != "model-preparation" || f.Get("key").String() != "gosxScene3dModelPreparationUrl" || f.Get("fallback").String() != "" {
		t.Fatal("incorrect lazy module authority")
	}
	f.Get("resolve").Invoke(map[string]any{"models": 2, "textures": 7})
	got := <-done
	if got.err != nil || got.result.Models != 2 || got.result.Textures != 7 {
		t.Fatalf("got %#v, %v", got.result, got.err)
	}
	if !f.Get("signal").Get("aborted").Bool() {
		t.Fatal("operation controller was retained")
	}
}

func TestSurfaceModelPreparationCancellationAndDisposal(t *testing.T) {
	for _, phase := range []string{"ready", "chunk", "maps", "dispose"} {
		t.Run(phase, func(t *testing.T) {
			f := preparationFixture(t)
			s := NewSurface("stage")
			defer s.Dispose()
			if phase != "dispose" {
				f.Set("phase", phase)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := s.PrepareModels(ctx, []ModelPreparation{{ID: "hero", Src: "/hero.glb"}})
				done <- err
			}()
			awaitScene(t, func() bool {
				if phase == "ready" {
					return f.Get("readyCalls").Int() > 0
				}
				if phase == "chunk" {
					return f.Get("kind").Type() == js.TypeString
				}
				return f.Get("calls").Length() > 0
			})
			if phase == "dispose" {
				s.Dispose()
			} else {
				cancel()
			}
			err := <-done
			if phase == "dispose" {
				if err == nil || !strings.Contains(err.Error(), "disposed") {
					t.Fatalf("dispose: %v", err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel: %v", err)
			}
			if !f.Get("signal").Get("aborted").Bool() {
				t.Fatal("native operation was not aborted")
			}
			f.Get("reject").Invoke(js.Global().Get("Error").New("late rejection"))
		})
	}
}

func TestSurfaceModelPreparationRejectsReplacementAndRuntimeFailure(t *testing.T) {
	for _, kind := range []string{"replacement", "failure"} {
		t.Run(kind, func(t *testing.T) {
			f := preparationFixture(t)
			s := NewSurface("stage")
			defer s.Dispose()
			f.Set("phase", "chunk")
			done := make(chan error, 1)
			go func() {
				_, err := s.PrepareModels(context.Background(), []ModelPreparation{{ID: "hero", Src: "/hero.glb"}})
				done <- err
			}()
			awaitScene(t, func() bool { return f.Get("kind").Type() == js.TypeString })
			if kind == "replacement" {
				f.Get("mount").Set("__gosxScene3DHandle", js.Global().Get("Object").New())
				f.Get("resolve").Invoke(f.Get("module"))
			} else {
				f.Get("reject").Invoke(js.Global().Get("Error").New("script failed"))
			}
			if err := <-done; err == nil {
				t.Fatal("failure returned success")
			}
			if f.Get("calls").Length() != 0 {
				t.Fatal("failed owner started model preparation")
			}
		})
	}
}

func TestSurfaceModelPreparationOwnsLifetimeDuringReadyAndChunkDelays(t *testing.T) {
	for _, phase := range []string{"ready", "chunk", "maps"} {
		for _, kind := range []string{"removal", "replacement", "renderer"} {
			t.Run(phase+"/"+kind, func(t *testing.T) {
				f := preparationFixture(t)
				s := NewSurface("stage")
				defer s.Dispose()
				f.Set("phase", phase)
				done := make(chan error, 1)
				go func() {
					_, err := s.PrepareModels(context.Background(), []ModelPreparation{{ID: "hero", Src: "/hero.glb"}})
					done <- err
				}()
				awaitScene(t, func() bool {
					if phase == "ready" {
						return f.Get("readyCalls").Int() > 0
					}
					if phase == "chunk" {
						return f.Get("kind").Type() == js.TypeString
					}
					return f.Get("calls").Length() > 0
				})
				f.Call("invalidate", kind)
				if err := <-done; err == nil || !strings.Contains(err.Error(), "disposed or replaced") {
					t.Fatalf("owner loss: %v", err)
				}
				if !f.Get("observers").Index(0).Get("closed").Bool() || f.Get("listeners").Get("size").Int() != 0 {
					t.Fatal("lifetime observer or listener retained")
				}
				if !f.Get("signal").Get("aborted").Bool() {
					t.Fatal("owner loss did not abort operation")
				}
				f.Get("reject").Invoke(js.Global().Get("Error").New("late owner failure"))
			})
		}
	}
}
