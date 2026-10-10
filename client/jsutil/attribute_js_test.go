//go:build js && wasm

package jsutil

import (
	"context"
	"errors"
	"syscall/js"
	"testing"
	"time"
)

func TestWaitForAttributeCompletionAndCancellation(t *testing.T) {
	previous := js.Global().Get("MutationObserver")
	defer js.Global().Set("MutationObserver", previous)
	js.Global().Call("eval", `globalThis.MutationObserver=class {constructor(callback){this.callback=callback;} observe(element,options){this.element=element;this.options=options;element.observer=this;} disconnect(){this.element.disconnected=true;this.element.observer=null;}}`)
	element := js.Global().Call("eval", `({value:'false',getAttribute(){return this.value},change(value){this.value=value;if(this.observer)this.observer.callback([])}})`)
	go func() { time.Sleep(time.Millisecond); element.Call("change", "true") }()
	if err := WaitForAttribute(context.Background(), element, "data-ready", "true"); err != nil {
		t.Fatal(err)
	}
	if !element.Get("disconnected").Bool() {
		t.Fatal("completed observer leaked")
	}
	element.Call("change", "false")
	element.Set("disconnected", false)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(time.Millisecond); cancel() }()
	if err := WaitForAttribute(ctx, element, "data-ready", "true"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if !element.Get("disconnected").Bool() {
		t.Fatal("cancelled observer leaked")
	}
	element.Call("change", "true") // no released Go callback remains
	if err := WaitForAttribute(context.Background(), element, "data-ready", "true"); err != nil {
		t.Fatal(err)
	}
}
