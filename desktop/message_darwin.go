//go:build darwin && !ios && (amd64 || arm64)

package desktop

import "fmt"

// showPlatformMessage is the native message box. The darwin backend shows it
// with NSAlert in a later slice (DT1); until then it reports ErrUnsupported, as
// every platform without a backend does, so ShowMessage callers (the example
// host's error path) keep working by ignoring the result.
func showPlatformMessage(MessageOptions, uintptr) (MessageResult, error) {
	return "", fmt.Errorf("%w: native message boxes are not implemented by the darwin backend yet", ErrUnsupported)
}
