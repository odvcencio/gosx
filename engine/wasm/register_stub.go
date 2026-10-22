//go:build !js || !wasm

package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"m31labs.dev/gosx/client/browser"
	"m31labs.dev/gosx/engine"
)

// Context is an unavailable browser context on native targets. The string and
// capability accessors keep shared configuration code portable; DOM access is
// intentionally exposed only by the js/wasm implementation.
type Context struct{}

func (Context) ID() string                                { return "" }
func (Context) Kind() engine.Kind                         { return "" }
func (Context) Component() string                         { return "" }
func (Context) ProgramRef() string                        { return "" }
func (Context) RuntimeMode() engine.Runtime               { return engine.RuntimeNone }
func (Context) Capabilities() []engine.Capability         { return nil }
func (Context) RequiredCapabilities() []engine.Capability { return nil }
func (Context) HasCapability(engine.Capability) bool      { return false }
func (Context) PropsJSON() ([]byte, error)                { return nil, ErrUnsupported }
func (Context) DecodeProps(any) error                     { return ErrUnsupported }
func (Context) Emit(string, any) error                    { return ErrUnsupported }
func (Context) SetSignal(string, any) error               { return ErrUnsupported }
func (Context) SubscribeSignal(string, func(json.RawMessage)) (HandleFunc, error) {
	return nil, ErrUnsupported
}

// Register returns ErrUnsupported on native targets after validating inputs.
func Register(component string, factory Factory) error {
	if strings.TrimSpace(component) == "" {
		return fmt.Errorf("Go-WASM engine component is required")
	}
	if factory == nil {
		return fmt.Errorf("Go-WASM engine factory %q is nil", strings.TrimSpace(component))
	}
	return ErrUnsupported
}

func (Context) MountElement() browser.Element                             { return browser.Element{} }
func (Context) SignalJSON(string) (json.RawMessage, error)                { return nil, ErrUnsupported }
func (Context) SetSignals(map[string]any) error                           { return ErrUnsupported }
func (Context) SetChangedSignals(map[string]any) error                    { return ErrUnsupported }
func (Context) Navigate(context.Context, string, NavigationOptions) error { return ErrUnsupported }
func (Context) IsCurrent() bool                                           { return false }

func (Context) subscribeSignalReplay(string, func(json.RawMessage)) (HandleFunc, error) {
	return nil, ErrUnsupported
}
func (Context) declareCalls(string, time.Duration, int, string) {}

func (Context) viewSink() viewSink { return unsupportedViewSink{} }

type unsupportedViewSink struct{}

func (unsupportedViewSink) send([]byte, func(bool)) error  { return ErrUnsupported }
func (unsupportedViewSink) hidden() bool                   { return false }
func (unsupportedViewSink) onVisibility(func()) HandleFunc { return func() {} }
func (unsupportedViewSink) release()                       {}
