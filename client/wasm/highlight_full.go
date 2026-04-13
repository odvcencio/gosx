//go:build js && wasm && !gosx_tiny_runtime

package main

import (
	"syscall/js"

	"github.com/odvcencio/gosx/highlight"
)

func registerHighlightRuntime() {
	setRuntimeFunc("__gosx_highlight", highlightRuntimeFunc())
}

func highlightRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 1 {
			return js.ValueOf("")
		}
		lang := highlight.LangGo
		if len(args) > 1 && args[1].Type() == js.TypeString && args[1].String() != "" {
			lang = args[1].String()
		}
		return js.ValueOf(highlight.HTML(lang, args[0].String()))
	})
}
