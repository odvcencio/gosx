//go:build js && wasm

package browser

import (
	"errors"
	"fmt"
	"syscall/js"
)

func CurrentLocation() Location {
	v := js.Global().Get("location")
	if !v.Truthy() {
		return Location{}
	}
	return Location{Href: v.Get("href").String(), Origin: v.Get("origin").String(),
		Protocol: v.Get("protocol").String(), Host: v.Get("host").String(),
		Path: v.Get("pathname").String(), Search: v.Get("search").String(), Hash: v.Get("hash").String()}
}

func Navigate(url string)       { js.Global().Get("location").Call("assign", url) }
func ReplaceHistory(url string) { js.Global().Get("history").Call("replaceState", js.Null(), "", url) }
func Reload()                   { js.Global().Get("location").Call("reload") }
func Viewport() Size {
	g := js.Global()
	return Size{Width: platformNumber(g.Get("innerWidth")), Height: platformNumber(g.Get("innerHeight"))}
}
func PixelRatio() float64     { return platformNumber(js.Global().Get("devicePixelRatio")) }
func DispatchResize()         { js.Global().Call("dispatchEvent", js.Global().Get("Event").New("resize")) }
func LogError(message string) { js.Global().Get("console").Call("error", message) }

func platformNumber(v js.Value) float64 {
	if v.Type() == js.TypeNumber {
		return v.Float()
	}
	return 0
}

func (c JSONCapture) Enabled() (enabled bool) {
	defer func() {
		if recover() != nil {
			enabled = false
		}
	}()
	if c.GlobalName == "" || c.EnabledFlag == "" {
		return false
	}
	observer := js.Global().Get(c.GlobalName)
	return observer.Truthy() && observer.Get(c.EnabledFlag).Type() == js.TypeBoolean && observer.Get(c.EnabledFlag).Bool()
}

func (c JSONCapture) Publish(data []byte, label string) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("browser diagnostic capture: %v", value)
		}
	}()
	if !c.Enabled() {
		return nil
	}
	if c.PayloadField == "" || c.LabelField == "" || c.SequenceField == "" {
		return errors.New("browser diagnostic capture: missing field name")
	}
	observer := js.Global().Get(c.GlobalName)
	payload := js.Global().Get("JSON").Call("parse", string(data))
	observer.Set(c.PayloadField, payload)
	observer.Set(c.LabelField, label)
	observer.Set(c.SequenceField, platformNumber(observer.Get(c.SequenceField))+1)
	return nil
}
