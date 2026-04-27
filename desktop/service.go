package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"

	"github.com/odvcencio/gosx/desktop/bridge"
)

const (
	serviceMethodPrefix = "gosx.desktop.service."
	servicesListMethod  = "gosx.desktop.services.list"
)

var (
	contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType   = reflect.TypeOf((*error)(nil)).Elem()
)

// ServiceBinding describes one Go service bound onto the desktop bridge.
type ServiceBinding struct {
	Name    string          `json:"name"`
	Methods []ServiceMethod `json:"methods"`
}

// ServiceMethod describes one method exposed by a bound Go service.
type ServiceMethod struct {
	Name   string `json:"name"`
	GoName string `json:"goName"`
	Method string `json:"method"`
}

// Bind exposes exported methods from service to trusted desktop page code.
//
// Bound methods are callable as:
//
//	window.gosxDesktop.service("prefs").load({ ... })
//
// or through the raw typed bridge method name:
//
//	gosx.desktop.service.prefs.load
//
// Supported Go method shapes are:
//
//	func()
//	func() error
//	func() T
//	func() (T, error)
//	func(context.Context)
//	func(context.Context) error
//	func(Payload) (T, error)
//	func(context.Context, Payload) (T, error)
//
// Payloads are decoded from JSON into the single payload argument. Services are
// app-owned native code; bind only when the hosted content is trusted.
func (a *App) Bind(name string, service any) (ServiceBinding, error) {
	if a == nil || a.bridge == nil {
		return ServiceBinding{}, fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	name = strings.TrimSpace(name)
	if err := validateServiceName(name); err != nil {
		return ServiceBinding{}, err
	}
	methods, err := buildServiceMethods(name, service)
	if err != nil {
		return ServiceBinding{}, err
	}
	if len(methods) == 0 {
		return ServiceBinding{}, fmt.Errorf("%w: service %q has no supported exported methods", ErrInvalidOptions, name)
	}

	a.registerServiceBridgeMethods()
	a.serviceMu.Lock()
	if a.services == nil {
		a.services = make(map[string]ServiceBinding)
	}
	if previous, ok := a.services[name]; ok {
		for _, method := range previous.Methods {
			a.bridge.Unregister(method.Method)
		}
	}
	binding := ServiceBinding{Name: name, Methods: serviceMethodInfos(methods)}
	for _, method := range methods {
		method := method
		a.bridge.Register(method.info.Method, func(ctx *bridge.Context) error {
			return method.invoke(ctx)
		})
	}
	a.services[name] = binding
	a.serviceMu.Unlock()
	return binding, nil
}

// Unbind removes a previously bound desktop service.
func (a *App) Unbind(name string) error {
	if a == nil || a.bridge == nil {
		return fmt.Errorf("%w: nil app", ErrInvalidOptions)
	}
	name = strings.TrimSpace(name)
	if err := validateServiceName(name); err != nil {
		return err
	}
	a.serviceMu.Lock()
	binding, ok := a.services[name]
	if ok {
		delete(a.services, name)
	}
	a.serviceMu.Unlock()
	if !ok {
		return nil
	}
	for _, method := range binding.Methods {
		a.bridge.Unregister(method.Method)
	}
	return nil
}

// ServiceBindings returns a stable snapshot of currently bound services.
func (a *App) ServiceBindings() []ServiceBinding {
	if a == nil {
		return nil
	}
	a.serviceMu.RLock()
	defer a.serviceMu.RUnlock()
	if len(a.services) == 0 {
		return nil
	}
	bindings := make([]ServiceBinding, 0, len(a.services))
	for _, binding := range a.services {
		binding.Methods = append([]ServiceMethod(nil), binding.Methods...)
		bindings = append(bindings, binding)
	}
	sort.Slice(bindings, func(i, j int) bool {
		return bindings[i].Name < bindings[j].Name
	})
	return bindings
}

func (a *App) registerServiceBridgeMethods() {
	if a == nil || a.bridge == nil {
		return
	}
	a.bridge.Register(servicesListMethod, func(ctx *bridge.Context) error {
		return ctx.Respond(a.ServiceBindings())
	})
}

type boundServiceMethod struct {
	service reflect.Value
	method  reflect.Method
	sig     serviceMethodSignature
	info    ServiceMethod
}

type serviceMethodSignature struct {
	hasContext bool
	hasPayload bool
	payload    reflect.Type
	hasResult  bool
	hasError   bool
}

func buildServiceMethods(serviceName string, service any) ([]boundServiceMethod, error) {
	value := reflect.ValueOf(service)
	if !value.IsValid() {
		return nil, fmt.Errorf("%w: service %q is nil", ErrInvalidOptions, serviceName)
	}
	if isNilReflectValue(value) {
		return nil, fmt.Errorf("%w: service %q is nil", ErrInvalidOptions, serviceName)
	}
	typ := value.Type()
	methods := make([]boundServiceMethod, 0, typ.NumMethod())
	seen := map[string]string{}
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		if method.PkgPath != "" {
			continue
		}
		jsName := lowerCamel(method.Name)
		if !validServiceMethodName(jsName) {
			continue
		}
		if prior := seen[jsName]; prior != "" {
			return nil, fmt.Errorf("%w: service %q methods %s and %s both bind as %q",
				ErrInvalidOptions, serviceName, prior, method.Name, jsName)
		}
		sig, ok := analyzeServiceMethod(method)
		if !ok {
			continue
		}
		seen[jsName] = method.Name
		methods = append(methods, boundServiceMethod{
			service: value,
			method:  method,
			sig:     sig,
			info: ServiceMethod{
				Name:   jsName,
				GoName: method.Name,
				Method: serviceMethodName(serviceName, jsName),
			},
		})
	}
	sort.Slice(methods, func(i, j int) bool {
		return methods[i].info.Name < methods[j].info.Name
	})
	return methods, nil
}

func analyzeServiceMethod(method reflect.Method) (serviceMethodSignature, bool) {
	typ := method.Type
	index := 1 // receiver
	sig := serviceMethodSignature{}
	if typ.NumIn() > index && typ.In(index).Implements(contextType) {
		sig.hasContext = true
		index++
	}
	switch typ.NumIn() - index {
	case 0:
	case 1:
		sig.hasPayload = true
		sig.payload = typ.In(index)
	default:
		return serviceMethodSignature{}, false
	}

	switch typ.NumOut() {
	case 0:
	case 1:
		if typ.Out(0).Implements(errorType) {
			sig.hasError = true
		} else {
			sig.hasResult = true
		}
	case 2:
		if !typ.Out(1).Implements(errorType) {
			return serviceMethodSignature{}, false
		}
		sig.hasResult = true
		sig.hasError = true
	default:
		return serviceMethodSignature{}, false
	}
	return sig, true
}

func (m boundServiceMethod) invoke(ctx *bridge.Context) error {
	args := []reflect.Value{m.service}
	if m.sig.hasContext {
		args = append(args, reflect.ValueOf(context.Background()))
	}
	if m.sig.hasPayload {
		payload, err := decodeServicePayload(ctx.RawPayload(), m.sig.payload)
		if err != nil {
			return err
		}
		args = append(args, payload)
	}
	results := m.method.Func.Call(args)
	var response any
	if m.sig.hasResult {
		response = results[0].Interface()
	}
	if m.sig.hasError {
		errValue := results[len(results)-1]
		if !isNilReflectValue(errValue) {
			return errValue.Interface().(error)
		}
	}
	return ctx.Respond(response)
}

func decodeServicePayload(raw json.RawMessage, typ reflect.Type) (reflect.Value, error) {
	if len(raw) == 0 {
		return reflect.Zero(typ), nil
	}
	value := reflect.New(typ)
	if err := json.Unmarshal(raw, value.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("decode service payload: %w", err)
	}
	return value.Elem(), nil
}

func serviceMethodInfos(methods []boundServiceMethod) []ServiceMethod {
	infos := make([]ServiceMethod, len(methods))
	for i, method := range methods {
		infos[i] = method.info
	}
	return infos
}

func serviceMethodName(serviceName, methodName string) string {
	return serviceMethodPrefix + serviceName + "." + methodName
}

func validateServiceName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: service name is empty", ErrInvalidOptions)
	}
	if strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("%w: service name contains NUL", ErrInvalidOptions)
	}
	for _, part := range strings.Split(name, ".") {
		if part == "" {
			return fmt.Errorf("%w: service name %q has an empty segment", ErrInvalidOptions, name)
		}
		for i, r := range part {
			if i == 0 {
				if !unicode.IsLetter(r) && r != '_' {
					return fmt.Errorf("%w: service name %q has invalid segment %q", ErrInvalidOptions, name, part)
				}
				continue
			}
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
				return fmt.Errorf("%w: service name %q has invalid segment %q", ErrInvalidOptions, name, part)
			}
		}
	}
	return nil
}

func validServiceMethodName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if i == 0 {
			if !unicode.IsLetter(r) && r != '_' {
				return false
			}
			continue
		}
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func lowerCamel(name string) string {
	if name == "" {
		return ""
	}
	runes := []rune(name)
	end := 1
	for end < len(runes) && unicode.IsUpper(runes[end-1]) && unicode.IsUpper(runes[end]) {
		if end+1 < len(runes) && unicode.IsLower(runes[end+1]) {
			break
		}
		end++
	}
	for i := 0; i < end; i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

func isNilReflectValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
