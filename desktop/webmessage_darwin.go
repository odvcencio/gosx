//go:build darwin && !ios && (amd64 || arm64)

package desktop

import "encoding/json"

const darwinMessageHandler = "gosxDesktop"

// Install the same string transport the public bridge uses on Windows, before
// the bridge bootstrap runs. Only the main frame can send messages to Go.
const darwinTransportBootstrap = `(function () {
  var transport = new EventTarget();
  transport.postMessage = function (message) {
    if (typeof message !== "string") throw new TypeError("desktop messages must be strings");
    window.webkit.messageHandlers.gosxDesktop.postMessage(message);
  };
  window.__gosxNativeReceive = function (message) {
    var event = new MessageEvent("message", {data: message});
    transport.dispatchEvent(event);
    if (typeof transport.onmessage === "function") transport.onmessage(event);
  };
  window.chrome = window.chrome || {};
  window.chrome.webview = transport;
})();`

func (a *darwinApp) receiveWebMessage(message uintptr) {
	if message == 0 || !sendBool0(send0(message, objcSel("frameInfo")), objcSel("isMainFrame")) {
		return
	}
	body := send0(message, objcSel("body"))
	if !sendBool1(body, objcSel("isKindOfClass:"), objcClass("NSString")) {
		return
	}
	raw, ok := cfStringToString(body)
	a.mu.Lock()
	callback, closing := a.options.OnWebMessage, a.closing
	a.mu.Unlock()
	if ok && callback != nil && !closing {
		callback(raw)
	}
}

func (a *darwinApp) PostMessage(message string) error {
	encoded, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return a.ExecuteScript("window.__gosxNativeReceive(" + string(encoded) + ")")
}
