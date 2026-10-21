//go:build js && wasm

package scene

import (
	"bytes"
	"syscall/js"
	"testing"
)

// workerFixture installs a stand-in for the engine worker's page bridge and
// removes it after the test. It records every call the worker would post.
func workerFixture(t *testing.T) js.Value {
	t.Helper()
	f := js.Global().Get("Function").New(`
const saved = Object.getOwnPropertyDescriptor(globalThis, '__gosx');
const f = { ops: [], listeners: new Map(), frames: [], stream: [], nextLid: 1 };
f.bridge = {
  mountOp(target, op, name, json) { f.ops.push({ target, op, name, json }); },
  mountListen(target, name, cb) { const lid = f.nextLid++; f.listeners.set(lid, { target, name, cb }); return () => f.listeners.delete(lid); },
};
f.scene3d = {
  dispatchPoseFrame(target, bytes, options) { f.frames.push({ kind: 'pose', target, bytes: bytes.slice(), buffer: bytes.buffer, options }); return Promise.resolve({ queued: true }); },
  dispatchClipFrame(target, bytes) { f.frames.push({ kind: 'clip', target, bytes: bytes.slice() }); return Promise.resolve({ queued: true }); },
  applyInstanceStream(target, bytes) { f.stream.push({ target, bytes: bytes.slice() }); return { applied: true, queued: true }; },
};
globalThis.__gosx = { worker: f.bridge, scene3d: f.scene3d };
f.restore = () => { if (saved) Object.defineProperty(globalThis, '__gosx', saved); else delete globalThis.__gosx; };
return f;
`).Invoke()
	t.Cleanup(func() { f.Call("restore") })
	return f
}

func TestWorkerSurfaceSendsCommandsAndCancellationToThePage(t *testing.T) {
	f := workerFixture(t)
	s := NewSurface("arena")
	t.Cleanup(s.Dispose)
	batch := MountCommandBatch{Revision: 3, Commands: []Command{}, FrameRequest: &MountFrameRequest{ID: "req-1", ModelIDs: []string{"hero"}}}
	if err := s.DispatchCommands(batch); err != nil {
		t.Fatal(err)
	}
	op := f.Get("ops").Index(0)
	if op.Get("target").String() != "arena" || op.Get("op").String() != "dispatchEvent" || op.Get("name").String() != MountCommandsEvent {
		t.Fatalf("command op: %v", op)
	}
	want, _ := batch.Marshal()
	if op.Get("json").String() != string(want) {
		t.Fatalf("payload %q, want %q", op.Get("json").String(), want)
	}
	if err := s.CancelFrameRequest("req-1"); err != nil {
		t.Fatal(err)
	}
	cancel := f.Get("ops").Index(1)
	if cancel.Get("name").String() != MountFrameCancelEvent || cancel.Get("json").String() != `{"id":"req-1"}` {
		t.Fatalf("cancel op: %v", cancel)
	}
	if s.frameRequestID != "" {
		t.Fatal("a cancelled request must be forgotten")
	}
	if err := s.DispatchCommands(MountCommandBatch{}); err == nil {
		t.Fatal("an invalid batch must fail before reaching the page")
	}
}

func TestWorkerSurfaceReceivesFrameSubmissionReceipts(t *testing.T) {
	f := workerFixture(t)
	s := NewSurface("arena")
	t.Cleanup(s.Dispose)
	var got []MountFrameSubmission
	cancel, err := s.OnFrameSubmitted(func(r MountFrameSubmission) { got = append(got, r) })
	if err != nil {
		t.Fatal(err)
	}
	listener := f.Get("listeners").Call("get", 1)
	if listener.Get("name").String() != MountFrameSubmittedEvent || listener.Get("target").String() != "arena" {
		t.Fatalf("listener: %v", listener)
	}
	valid := `{"requestID":"r","revision":2,"commandRevision":2,"hydrationGeneration":1,"frameSerial":9,"renderer":"webgl","modelIDs":["a"],"objectIDs":["b"]}`
	listener.Get("cb").Invoke(valid)
	listener.Get("cb").Invoke(`{"requestID":"","revision":2}`) // incomplete receipts are dropped
	listener.Get("cb").Invoke("not json")
	if len(got) != 1 || got[0].RequestID != "r" || got[0].FrameSerial != 9 {
		t.Fatalf("receipts %+v", got)
	}
	cancel()
	cancel()
	if f.Get("listeners").Get("size").Int() != 0 {
		t.Fatal("cancel must unlisten on the page")
	}
}

func TestWorkerSurfaceUpdateAndDOMOnlyOperations(t *testing.T) {
	f := workerFixture(t)
	s := NewSurface("arena")
	t.Cleanup(s.Dispose)
	fps := 30.0
	if err := s.Update(SurfaceOptions{MaxFrameRate: &fps}); err != nil {
		t.Fatal(err)
	}
	op := f.Get("ops").Index(0)
	if op.Get("op").String() != "call" || op.Get("name").String() != "updateSceneProps" || op.Get("json").String() != `{"maxFrameRate":30}` {
		t.Fatalf("update op: %v", op)
	}
	bad := -1.0
	if err := s.Update(SurfaceOptions{MaxFrameRate: &bad}); err == nil {
		t.Fatal("a negative frame rate must fail")
	}
	if _, err := s.Resize(100, 100); err == nil {
		t.Fatal("Resize needs the canvas, which the page owns")
	}
	if _, err := s.LoseContext(); err == nil {
		t.Fatal("LoseContext needs the canvas, which the page owns")
	}
}

func TestWorkerFramesUseTheSameEntryPointsAsTheMainThread(t *testing.T) {
	f := workerFixture(t)
	frame := PoseFrame{Batches: []PoseBatch{{ID: "crowd", Instances: []MeshInstanceIR{{ID: "actor-0", X: 1, ScaleX: 1, ScaleY: 1, ScaleZ: 1, Animation: "Run", AnimationTime: .25, AnimationLoop: true}}}}}
	var enc PoseFrameEncoder
	wire, err := enc.Encode(frame)
	if err != nil {
		t.Fatal(err)
	}
	wire = append([]byte(nil), wire...)
	if err := DispatchPoseFrame("arena", frame, nil); err != nil {
		t.Fatal(err)
	}
	got := f.Get("frames").Index(0)
	if got.Get("kind").String() != "pose" || got.Get("target").String() != "arena" {
		t.Fatalf("pose dispatch: %v", got)
	}
	copied := make([]byte, got.Get("bytes").Length())
	js.CopyBytesToGo(copied, got.Get("bytes"))
	if !bytes.Equal(copied, wire) {
		t.Fatal("the worker shim received different bytes than the main-thread path encodes")
	}
	// The buffer handed over must be owned exactly, so the shim can transfer it.
	if got.Get("buffer").Get("byteLength").Int() != len(wire) {
		t.Fatal("the dispatched array must own its whole buffer")
	}
}

func TestWorkerStreamWriterQueuesFramesForThePage(t *testing.T) {
	f := workerFixture(t)
	w := NewStreamWriter("arena")
	t.Cleanup(w.Dispose)
	if !w.Prepare() {
		t.Fatal("a worker stream writer is ready without loading a codec")
	}
	frame := benchInstanceStreamFrame(24)
	want, _ := frame.Encode()
	applied, err := w.Apply(frame)
	if err != nil || !applied {
		t.Fatalf("apply = %v, %v", applied, err)
	}
	sent := f.Get("stream").Index(0)
	if sent.Get("target").String() != "arena" {
		t.Fatalf("target %v", sent.Get("target"))
	}
	copied := make([]byte, sent.Get("bytes").Length())
	js.CopyBytesToGo(copied, sent.Get("bytes"))
	if !bytes.Equal(copied, want) {
		t.Fatal("stream bytes differ from the encoded frame")
	}
	// A second frame must not alias the first: the first is already on its way.
	frame.Revision = 2
	if _, err := w.Apply(frame); err != nil {
		t.Fatal(err)
	}
	if f.Get("stream").Length() != 2 {
		t.Fatal("each frame is posted separately")
	}
}
