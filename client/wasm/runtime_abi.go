//go:build js && wasm

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/bridge"
	runtimewasm "m31labs.dev/gosx/client/runtime/wasm"
)

// registerRuntimeABI publishes the typed direct surface under the single
// browser facade. The browser can inspect this object before choosing a
// feature route without growing the ambient global namespace.
func registerRuntimeABI(b *bridge.Bridge) {
	handshake := runtimewasm.NewHandshake(runtimeVariant())
	handshakeFn := js.FuncOf(func(this js.Value, args []js.Value) any {
		return handshakeJSValue(handshake)
	})
	mailboxFn := js.FuncOf(runtimeMailboxFunc)
	api := js.Global().Get("Object").New()
	api.Set("abiVersion", int(runtimewasm.ABIVersion))
	api.Set("featureMask", int(handshake.FeatureMask))
	api.Set("variant", string(handshake.Variant))
	api.Set("mailboxVersion", int(runtimewasm.MailboxVersion))
	api.Set("handshake", handshakeFn)
	api.Set("mailbox", mailboxFn)
	api.Set("validate", js.FuncOf(func(this js.Value, args []js.Value) any {
		required := runtimewasm.FeatureMask(0)
		if len(args) > 0 && args[0].Type() == js.TypeNumber {
			required = runtimewasm.FeatureMask(args[0].Int())
		}
		return handshake.Supports(required)
	}))
	runtimeFacade().Set("abi", api)
	_ = b // The bridge owns the patch mailbox callback registered by main.go.
}

func runtimeFacade() js.Value {
	object := js.Global().Get("Object")
	facade := js.Global().Get("__gosx")
	if facade.Type() != js.TypeObject {
		facade = object.New()
		js.Global().Set("__gosx", facade)
	}
	runtime := facade.Get("runtime")
	if runtime.Type() != js.TypeObject {
		runtime = object.New()
		facade.Set("runtime", runtime)
	}
	return runtime
}

func handshakeJSValue(handshake runtimewasm.Handshake) js.Value {
	value := js.Global().Get("Object").New()
	value.Set("abiVersion", int(handshake.ABIVersion))
	value.Set("featureMask", int(handshake.FeatureMask))
	value.Set("variant", string(handshake.Variant))
	value.Set("mailboxVersion", int(handshake.MailboxVersion))
	value.Set("manifestHash", handshake.ManifestHash)
	return value
}

func runtimeMailboxFunc(this js.Value, args []js.Value) any {
	if len(args) < 1 {
		return jsError(errors.New("runtime mailbox requires a Uint8Array"))
	}
	request, err := uint8ArrayBytes(args[0])
	if err != nil {
		return jsError(err)
	}
	header, payload, err := runtimewasm.DecodeMailbox(request)
	if err != nil {
		return jsError(err)
	}
	responsePayload := []byte{}
	status := runtimewasm.MailboxStatusOK
	switch header.Opcode {
	case runtimewasm.MailboxOpcodeHandshake:
		if len(payload) != 0 && len(payload) != 4 {
			status = -1
			responsePayload = []byte("handshake request must contain a uint32 feature mask")
			break
		}
		required := runtimewasm.FeatureMask(0)
		if len(payload) == 4 {
			required = runtimewasm.FeatureMask(binary.LittleEndian.Uint32(payload))
		}
		handshake := runtimewasm.NewHandshake(runtimeVariant())
		if !handshake.Supports(required) {
			status = -1
			responsePayload = []byte("runtime does not expose the requested feature mask")
			break
		}
		responsePayload, err = runtimewasm.EncodeHandshakePayload(handshake)
	case runtimewasm.MailboxOpcodePing:
		responsePayload = append([]byte(nil), payload...)
	default:
		status = -1
		responsePayload = []byte(fmt.Sprintf("unsupported runtime mailbox opcode %d", header.Opcode))
	}
	if err != nil {
		status = -1
		responsePayload = []byte(err.Error())
	}
	response, err := runtimewasm.EncodeMailbox(header.Opcode, header.RequestID, status, runtimewasm.MailboxFlagResponse, responsePayload)
	if err != nil {
		return jsError(err)
	}
	return bytesToUint8Array(response)
}

func uint8ArrayBytes(value js.Value) ([]byte, error) {
	// value.Get and js.CopyBytesToGo both panic on non-object inputs, so the
	// type must be verified before either is called. A caller can invoke the
	// mailbox with any JS value (a number, a string, a plain object without
	// the right prototype), and none of that may crash the runtime.
	if value.Type() != js.TypeObject {
		return nil, errors.New("runtime mailbox requires a Uint8Array or ArrayBuffer")
	}
	if value.InstanceOf(js.Global().Get("ArrayBuffer")) {
		value = js.Global().Get("Uint8Array").New(value)
	}
	if !value.InstanceOf(js.Global().Get("Uint8Array")) {
		return nil, errors.New("runtime mailbox requires a Uint8Array or ArrayBuffer")
	}
	length := value.Get("length").Int()
	if length < 0 || uint64(length) > uint64(runtimewasm.MaxMailboxPayload+runtimewasm.MailboxHeaderSize) {
		return nil, errors.New("runtime mailbox exceeds the size limit")
	}
	data := make([]byte, length)
	js.CopyBytesToGo(data, value)
	return data, nil
}
