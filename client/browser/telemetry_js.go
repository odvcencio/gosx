//go:build js && wasm

package browser

import "syscall/js"

func telemetryGlobal() js.Value {
	global := js.Global()
	if win := global.Get("window"); win.Truthy() {
		return win
	}
	return global
}

// TelemetryConfigEnabled reads an explicitly configured value. Missing or
// inaccessible configuration returns found=false and the default enabled=true.
func TelemetryConfigEnabled() (enabled, found bool) {
	enabled = true
	defer func() {
		if recover() != nil {
			enabled, found = true, false
		}
	}()
	config := telemetryGlobal().Get("__gosx_telemetry_config")
	if config.Type() != js.TypeObject || config.IsNull() {
		return true, false
	}
	value := config.Get("enabled")
	if value.Type() != js.TypeBoolean {
		return true, false
	}
	return value.Bool(), true
}

func SetTelemetryEnabled(enabled bool) {
	defer func() { _ = recover() }()
	global := telemetryGlobal()
	config := global.Get("__gosx_telemetry_config")
	if config.Type() != js.TypeObject || config.IsNull() {
		config = js.Global().Get("Object").New()
		global.Set("__gosx_telemetry_config", config)
	}
	config.Set("enabled", enabled)
	gosx := global.Get("__gosx")
	if gosx.Truthy() {
		telemetry := gosx.Get("telemetry")
		if telemetry.Truthy() {
			telemetry.Set("enabled", enabled)
			if snapshot := telemetry.Get("snapshot"); snapshot.Type() == js.TypeFunction {
				telemetry.Call("snapshot")
			}
		}
	}
}

// TelemetrySession returns the runtime's existing correlation identifier. It
// never creates or persists a browser identifier.
func TelemetrySession() (session string) {
	defer func() {
		if recover() != nil {
			session = ""
		}
	}()
	gosx := telemetryGlobal().Get("__gosx")
	if !gosx.Truthy() {
		return ""
	}
	telemetry := gosx.Get("telemetry")
	if !telemetry.Truthy() || telemetry.Get("session").Type() != js.TypeFunction {
		return ""
	}
	return telemetry.Call("session").String()
}

// TelemetryReady reports whether the runtime emitter is installed.
func TelemetryReady() (ready bool) {
	defer func() {
		if recover() != nil {
			ready = false
		}
	}()
	return telemetryGlobal().Get("__gosx_emit").Type() == js.TypeFunction
}

func EmitTelemetry(level, source, message string, detail map[string]any) (emitted bool) {
	defer func() {
		if recover() != nil {
			emitted = false
		}
	}()
	fn := telemetryGlobal().Get("__gosx_emit")
	if fn.Type() != js.TypeFunction {
		return false
	}
	fn.Invoke(level, source, message, detail)
	return true
}
