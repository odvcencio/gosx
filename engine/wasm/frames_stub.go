//go:build !js || !wasm

package wasm

import "context"

// EachFrame is available only on js/wasm.
func EachFrame(context.Context, func(Frame) error) error { return ErrUnsupported }
