//go:build js && wasm

package scene

import (
	"strings"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/jsutil"
)

// The frame rejection handlers are Go callbacks that JavaScript runs from a
// microtask. They describe whatever the page rejected with. A panic in a
// callback stops the whole program, so a rejection that is not an Error (a
// number, a Symbol, a BigInt, a revoked Proxy) must be logged, not fatal.
func TestFrameRejectionHandlersLogAnyRejectionValue(t *testing.T) {
	fixture := js.Global().Get("Function").New(`
		const saved = console.error;
		const logged = [];
		console.error = function () { logged.push(Array.prototype.map.call(arguments, String).join(" ")); };
		return { logged, restore() { console.error = saved; } };
	`).Invoke()
	t.Cleanup(func() { fixture.Call("restore") })

	handlers := []struct {
		name    string
		handler func() js.Value
		prefix  string
	}{
		{"clip", clipFrameRejectionHandler, "[gosx] Scene3D clip frame rejected:"},
		{"motion", motionFrameRejectionHandler, "[gosx] Scene3D motion frame rejected:"},
		{"pose", poseFrameRejectionHandler, "[gosx] Scene3D pose frame rejected:"},
	}
	rejections := []struct{ expression, want string }{
		{`new Error("renderer lost")`, "renderer lost"},
		{`"plain text"`, "plain text"},
		{`42`, "42"},
		{`10n`, "10"},
		{`Symbol("why")`, "Symbol(why)"},
		{`undefined`, "<undefined>"},
		{`({ get message() { throw new Error("getter"); } })`, "[object Object]"},
		{`(function () { const r = Proxy.revocable({}, {}); r.revoke(); return r.proxy; })()`, "<unprintable value>"},
	}
	next := 0
	for _, h := range handlers {
		for _, r := range rejections {
			rejected := js.Global().Get("Function").New("return Promise.reject(" + r.expression + ")").Invoke()
			// catch returns a promise that settles after the handler ran.
			if _, err := jsutil.AwaitPromise(rejected.Call("catch", h.handler())); err != nil {
				t.Fatalf("%s handler, %s: %v", h.name, r.expression, err)
			}
			logged := fixture.Get("logged")
			if logged.Length() != next+1 {
				t.Fatalf("%s handler, %s: expected one more log line, have %d", h.name, r.expression, logged.Length())
			}
			line := logged.Index(next).String()
			next++
			if !strings.HasPrefix(line, h.prefix) || !strings.Contains(line, r.want) {
				t.Errorf("%s handler, %s: logged %q, want prefix %q and %q", h.name, r.expression, line, h.prefix, r.want)
			}
		}
	}
}
