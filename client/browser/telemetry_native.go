//go:build !(js && wasm)

package browser

func TelemetryConfigEnabled() (bool, bool) { return true, false }
func SetTelemetryEnabled(bool)             {}
func TelemetrySession() string             { return "" }

func TelemetryReady() bool                                      { return false }
func EmitTelemetry(string, string, string, map[string]any) bool { return false }
