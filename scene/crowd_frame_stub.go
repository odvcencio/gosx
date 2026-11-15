//go:build !js || !wasm

package scene

import "errors"

// DispatchCrowdFrame is available only in a browser WASM runtime. Native
// code encodes frames with EncodeCrowdFrame and sends them to the page.
func DispatchCrowdFrame(string, CrowdFrame) error {
	return errors.New("Scene3D crowd frame dispatch requires js/wasm")
}
