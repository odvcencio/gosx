//go:build js && wasm && !gosx_tiny_runtime

package main

import (
	"syscall/js"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/crdt"
)

func registerCRDTRuntime() {
	crdtBridge := bridge.NewCRDTBridge()
	setRuntimeFunc("__gosx_crdt_init", crdtInitRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_sync", crdtSyncRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_put", crdtPutRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_get", crdtGetRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_make_text", crdtMakeTextRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_insert_at", crdtInsertAtRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_delete_at", crdtDeleteAtRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_commit", crdtCommitRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_save", crdtSaveRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_load", crdtLoadRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_text_to_string", crdtTextToStringRuntimeFunc(crdtBridge))
	setRuntimeFunc("__gosx_crdt_get_obj_id", crdtGetObjIDRuntimeFunc(crdtBridge))
}

func crdtInitRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Type() == js.TypeUndefined || args[0].Type() == js.TypeNull {
			if err := b.InitDoc(nil); err != nil {
				return jsError(err)
			}
			return js.Null()
		}
		data, err := decodeProgramData(args[0])
		if err != nil {
			return jsError(err)
		}
		if err := b.InitDoc(data); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func crdtSyncRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (syncMsg)")
		}
		msg, err := decodeProgramData(args[0])
		if err != nil {
			return jsError(err)
		}
		reply, err := b.Sync(msg)
		if err != nil {
			return jsError(err)
		}
		if len(reply) == 0 {
			return js.Null()
		}
		return bytesToJS(reply)
	})
}

func crdtPutRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (obj, prop, value)")
		}
		valueJSON, err := normalizeJSONArg(args[2], "null")
		if err != nil {
			return jsError(err)
		}
		if err := b.Put(crdt.ObjID(args[0].String()), crdt.Prop(args[1].String()), valueJSON); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func crdtGetRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return jsErrorf("need 2 args (obj, prop)")
		}
		valueJSON, err := b.Get(crdt.ObjID(args[0].String()), crdt.Prop(args[1].String()))
		if err != nil {
			return jsError(err)
		}
		return js.Global().Get("JSON").Call("parse", valueJSON)
	})
}

func crdtMakeTextRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return jsErrorf("need 2 args (obj, prop)")
		}
		objID, err := b.MakeText(crdt.ObjID(args[0].String()), crdt.Prop(args[1].String()))
		if err != nil {
			return jsError(err)
		}
		return js.ValueOf(objID)
	})
}

func crdtInsertAtRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (list, index, value)")
		}
		valueJSON, err := normalizeJSONArg(args[2], "null")
		if err != nil {
			return jsError(err)
		}
		if err := b.InsertAt(crdt.ObjID(args[0].String()), uint64(args[1].Int()), valueJSON); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func crdtDeleteAtRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return jsErrorf("need 2 args (list, index)")
		}
		if err := b.DeleteAt(crdt.ObjID(args[0].String()), uint64(args[1].Int())); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func crdtCommitRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		msg := ""
		if len(args) > 0 && args[0].Type() == js.TypeString {
			msg = args[0].String()
		}
		if err := b.Commit(msg); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func crdtSaveRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		data, err := b.Save()
		if err != nil {
			return jsError(err)
		}
		return bytesToJS(data)
	})
}

func crdtLoadRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (data)")
		}
		data, err := decodeProgramData(args[0])
		if err != nil {
			return jsError(err)
		}
		if err := b.LoadDoc(data); err != nil {
			return jsError(err)
		}
		return js.Null()
	})
}

func crdtTextToStringRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return jsErrorf("need 1 arg (text)")
		}
		str, err := b.TextToString(crdt.ObjID(args[0].String()))
		if err != nil {
			return jsError(err)
		}
		return js.ValueOf(str)
	})
}

func crdtGetObjIDRuntimeFunc(b *bridge.CRDTBridge) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			return jsErrorf("need 2 args (obj, prop)")
		}
		objID, err := b.GetObjID(crdt.ObjID(args[0].String()), crdt.Prop(args[1].String()))
		if err != nil {
			return jsError(err)
		}
		return js.ValueOf(objID)
	})
}

func bytesToJS(data []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(arr, data)
	return arr
}
