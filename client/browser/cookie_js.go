//go:build js && wasm

package browser

import (
	"fmt"
	"syscall/js"
)

// Cookie reads the document's cookie string. Unavailable access returns empty.
func Cookie() (value string) {
	defer func() {
		if recover() != nil {
			value = ""
		}
	}()
	reflect := js.Global().Get("Reflect")
	document := reflect.Call("get", telemetryGlobal(), "document")
	return reflect.Call("get", document, "cookie").String()
}

// SetCookie assigns a cookie with caller-selected attributes.
func SetCookie(value string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("browser: cookie: %v", r)
		}
	}()
	reflect := js.Global().Get("Reflect")
	document := reflect.Call("get", telemetryGlobal(), "document")
	if !reflect.Call("set", document, "cookie", value).Bool() {
		return fmt.Errorf("browser: cookie assignment rejected")
	}
	return nil
}
