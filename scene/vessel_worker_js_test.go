//go:build js && wasm

package scene

import (
	"os"
	"syscall/js"
	"testing"
)

// workerHostFixture runs the real engine-worker host script (the worker half)
// with the test's global object as the worker scope, and records what it posts
// to the page. DispatchVesselPose and DispatchVesselCutaway then go through the
// globals that script installs, not a hand-written stand-in.
func workerHostFixture(t *testing.T) js.Value {
	t.Helper()
	source, err := os.ReadFile("../client/runtime/host/engine-worker.ts")
	if err != nil {
		t.Fatal(err)
	}
	fixture := js.Global().Get("Function").New("source", `
const g = globalThis;
const names = ['self', 'postMessage', 'onmessage', '__gosx', '__gosx_scene3d_vessel_pose', '__gosx_scene3d_vessel_cutaway', '__gosx_worker_extensions'];
const saved = names.map(n => [n, Object.getOwnPropertyDescriptor(g, n)]);
const posted = [];
g.self = g;
g.postMessage = (msg, transfer) => posted.push({ msg, transfer: transfer || [] });
// Declaring "window" as a parameter shadows any global, so the script takes its worker branch.
new Function('window', source)(undefined);
return { posted, restore() { for (const [n, d] of saved) { if (d) Object.defineProperty(g, n, d); else delete g[n]; } } };
`).Invoke(string(source))
	t.Cleanup(func() { fixture.Call("restore") })
	return fixture
}

func TestVesselPoseAndCutawayWorkFromAnEngineWorker(t *testing.T) {
	f := workerHostFixture(t)
	posted := f.Get("posted")

	frame := VesselPoseFrame{Vessels: []VesselPose{{ID: "ship", Time: 1.5, X: 10, Y: 0, Z: -4, Heading: 0.5, Speed: 3}}}
	want, err := EncodeVesselPoseFrame(nil, frame)
	if err != nil {
		t.Fatal(err)
	}
	// Until the page answers, a worker reports what the main thread reports
	// for a mount with no vessel yet.
	if err := DispatchVesselPose("scene", frame); err == nil {
		t.Fatal("the first pose must report that the vessel is not ready")
	}
	reply := func(kind, nodeID string, ok bool) {
		js.Global().Call("onmessage", map[string]any{"data": map[string]any{"t": "vessel.result", "kind": kind, "target": "scene", "nodeID": nodeID, "ok": ok}})
	}
	reply("pose", "", true)
	if err := DispatchVesselPose("scene", frame); err != nil {
		t.Fatalf("DispatchVesselPose after the page accepted a pose: %v", err)
	}
	posted.Call("splice", 0, 1) // keep only the second pose frame
	if posted.Length() != 1 {
		t.Fatalf("posted %d messages, want 1 frame", posted.Length())
	}
	msg := posted.Index(0).Get("msg")
	if msg.Get("t").String() != "frame" || msg.Get("kind").String() != "vessel-pose" || msg.Get("target").String() != "scene" {
		t.Fatalf("pose message = %s", js.Global().Get("JSON").Call("stringify", msg).String())
	}
	if posted.Index(0).Get("transfer").Length() != 1 {
		t.Fatal("the pose buffer must be transferred, not copied again")
	}
	got := make([]byte, js.Global().Get("Uint8Array").New(msg.Get("buf")).Length())
	js.CopyBytesToGo(got, js.Global().Get("Uint8Array").New(msg.Get("buf")))
	if string(got) != string(want) {
		t.Fatalf("pose bytes differ: got %d bytes, want %d", len(got), len(want))
	}

	cut := VesselCutaway{Enabled: true, Deck: 1}
	if err := DispatchVesselCutaway("scene", "ship", cut); err == nil {
		t.Fatal("the first cutaway must report that the vessel is not ready")
	}
	reply("cutaway", "ship", true)
	if err := DispatchVesselCutaway("scene", "ship", cut); err != nil {
		t.Fatalf("DispatchVesselCutaway after the page accepted one: %v", err)
	}
	reply("pose", "", false)
	if err := DispatchVesselPose("scene", frame); err == nil {
		t.Fatal("a mount that stopped showing a vessel must report an error again")
	}
	cm := posted.Index(2).Get("msg")
	if cm.Get("t").String() != "vessel.cutaway" || cm.Get("target").String() != "scene" || cm.Get("nodeID").String() != "ship" {
		t.Fatalf("cutaway message = %s", js.Global().Get("JSON").Call("stringify", cm).String())
	}
	if cm.Get("json").String() != `{"enabled":true,"deck":1}` {
		t.Fatalf("cutaway json = %s", cm.Get("json").String())
	}
}
