//go:build !(js && wasm)

package browser

import "context"

func WriteClipboard(_ string, done func(error)) {
	if done != nil {
		done(errPlatformUnsupported)
	}
}
func FullscreenSupported() bool { return false }
func InFullscreen() bool        { return false }
func RequestFullscreen(done func(error)) {
	if done != nil {
		done(errPlatformUnsupported)
	}
}
func ExitFullscreen(done func(error)) {
	if done != nil {
		done(errPlatformUnsupported)
	}
}
func OnNativeMessage(func(string)) *Listener { return nil }
func NativePostJSON([]byte) error            { return errPlatformUnsupported }
func StartupString(string) string            { return "" }
func Vibrate(int) bool                       { return false }
func DispatchError(string, string) error     { return errPlatformUnsupported }

type Observer struct{}

func ObserveAttributes(Element, []string, func()) *Observer { return nil }
func (*Observer) Dispose()                                  {}
func HardwareConcurrency() int                              { return 0 }

func WriteClipboardContext(_ context.Context, text string, done func(error)) {
	WriteClipboard(text, done)
}
func RequestFullscreenContext(_ context.Context, done func(error)) { RequestFullscreen(done) }
func ExitFullscreenContext(_ context.Context, done func(error))    { ExitFullscreen(done) }
