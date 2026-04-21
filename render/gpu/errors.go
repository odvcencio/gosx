package gpu

import "errors"

// ErrUnsupported is returned by backends that cannot provide a requested
// operation on the current platform. The non-WASM stub backend returns this
// from every constructor; a future WebGL2 fallback will return it for
// WebGPU-only features such as compute passes.
var ErrUnsupported = errors.New("gpu: operation unsupported on this backend")

// ErrDeviceLost is returned when the underlying GPU device has been lost
// (driver reset, tab backgrounded too long, etc). Callers should dispose
// resources and recreate the device.
var ErrDeviceLost = errors.New("gpu: device lost")

// ErrInvalidDesc is returned when a descriptor fails local validation
// (negative sizes, conflicting flags, empty vertex layouts). Backends may
// also return backend-specific errors for issues that only surface on the
// GPU.
var ErrInvalidDesc = errors.New("gpu: invalid descriptor")
