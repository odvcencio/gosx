//go:build js && wasm

package wasm

import (
	"context"
	"encoding/json"
	"errors"
	"syscall/js"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
)

func TestSignalJSONHandoffAvoidsRedundantBrowserConversions(t *testing.T) {
	host := js.Global().Call("eval", `({calls:0,setSignalsJSON(raw,changed){this.calls++;this.raw=raw;this.changed=changed;},subscribeSignalJSON(name,callback){this.callback=callback;return ()=>{this.callback=null}},deliver(raw){if(this.callback)this.callback(raw)}})`)
	ctx := Context{value: host}
	if err := ctx.SetChangedSignals(map[string]any{"$view": map[string]any{"turn": "west"}}); err != nil {
		t.Fatal(err)
	}
	if host.Get("raw").String() != `{"$view":{"turn":"west"}}` || !host.Get("changed").Bool() {
		t.Fatal("encoded snapshot/changed-only flag lost")
	}
	if err := ctx.SetSignals(map[string]any{"$pulse": true}); err != nil || host.Get("changed").Bool() {
		t.Fatal("pulse batch changed semantics")
	}
	if err := ctx.SetChangedSignals(map[string]any{"$bad": make(chan int)}); err == nil || host.Get("calls").Int() != 2 {
		t.Fatal("invalid batch reached the browser")
	}
	var got json.RawMessage
	dispose, err := ctx.SubscribeSignal("$view", func(raw json.RawMessage) { got = raw })
	if err != nil {
		t.Fatal(err)
	}
	host.Call("deliver", `{"turn":"west","hand":[1,2]}`)
	if string(got) != `{"turn":"west","hand":[1,2]}` {
		t.Fatal("raw subscriber data was re-encoded")
	}
	dispose.Dispose()
	host.Call("deliver", `{"turn":"east"}`)
	if string(got) != `{"turn":"west","hand":[1,2]}` {
		t.Fatal("disposed subscriber called")
	}
}

func TestTypedSignalReadAndBatchFailureIsAtomic(t *testing.T) {
	host := js.Global().Call("eval", `({values:{"$selected":"tile"}, calls:0, getSignal(n){return this.values[n]}, setSignals(v){this.calls++; Object.assign(this.values,v)}})`)
	engine := Context{value: host}
	value, err := ReadSignal[string](engine, "$selected")
	if err != nil || value != "tile" {
		t.Fatalf("read=%q: %v", value, err)
	}
	if _, err = ReadSignal[int](engine, "$missing"); !errors.Is(err, ErrSignalNotFound) {
		t.Fatalf("missing=%v", err)
	}
	if err = engine.SetSignals(map[string]any{"$selected": "next", "$count": 2}); err != nil {
		t.Fatal(err)
	}
	if value, err = ReadSignal[string](engine, "$selected"); err != nil || value != "next" {
		t.Fatalf("updated=%q %v", value, err)
	}
	if err = engine.SetSignals(map[string]any{"$selected": "bad", "$invalid": make(chan int)}); err == nil {
		t.Fatal("non-JSON batch accepted")
	}
	if host.Get("calls").Int() != 1 || host.Get("values").Get("$selected").String() != "next" {
		t.Fatal("invalid batch partially committed")
	}
}

func TestSceneCommandsCameraAndCancellation(t *testing.T) {
	host := js.Global().Call("eval", `({current:true, isCurrent(){return this.current}, calls:[], scene3D(method,mount,...args){
   this.calls.push(method); this.mount=mount; this.method=method;
   if(method==='getCamera') return {kind:'perspective', x:3,y:4,z:5,fov:41,near:.5,far:90};
   this.signal=args.at(-1).signal;
   if(method==='whenReady') return new Promise(resolve => {this.complete=resolve});
   if(method==='live'){this.liveEvent=args[0]; this.payload=args[1]; return Promise.resolve();}
   this.payload=args[0]; return Promise.resolve();
 }})`)
	engine := Context{value: host}
	mount := js.Global().Get("Object").New()
	target := engine.Scene3D(mount)
	camera, err := target.Camera()
	if err != nil || camera.X != 3 || camera.FOV != 41 {
		t.Fatalf("camera=%+v: %v", camera, err)
	}
	if err = target.SetCamera(context.Background(), scene.PerspectiveCamera{Position: scene.Vector3{X: 8}, FOV: 50, Near: .25, Far: 120}); err != nil {
		t.Fatal(err)
	}
	if host.Get("payload").Get("fov").Float() != 50 || !host.Get("mount").Equal(mount) {
		t.Fatal("camera payload/mount lost")
	}
	if err = target.Dispatch(context.Background(), []scene.Command{scene.SetCameraCommand(camera)}); err != nil {
		t.Fatal(err)
	}
	if host.Get("payload").Length() != 1 {
		t.Fatal("commands not forwarded")
	}
	if err = target.Live(context.Background(), "attack", map[string]any{"rig": map[string]any{"animationSeq": "hit-1"}}); err != nil {
		t.Fatal(err)
	}
	if host.Get("method").String() != "live" || host.Get("liveEvent").String() != "attack" ||
		host.Get("payload").Get("rig").Get("animationSeq").String() != "hit-1" || !host.Get("mount").Equal(mount) {
		t.Fatal("live event, data or mount lost")
	}
	if err = target.Live(context.Background(), "", nil); err == nil {
		t.Fatal("empty live event name accepted")
	}
	if err = target.SetAnimationClock(context.Background(), 2.5, true); err != nil {
		t.Fatal(err)
	}
	if !host.Get("payload").Get("paused").Bool() || host.Get("payload").Get("timeSeconds").Float() != 2.5 {
		t.Fatal("clock payload lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(time.Millisecond); cancel() }()
	if err = target.Ready(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("ready=%v", err)
	}
	if !host.Get("signal").Get("aborted").Bool() {
		t.Fatal("renderer wait survived cancellation")
	}
	host.Call("complete") // no call into a released Go callback
	host.Set("current", false)
	if err = target.Dispatch(context.Background(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("disposed dispatch=%v", err)
	}
}

func TestNavigationOptionsAndCanceledWait(t *testing.T) {
	host := js.Global().Call("eval", `({navigate(target,options){this.target=target;this.options=options;return new Promise(resolve=>{this.complete=resolve})}})`)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(time.Millisecond); cancel() }()
	err := (Context{value: host}).Navigate(ctx, "/room/next", NavigationOptions{Replace: true, Force: true, Revalidate: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("navigate=%v", err)
	}
	if host.Get("target").String() != "/room/next" || !host.Get("options").Get("revalidate").Bool() {
		t.Fatal("navigation options lost")
	}
	host.Call("complete")
}
