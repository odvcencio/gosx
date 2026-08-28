//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"
)

// TestUint8ArrayBytesRejectsNonObjectInputsWithoutPanicking covers B2: a
// browser caller can invoke the facade ABI mailbox with any JS value.
// value.Get and js.CopyBytesToGo both panic on non-object inputs, so
// uint8ArrayBytes must reject those inputs with an error instead of letting
// the panic reach the caller and kill the runtime.
func TestUint8ArrayBytesRejectsNonObjectInputsWithoutPanicking(t *testing.T) {
	cases := []struct {
		name  string
		value js.Value
	}{
		{"number", js.ValueOf(1)},
		{"string", js.ValueOf("not an array")},
		{"boolean", js.ValueOf(true)},
		{"undefined", js.Undefined()},
		{"null", js.Null()},
		{"plain object with numeric length", js.Global().Get("Object").Call("assign", js.Global().Get("Object").New(), map[string]any{"length": 4})},
		{"array (not a Uint8Array)", js.Global().Get("Array").New(4)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("uint8ArrayBytes panicked on %s input: %v", tc.name, r)
				}
			}()
			_, err := uint8ArrayBytes(tc.value)
			if err == nil {
				t.Fatalf("expected an error for %s input, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "Uint8Array") {
				t.Fatalf("expected Uint8Array error for %s input, got %q", tc.name, err.Error())
			}
		})
	}
}

// TestUint8ArrayBytesAcceptsUint8ArrayAndArrayBuffer confirms the happy path
// still works for both accepted input shapes after the type guard.
func TestUint8ArrayBytesAcceptsUint8ArrayAndArrayBuffer(t *testing.T) {
	want := []byte{1, 2, 3, 4}

	uint8Array := js.Global().Get("Uint8Array").New(len(want))
	js.CopyBytesToJS(uint8Array, want)

	got, err := uint8ArrayBytes(uint8Array)
	if err != nil {
		t.Fatalf("uint8ArrayBytes(Uint8Array) returned an error: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("uint8ArrayBytes(Uint8Array) = %v, want %v", got, want)
	}

	arrayBuffer := uint8Array.Get("buffer")
	got, err = uint8ArrayBytes(arrayBuffer)
	if err != nil {
		t.Fatalf("uint8ArrayBytes(ArrayBuffer) returned an error: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("uint8ArrayBytes(ArrayBuffer) = %v, want %v", got, want)
	}
}

// TestRuntimeMailboxRejectsMalformedInputWithoutPanicking exercises the
// registered facade ABI mailbox export end to end: calling it with a
// bare number or a plain object shaped like an array must return the ABI's
// string error encoding, not panic and kill the runtime.
func TestRuntimeMailboxRejectsMalformedInputWithoutPanicking(t *testing.T) {
	registerRuntimeABI(nil)

	mailbox := runtimeFacade().Get("abi").Get("mailbox")
	if mailbox.Type() != js.TypeFunction {
		t.Fatal("expected the facade ABI mailbox to be registered")
	}

	malformed := []struct {
		name  string
		value any
	}{
		{"bare number", 1},
		{"object with length", map[string]any{"length": 4}},
	}

	for _, tc := range malformed {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("mailbox call panicked on %s input: %v", tc.name, r)
				}
			}()
			ret := mailbox.Invoke(js.ValueOf(tc.value))
			if ret.Type() != js.TypeString || !strings.HasPrefix(ret.String(), "error:") {
				t.Fatalf("expected an error: string result for %s input, got %v (%q)", tc.name, ret.Type(), ret.String())
			}
		})
	}
}
