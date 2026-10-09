//go:build js && wasm

package scene

import (
	"bytes"
	"syscall/js"
	"testing"
	"time"
)

func surfaceFixture(t *testing.T) js.Value {
	t.Helper()
	f := js.Global().Get("Function").New(`
const names = ['document', 'CustomEvent', '__gosx_scene3d_instance_stream_apply', '__gosx'];
const saved = names.map(name => Object.getOwnPropertyDescriptor(globalThis, name));
const f = { queries: 0, loads: 0, lost: 0, restored: 0 };
f.handle = { views: [], snapshots: [], updates: [], applyInstanceStream(bytes) {
  this.views.push(bytes); this.snapshots.push(bytes.slice()); return { applied: true };
}, updateSceneProps(props) { this.updates.push(props); } };
f.canvas = { width: 800, height: 600, getContext(name) { return name === 'webgl2' ? {
  getExtension(name) { return name === 'WEBGL_lose_context' ? {
    loseContext() { f.lost++; }, restoreContext() { f.restored++; }
  } : null; }
} : null; } };
f.newMount = () => ({ isConnected: true, __gosxScene3DHandle: f.handle, events: [],
  querySelector() { return f.canvas; }, querySelectorAll() { return [f.canvas]; },
  dispatchEvent(event) { this.events.push(event); return true; }
});
f.mount = f.newMount();
globalThis.document = { getElementById(id) { f.queries++; return id === 'stage' ? f.mount : null; } };
globalThis.CustomEvent = class { constructor(type, options) { this.type = type; this.detail = options.detail; } };
globalThis.__gosx_scene3d_instance_stream_apply = () => {};
globalThis.__gosx = { host: { scene3d: {} } };
f.deferLoad = () => {
  delete globalThis.__gosx_scene3d_instance_stream_apply;
  globalThis.__gosx.host.scene3d.preloadInstanceStream = () => {
    f.loads++; return new Promise((resolve, reject) => { f.resolve = resolve; f.reject = reject; });
  };
};
f.restore = () => names.forEach((name, i) => {
  if (saved[i]) Object.defineProperty(globalThis, name, saved[i]); else delete globalThis[name];
});
return f;
`).Invoke()
	t.Cleanup(func() { f.Call("restore") })
	return f
}

func awaitScene(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("scene async operation did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStreamWriterReusesBuffersAndRetainedHandle(t *testing.T) {
	f := surfaceFixture(t)
	w := NewStreamWriter("stage")
	t.Cleanup(w.Dispose)
	if !w.Prepare() {
		t.Fatal("resident codec not ready")
	}
	frame := benchInstanceStreamFrame(180)
	want, _ := frame.Encode()
	if applied, err := w.Apply(frame); err != nil || !applied {
		t.Fatalf("first apply = %v, %v", applied, err)
	}
	firstGoBuffer := &w.buffer[0]
	frame.Data[0], frame.Revision = 99, 2
	if applied, err := w.Apply(frame); err != nil || !applied {
		t.Fatalf("second apply = %v, %v", applied, err)
	}
	views := f.Get("handle").Get("views")
	if !views.Index(0).Equal(views.Index(1)) || &w.buffer[0] != firstGoBuffer {
		t.Fatal("same-size stream allocated new Go or JS storage")
	}
	if f.Get("queries").Int() != 1 {
		t.Fatal("Apply performed a DOM lookup")
	}
	copyOfFirst := make([]byte, len(want))
	js.CopyBytesToGo(copyOfFirst, f.Get("handle").Get("snapshots").Index(0))
	if !bytes.Equal(copyOfFirst, want) {
		t.Fatal("reused storage changed retained frame or wire layout")
	}
	frame.Count, frame.Data = 0, nil
	if applied, err := w.Apply(frame); err != nil || !applied || w.viewLength >= len(want) {
		t.Fatalf("shrinking frame failed: %v", err)
	}
	if !w.bytes.Get("buffer").Equal(views.Index(0).Get("buffer")) {
		t.Fatal("shrinking frame allocated a backing buffer")
	}
	w.Dispose()
	if applied, err := w.Apply(frame); applied || err != nil || w.buffer != nil || w.bytes.Truthy() {
		t.Fatal("disposed stream retained buffers or accepted writes")
	}
}

func TestStreamWriterRejectsReentryAndRefreshesAfterRemount(t *testing.T) {
	f := surfaceFixture(t)
	w := NewStreamWriter("stage")
	t.Cleanup(w.Dispose)
	w.Prepare()
	frame := benchInstanceStreamFrame(1)
	reentered := false
	apply := js.FuncOf(func(js.Value, []js.Value) any {
		reentered = true
		if ok, err := w.Apply(frame); ok || err == nil {
			t.Error("reentry accepted and could overwrite the active byte view")
		}
		return map[string]any{"applied": true}
	})
	t.Cleanup(apply.Release)
	f.Get("handle").Set("applyInstanceStream", apply)
	if ok, err := w.Apply(frame); !ok || err != nil || !reentered {
		t.Fatalf("outer apply failed: %v", err)
	}
	old := f.Get("mount")
	old.Set("isConnected", false)
	f.Set("mount", f.Call("newMount"))
	if !w.Prepare() || f.Get("queries").Int() != 2 || !w.surface.mount.Equal(f.Get("mount")) {
		t.Fatal("writer did not refresh a replaced mount")
	}
}

func TestStreamWriterPreloadCancellationAndRetry(t *testing.T) {
	f := surfaceFixture(t)
	f.Call("deferLoad")
	w := NewStreamWriter("stage")
	t.Cleanup(w.Dispose)
	if w.Prepare() || w.Prepare() || f.Get("loads").Int() != 1 {
		t.Fatal("loading codec reported ready or started duplicate loads")
	}
	if ok, err := w.Apply(benchInstanceStreamFrame(1)); ok || err != nil || w.buffer != nil {
		t.Fatal("loading writer queued an unowned frame")
	}
	f.Call("reject", js.Global().Get("Error").New("load failed"))
	awaitScene(t, func() bool { return !w.loading })
	w.Prepare()
	if f.Get("loads").Int() != 1 || w.retryAt == 0 {
		t.Fatal("failed codec load retried each frame")
	}
	w.retryAt = 0
	w.Prepare()
	if f.Get("loads").Int() != 2 {
		t.Fatal("codec never retried after failure")
	}
	w.Dispose()
	awaitScene(t, func() bool { return !w.loading })
	f.Call("reject", js.Global().Get("Error").New("late rejected load"))
	time.Sleep(time.Millisecond)
	if w.Prepare() {
		t.Fatal("disposed writer became ready after late completion")
	}
}

func TestStreamWriterSynchronousPreloadFailureIsBounded(t *testing.T) {
	f := surfaceFixture(t)
	f.Call("deferLoad")
	js.Global().Get("__gosx").Get("host").Get("scene3d").Set("preloadInstanceStream", js.Global().Get("Function").New("throw new Error('load unavailable')"))
	w := NewStreamWriter("stage")
	t.Cleanup(w.Dispose)
	if w.Prepare() || w.retryAt == 0 || w.loading {
		t.Fatal("throwing preload did not enter recoverable backoff")
	}
}

func TestSurfaceOwnsCommandSnapshotAndPartialOptions(t *testing.T) {
	f := surfaceFixture(t)
	s := NewSurface("stage")
	batch := MountCommandBatch{Revision: 1, Commands: []Command{RemoveObjectCommand("actor")}}
	want, _ := batch.Marshal()
	if err := s.DispatchCommands(batch); err != nil {
		t.Fatal(err)
	}
	batch.Commands[0] = RemoveObjectCommand("different")
	event := f.Get("mount").Get("events").Index(0)
	if event.Get("type").String() != MountCommandsEvent || js.Global().Get("JSON").Call("stringify", event.Get("detail")).String() != string(want) {
		t.Fatal("command snapshot changed after caller reused commands")
	}
	fps, pixels := 60.0, 900000
	if err := s.Update(SurfaceOptions{MaxFrameRate: &fps, PostFXMaxPixels: &pixels}); err != nil {
		t.Fatal(err)
	}
	update := f.Get("handle").Get("updates").Index(0)
	if update.Get("maxFrameRate").Int() != 60 || update.Get("postFXMaxPixels").Int() != pixels || js.Global().Get("Object").Call("keys", update).Length() != 2 {
		t.Fatal("partial graphics options replaced unrelated scene properties")
	}
	if changed, err := s.Resize(1200, 900); !changed || err != nil {
		t.Fatalf("resize failed: %v", err)
	}
	if changed, err := s.Resize(1200, 900); changed || err != nil {
		t.Fatalf("settled size changed: %v", err)
	}
	if changed, err := s.Resize(0, 0); changed || err == nil || f.Get("canvas").Get("width").Int() != 1200 {
		t.Fatal("invalid viewport destroyed backing dimensions")
	}
	loss, err := s.LoseContext()
	if err != nil || f.Get("lost").Int() != 1 {
		t.Fatalf("native context loss not invoked: %v", err)
	}
	if err := loss.Restore(); err != nil || f.Get("restored").Int() != 1 {
		t.Fatalf("native context restore not invoked: %v", err)
	}
	s.Dispose()
	if err := s.DispatchCommands(batch); err == nil || s.mount.Truthy() {
		t.Fatal("disposed surface accepted a command or retained mount")
	}
}
