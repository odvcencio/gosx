//go:build js && wasm && !gosx_tiny_runtime

package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/odvcencio/gosx/textlayout"
)

func registerTextLayoutRuntime() {
	setRuntimeFunc("__gosx_text_layout", textLayoutRuntimeFunc())
	setRuntimeFunc("__gosx_text_layout_metrics", textLayoutMetricsRuntimeFunc())
	setRuntimeFunc("__gosx_text_layout_ranges", textLayoutRangesRuntimeFunc())
}

func textLayoutRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (text, font, maxWidth)")
		}

		ws := textlayout.WhiteSpaceNormal
		if len(args) > 3 && args[3].Type() == js.TypeString && args[3].String() != "" {
			ws = textlayout.WhiteSpace(args[3].String())
		}

		lineHeight := 1.0
		if len(args) > 4 && args[4].Type() == js.TypeNumber {
			lineHeight = args[4].Float()
		}

		measurer, err := textlayout.NewBrowserMeasurer()
		if err != nil {
			return jsError(err)
		}

		result, err := textlayout.LayoutText(
			args[0].String(),
			measurer,
			args[1].String(),
			textlayout.PrepareOptions{WhiteSpace: ws},
			parseTextLayoutLayoutOptions(args, lineHeight),
		)
		if err != nil {
			return jsError(err)
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return jsError(err)
		}
		return js.Global().Get("JSON").Call("parse", string(resultJSON))
	})
}

func textLayoutMetricsRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (text, font, maxWidth)")
		}

		ws := textlayout.WhiteSpaceNormal
		if len(args) > 3 && args[3].Type() == js.TypeString && args[3].String() != "" {
			ws = textlayout.WhiteSpace(args[3].String())
		}

		lineHeight := 1.0
		if len(args) > 4 && args[4].Type() == js.TypeNumber {
			lineHeight = args[4].Float()
		}

		measurer, err := textlayout.NewBrowserMeasurer()
		if err != nil {
			return jsError(err)
		}

		result, err := textlayout.LayoutTextMetrics(
			args[0].String(),
			measurer,
			args[1].String(),
			textlayout.PrepareOptions{WhiteSpace: ws},
			parseTextLayoutLayoutOptions(args, lineHeight),
		)
		if err != nil {
			return jsError(err)
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return jsError(err)
		}
		return js.Global().Get("JSON").Call("parse", string(resultJSON))
	})
}

func textLayoutRangesRuntimeFunc() js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) < 3 {
			return jsErrorf("need 3 args (text, font, maxWidth)")
		}

		ws := textlayout.WhiteSpaceNormal
		if len(args) > 3 && args[3].Type() == js.TypeString && args[3].String() != "" {
			ws = textlayout.WhiteSpace(args[3].String())
		}

		lineHeight := 1.0
		if len(args) > 4 && args[4].Type() == js.TypeNumber {
			lineHeight = args[4].Float()
		}

		measurer, err := textlayout.NewBrowserMeasurer()
		if err != nil {
			return jsError(err)
		}

		result, err := textlayout.LayoutTextRanges(
			args[0].String(),
			measurer,
			args[1].String(),
			textlayout.PrepareOptions{WhiteSpace: ws},
			parseTextLayoutLayoutOptions(args, lineHeight),
		)
		if err != nil {
			return jsError(err)
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return jsError(err)
		}
		return js.Global().Get("JSON").Call("parse", string(resultJSON))
	})
}

func parseTextLayoutLayoutOptions(args []js.Value, lineHeight float64) textlayout.LayoutOptions {
	opts := textlayout.LayoutOptions{
		MaxWidth:   args[2].Float(),
		LineHeight: lineHeight,
	}
	if len(args) <= 5 || args[5].Type() != js.TypeObject {
		return opts
	}
	options := args[5]
	if maxLines := options.Get("maxLines"); maxLines.Type() == js.TypeNumber {
		opts.MaxLines = maxLines.Int()
	}
	if overflow := options.Get("overflow"); overflow.Type() == js.TypeString && overflow.String() != "" {
		opts.Overflow = textlayout.OverflowMode(overflow.String())
	}
	return opts
}
