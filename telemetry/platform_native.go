//go:build !js || !wasm

package telemetry

func platformEnable() error { return nil }
