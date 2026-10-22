//go:build js && wasm

package wasm

import (
	"syscall/js"
	"time"
)

// declareCalls tells the page half that this engine serves calls on channel,
// so its watchdog can fail a call the worker never answers. The main-thread
// context has no such method, and nothing is needed there.
func (c Context) declareCalls(channel string, timeout time.Duration, recent int, zeroResult string) {
	defer func() { _ = recover() }()
	declare := c.value.Get("declareCalls")
	if declare.Type() != js.TypeFunction {
		return
	}
	c.value.Call("declareCalls", channel, timeout.Milliseconds(), recent, zeroResult)
}
