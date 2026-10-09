//go:build js && !tinygo

package ir

import (
	"errors"

	"m31labs.dev/gosx/island/aot"
)

// LowerIslandAOT proves scalar contracts with go/types, which is host-only
// (go-islands spec amendment A1 §5). Standard Go js/wasm builds of shared
// packages such as cmd/gosx get this stub so they still compile; TinyGo client
// builds never reference it.
func LowerIslandAOT(src *Program, index int) (aot.Unit, error) {
	return aot.Unit{}, errors.New("ir: AOT admission is host-only")
}
