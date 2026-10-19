//go:build darwin && !ios && (amd64 || arm64)

package desktop

// Completion handlers are native Blocks. objc_retain does not move a stack
// Block to the heap; the recorder copies it before returning to WebKit.
//
//go:cgo_import_dynamic gosx_Block_copy _Block_copy "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic gosx_Block_release _Block_release "/usr/lib/libSystem.B.dylib"
var fnBlockCopy, fnBlockRelease, fnBlockInvoke uintptr

func webBlockInvoke()

func invokeNativeBlock(block, result uintptr) {
	if block != 0 {
		syscall_syscall(fnBlockInvoke, block, result, 0)
	}
}

func releaseNativeBlock(block uintptr) {
	if block != 0 {
		syscall_syscall(fnBlockRelease, block, 0, 0)
	}
}

// The hook runs only the panel. Tests return real file NSURLs without showing
// a picker, then verify that WKWebView exposes readable JavaScript Files.
var runWebFilePanel = func(panel uintptr) uintptr {
	if runFilePanel(panel) == 1 { // NSModalResponseOK
		return send0(panel, objcSel("URLs"))
	}
	return 0
}

var runWebAlert = func(alert uintptr) uintptr { return send0(alert, objcSel("runModal")) }

func (a *darwinApp) handleWebUI(ev nativeEvent) {
	result := uintptr(0)
	ownedResult := uintptr(0)
	// The ring releases the copied Block after this returns. Even a request
	// already queued when the window closes gets exactly one completion.
	defer func() {
		invokeNativeBlock(ev.completion, result)
		objcRelease(ownedResult)
	}()
	a.mu.Lock()
	closing := a.closing || a.webview == 0
	a.mu.Unlock()
	if closing {
		return
	}
	if ev.kind == evWebFilePanel {
		panel := send0(objcClass("NSOpenPanel"), objcSel("openPanel"))
		if panel == 0 {
			return
		}
		directories := sendBool0(ev.args[0], objcSel("allowsDirectories"))
		send1(panel, objcSel("setCanChooseFiles:"), boolArg(!directories))
		send1(panel, objcSel("setCanChooseDirectories:"), boolArg(directories))
		send1(panel, objcSel("setAllowsMultipleSelection:"), boolArg(sendBool0(ev.args[0], objcSel("allowsMultipleSelection"))))
		// WKOpenPanelParameters exposes no public accept/MIME filter API.
		// WebKit retains ownership of the HTML input's accept semantics.
		ownedResult = objcRetain(runWebFilePanel(panel))
		result = ownedResult
		return
	}
	frame := ev.args[1]
	if ev.kind == evWebPrompt {
		frame = ev.args[2]
	}
	request := send0(frame, objcSel("request"))
	origin, _ := cfStringToString(send0(send0(request, objcSel("URL")), objcSel("host")))
	if origin == "" {
		origin = "This page"
	}
	message, _ := cfStringToString(ev.args[0])
	buttons := []alertButton{{title: "OK"}}
	if ev.kind != evWebAlert {
		buttons = append(buttons, alertButton{title: "Cancel"})
	}
	alert := newAlert(MessageOptions{Title: origin, Text: message}, buttons)
	if alert == nil {
		return
	}
	defer alert.Release()
	var input *objcRef
	if ev.kind == evWebPrompt {
		input = ownObjC(sendRect(send0(objcClass("NSTextField"), objcSel("alloc")), objcSel("initWithFrame:"), &nsRect{w: 320, h: 24}, 0, 0, 0))
		if input == nil {
			return
		}
		defer input.Release()
		defaultText, _ := cfStringToString(ev.args[1])
		send1(input.obj, objcSel("setStringValue:"), nsString(defaultText))
		send1(alert.obj, objcSel("setAccessoryView:"), input.obj)
	}
	if runWebAlert(alert.obj) == nsAlertFirstButtonReturn {
		switch ev.kind {
		case evWebConfirm:
			result = 1
		case evWebPrompt:
			ownedResult = objcRetain(send0(input.obj, objcSel("stringValue")))
			result = ownedResult
		}
	}
}
