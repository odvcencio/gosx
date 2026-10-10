//go:build !(js && wasm)

package browser

import "errors"

var errPlatformUnsupported = errors.New("browser services require js/wasm")

type Timer struct{}

func Now() float64                               { return 0 }
func WallNow() float64                           { return 0 }
func Timeout(func(), float64) *Timer             { return nil }
func Interval(func(), float64) *Timer            { return nil }
func (*Timer) Active() bool                      { return false }
func (*Timer) Stop()                             {}
func (*Timer) Dispose()                          {}
func CurrentLocation() Location                  { return Location{} }
func Navigate(string)                            {}
func ReplaceHistory(string)                      {}
func Reload()                                    {}
func Viewport() Size                             { return Size{} }
func PixelRatio() float64                        { return 1 }
func DispatchResize()                            {}
func LogError(string)                            {}
func (JSONCapture) Enabled() bool                { return false }
func (JSONCapture) Publish([]byte, string) error { return errPlatformUnsupported }
