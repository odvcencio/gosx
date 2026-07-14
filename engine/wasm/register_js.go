//go:build js && wasm

package wasm

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall/js"

	"m31labs.dev/gosx/engine"
)

const registrationTokenEnv = "GOSX_GO_WASM_REGISTRATION_TOKEN"

var (
	factoryMu    sync.Mutex
	factoryFuncs []js.Func
)

// Context is the browser-owned mount context passed to a Go-WASM factory.
// Value, Mount, and Props expose syscall/js values because engines are the
// unrestricted client-computation tier rather than the constrained island VM.
type Context struct {
	value js.Value
}

// Value returns the complete JavaScript engine context.
func (c Context) Value() js.Value { return c.value }

// Mount returns the engine's DOM mount, or js.Null for worker engines.
func (c Context) Mount() js.Value { return c.value.Get("mount") }

// Props returns the manifest-decoded props object.
func (c Context) Props() js.Value { return c.value.Get("props") }

// ID returns the unique engine instance ID.
func (c Context) ID() string { return stringProperty(c.value, "id") }

// Kind returns worker, surface, or video.
func (c Context) Kind() engine.Kind { return engine.Kind(stringProperty(c.value, "kind")) }

// Component returns the registered component name.
func (c Context) Component() string { return stringProperty(c.value, "component") }

// ProgramRef returns the exact module URL used to boot this engine.
func (c Context) ProgramRef() string { return stringProperty(c.value, "programRef") }

// RuntimeMode returns the manifest runtime selector.
func (c Context) RuntimeMode() engine.Runtime {
	return engine.Runtime(stringProperty(c.value, "runtimeMode"))
}

// Capabilities returns the capabilities declared for this instance.
func (c Context) Capabilities() []engine.Capability {
	return capabilityValues(c.value.Get("capabilities"))
}

// RequiredCapabilities returns the hard-gated capabilities for this instance.
func (c Context) RequiredCapabilities() []engine.Capability {
	return capabilityValues(c.value.Get("requiredCapabilities"))
}

// HasCapability reports whether capability was declared as optional or required
// for the instance.
func (c Context) HasCapability(capability engine.Capability) bool {
	want := strings.TrimSpace(strings.ToLower(string(capability)))
	for _, candidates := range [][]engine.Capability{c.Capabilities(), c.RequiredCapabilities()} {
		for _, candidate := range candidates {
			if strings.TrimSpace(strings.ToLower(string(candidate))) == want {
				return true
			}
		}
	}
	return false
}

// PropsJSON serializes the decoded props object.
func (c Context) PropsJSON() ([]byte, error) {
	stringify := js.Global().Get("JSON").Get("stringify")
	if stringify.Type() != js.TypeFunction {
		return nil, fmt.Errorf("JSON.stringify is unavailable")
	}
	encoded := stringify.Invoke(c.Props())
	if encoded.Type() != js.TypeString {
		return nil, fmt.Errorf("engine props are not serializable")
	}
	return []byte(encoded.String()), nil
}

// DecodeProps decodes the instance props into dst.
func (c Context) DecodeProps(dst any) error {
	data, err := c.PropsJSON()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// Emit dispatches a gosx:engine:<name> CustomEvent through the framework
// context.
func (c Context) Emit(name string, detail any) error {
	return emitContextEvent(c.value, name, detail)
}

func emitContextEvent(context js.Value, name string, detail any) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("engine emit failed: %v", recovered)
		}
	}()
	emit := context.Get("emit")
	if emit.Type() != js.TypeFunction {
		return fmt.Errorf("engine emit bridge is unavailable")
	}
	detailValue, err := encodeEventDetail(detail)
	if err != nil {
		return err
	}
	emit.Invoke(name, detailValue)
	return nil
}

func encodeEventDetail(detail any) (js.Value, error) {
	if value, ok := detail.(js.Value); ok {
		return value, nil
	}
	encoded, err := json.Marshal(detail)
	if err != nil {
		return js.Undefined(), fmt.Errorf("encode engine event detail: %w", err)
	}
	parse := js.Global().Get("JSON").Get("parse")
	if parse.Type() != js.TypeFunction {
		return js.Undefined(), fmt.Errorf("JSON.parse is unavailable")
	}
	return parse.Invoke(string(encoded)), nil
}

// Register publishes component's factory during the narrow registration
// window opened for this module by the GoSX loader. Calls made by arbitrary
// scripts or after the module's synchronous registration phase are rejected.
// A module may register multiple components for reuse by later manifests.
func Register(component string, factory Factory) error {
	component = strings.TrimSpace(component)
	if component == "" {
		return fmt.Errorf("Go-WASM engine component is required")
	}
	if factory == nil {
		return fmt.Errorf("Go-WASM engine factory %q is nil", component)
	}
	registrar := js.Global().Get("__gosx_register_go_wasm_engine_factory")
	if registrar.Type() != js.TypeFunction {
		return ErrRegistrationClosed
	}
	token := strings.TrimSpace(os.Getenv(registrationTokenEnv))
	if token == "" {
		return ErrRegistrationClosed
	}

	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || (args[0].Type() != js.TypeObject && args[0].Type() != js.TypeFunction) {
			return rejectedPromise("Go-WASM engine context is missing")
		}
		handle, err := factory(Context{value: args[0]})
		if err != nil {
			return rejectedPromise(err.Error())
		}
		return browserHandle(handle)
	})
	accepted := registrar.Invoke(token, component, callback)
	if accepted.Type() != js.TypeBoolean || !accepted.Bool() {
		callback.Release()
		return ErrRegistrationClosed
	}

	// A standard Go module remains live for the browser document. Retain the
	// callback for that lifetime; per-instance dispose callbacks release themselves.
	factoryMu.Lock()
	factoryFuncs = append(factoryFuncs, callback)
	factoryMu.Unlock()
	return nil
}

func browserHandle(handle Handle) js.Value {
	object := js.Global().Get("Object").New()
	if handle == nil {
		return object
	}
	var once sync.Once
	var dispose js.Func
	dispose = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		once.Do(func() {
			object.Set("dispose", js.Undefined())
			defer dispose.Release()
			handle.Dispose()
		})
		return nil
	})
	object.Set("dispose", dispose)
	return object
}

func rejectedPromise(message string) js.Value {
	err := js.Global().Get("Error").New(message)
	return js.Global().Get("Promise").Call("reject", err)
}

func stringProperty(value js.Value, name string) string {
	property := value.Get(name)
	if property.Type() != js.TypeString {
		return ""
	}
	return property.String()
}

func capabilityValues(value js.Value) []engine.Capability {
	if !value.Truthy() || value.Get("length").Type() != js.TypeNumber {
		return nil
	}
	length := value.Length()
	out := make([]engine.Capability, 0, length)
	for i := 0; i < length; i++ {
		candidate := value.Index(i)
		if candidate.Type() == js.TypeString {
			out = append(out, engine.Capability(candidate.String()))
		}
	}
	return out
}
