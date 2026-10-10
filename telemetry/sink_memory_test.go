package telemetry_test

import (
	"m31labs.dev/gosx/telemetry"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

// The helper imports leaf types only, and its signatures match the production
// contract through schema aliases on both native Go and js/wasm.
var _ telemetry.Sink = (*telemetrytest.MemorySink)(nil)
