//go:build js && wasm

package browser

import (
	"fmt"
	"syscall/js"

	"m31labs.dev/gosx/client/jsutil"
)

func desktopValue() (bridge js.Value, err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("browser: desktop bridge: %v", value)
		}
	}()
	bridge = js.Global().Get("gosxDesktop")
	if bridge.Type() != js.TypeObject || bridge.IsNull() {
		return js.Undefined(), ErrDesktopUnavailable
	}
	brand := bridge.Get("__gosxDesktopBridge")
	if brand.Type() != js.TypeBoolean || !brand.Bool() {
		return js.Undefined(), ErrDesktopUnavailable
	}
	return bridge, nil
}
func (d DesktopBridge) ServicesAvailable() (available bool) {
	defer func() {
		if recover() != nil {
			available = false
		}
	}()
	bridge, err := desktopValue()
	return err == nil && bridge.Get("service").Type() == js.TypeFunction
}
func (d DesktopBridge) DiagnosticsAvailable() bool { return d.ServicesAvailable() }
func (d DesktopBridge) PrivacyAvailable() bool     { return d.ServicesAvailable() }
func (d DesktopBridge) WindowAvailable() (available bool) {
	defer func() {
		if recover() != nil {
			available = false
		}
	}()
	bridge, err := desktopValue()
	if err != nil {
		return false
	}
	window := bridge.Get("window")
	return window.Type() == js.TypeObject && !window.IsNull() && window.Get("setFullscreen").Type() == js.TypeFunction
}

// invoke resolves only a named framework-owned service method. It deliberately
// stays private; applications consume the typed operations below.
func (d DesktopBridge) invoke(service, method string, args []any, done func(js.Value, error)) {
	var promise js.Value
	err := func() (err error) {
		defer func() {
			if value := recover(); value != nil {
				err = fmt.Errorf("browser: desktop %s: %v", method, value)
			}
		}()
		if err = d.context().Err(); err != nil {
			return err
		}
		bridge, err := desktopValue()
		if err != nil {
			return err
		}
		var target js.Value
		if service == "" {
			target = bridge.Get("window")
		} else {
			if bridge.Get("service").Type() != js.TypeFunction {
				return ErrDesktopUnavailable
			}
			target = bridge.Call("service", service)
		}
		if target.Type() != js.TypeObject || target.IsNull() || target.Get(method).Type() != js.TypeFunction {
			return ErrDesktopUnavailable
		}
		promise = target.Call(method, args...)
		if promise.Type() != js.TypeObject || promise.IsNull() || promise.Get("then").Type() != js.TypeFunction {
			return fmt.Errorf("browser: desktop %s did not return a promise", method)
		}
		return nil
	}()
	if err != nil {
		if done != nil {
			done(js.Undefined(), err)
		}
		return
	}
	go func() {
		value, err := jsutil.AwaitPromiseContext(d.context(), promise)
		if done != nil {
			done(value, err)
		}
	}()
}
func (d DesktopBridge) SetFullscreen(enabled bool, done func(error)) {
	d.invoke("", "setFullscreen", []any{enabled}, desktopDone(done))
}
func desktopDone(done func(error)) func(js.Value, error) {
	return func(_ js.Value, err error) {
		if done != nil {
			done(err)
		}
	}
}
func (d DesktopBridge) PreviewDiagnostics(done func(DiagnosticsPlan, error)) {
	d.invoke("diagnostics", "preview", nil, func(value js.Value, err error) {
		var plan DiagnosticsPlan
		if err == nil {
			err = func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("browser: diagnostics plan: %v", r)
					}
				}()
				plan.ID = value.Get("id").String()
				plan.TotalSizeText = value.Get("totalSizeText").String()
				files := value.Get("files")
				plan.Files = make([]DiagnosticsFile, files.Length())
				for i := range plan.Files {
					file := files.Index(i)
					plan.Files[i] = DiagnosticsFile{Name: file.Get("name").String(), Size: file.Get("size").Float()}
				}
				return nil
			}()
		}
		if done != nil {
			done(plan, err)
		}
	})
}
func (d DesktopBridge) ExportDiagnostics(planID string, done func(error)) {
	d.invoke("diagnostics", "export", []any{planID}, desktopDone(done))
}
func (d DesktopBridge) boolean(service, method string, done func(bool, error)) {
	d.invoke(service, method, nil, func(value js.Value, err error) {
		enabled := false
		if err == nil {
			if value.Type() != js.TypeBoolean {
				err = fmt.Errorf("browser: desktop %s returned a non-boolean", method)
			} else {
				enabled = value.Bool()
			}
		}
		if done != nil {
			done(enabled, err)
		}
	})
}
func (d DesktopBridge) BrowserErrorReports(done func(bool, error)) {
	d.boolean("privacySettings", "sendBrowserErrorReports", done)
}
func (d DesktopBridge) CheckForUpdates(done func(bool, error)) {
	d.boolean("privacySettings", "checkForUpdates", done)
}
func (d DesktopBridge) SetBrowserErrorReports(enabled bool, done func(error)) {
	d.invoke("privacySettings", "setBrowserErrorReports", []any{map[string]any{"enabled": enabled}}, desktopDone(done))
}
func (d DesktopBridge) SetCheckForUpdates(enabled bool, done func(error)) {
	d.invoke("privacySettings", "setCheckForUpdates", []any{map[string]any{"enabled": enabled}}, desktopDone(done))
}
func (d DesktopBridge) DeleteMyData(done func(error)) {
	d.invoke("privacySettings", "deleteMyData", nil, desktopDone(done))
}
