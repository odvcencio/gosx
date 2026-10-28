//go:build js && wasm

package jsutil

import (
	"fmt"
	"strings"
	"sync"
	"syscall/js"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/jsreentry"
)

// native evaluates a JavaScript expression. The tests build their JavaScript
// fixtures with it, because a fixture written as a Go function would itself be
// a call from JavaScript into Go.
func native(expression string) js.Value {
	return js.Global().Get("Function").New("return (" + expression + ")").Invoke()
}

// rejections lists values a promise can reject with. Only the first is an Error
// with a message; the rest must not make Describe or AwaitPromise panic.
var rejections = []struct {
	name, expression string
	want             string // substring of the description
}{
	{"error", `new Error("boom")`, "boom"},
	{"error without a message", `new TypeError("")`, "TypeError"},
	{"string", `"plain text"`, "plain text"},
	{"number", `42`, "42"},
	{"zero", `0`, "0"},
	{"boolean", `false`, "false"},
	{"null", `null`, "<null>"},
	{"undefined", `undefined`, "<undefined>"},
	{"symbol", `Symbol("why")`, "Symbol(why)"},
	{"bigint", `10n`, "10"},
	{"plain object", `({ code: 7 })`, "[object Object]"},
	{"object without a prototype", `Object.create(null)`, "<unprintable value>"},
	{"object whose message getter throws", `({ get message() { throw new Error("getter") } })`, "[object Object]"},
	{"object whose conversion throws", `({ message: 1, toString() { throw new Error("toString") } })`, "<unprintable value>"},
	{"revoked proxy", `(function () { const revocable = Proxy.revocable({}, {}); revocable.revoke(); return revocable.proxy; })()`, "<unprintable value>"},
	{"proxy whose trap throws", `new Proxy({}, { get() { throw new Error("trap"); } })`, "<unprintable value>"},
	{"function", `(function namedFailure() {})`, "namedFailure"},
}

func TestAwaitPromiseNeverRunsGoInsideAJavaScriptCall(t *testing.T) {
	probe := jsreentry.Install(t)
	// Each promise is built just before its wait. A rejected promise that waits
	// for its turn would be reported as unhandled and stop Node.
	cases := []struct {
		name       string
		expression string
		want       string
		failure    string
	}{
		{"resolved", `Promise.resolve("done")`, "done", ""},
		{"value that is not a thenable", `"plain value"`, "plain value", ""},
		{"rejected", `Promise.reject(new Error("refused"))`, "", "refused"},
		{"settles later", `new Promise(function (resolve) { setTimeout(resolve, 2, "late"); })`, "late", ""},
		{"thenable that resolves at once", `({ then(resolve) { resolve("thenable"); } })`, "thenable", ""},
		{"thenable that rejects at once", `({ then(resolve, reject) { reject(new Error("thenable refused")); } })`, "", "thenable refused"},
		{"thenable that calls back twice", `({ then(resolve, reject) { resolve("first"); resolve("second"); reject(new Error("third")); } })`, "first", ""},
		{"thenable whose then throws", `({ then() { throw new Error("then threw"); } })`, "", "then threw"},
	}
	for _, c := range cases {
		value, err := AwaitPromise(native(c.expression))
		switch {
		case c.failure != "" && (err == nil || !strings.Contains(err.Error(), c.failure)):
			t.Errorf("%s: want error %q, got %v", c.name, c.failure, err)
		case c.failure == "" && (err != nil || value.String() != c.want):
			t.Errorf("%s: want %q, got %v %v", c.name, c.want, value, err)
		}
	}
	probe.AssertNone(t, "AwaitPromise")
	if probe.Entries() < len(cases) {
		t.Fatalf("the handlers did not run as separate entries: %d entries for %d waits", probe.Entries(), len(cases))
	}
}

func TestAwaitPromiseReportsAnyRejectionValueWithoutPanicking(t *testing.T) {
	for _, rejection := range rejections {
		t.Run(rejection.name, func(t *testing.T) {
			defer func() {
				if failure := recover(); failure != nil {
					t.Fatalf("AwaitPromise panicked on a rejection with %s: %v", rejection.name, failure)
				}
			}()
			promise := js.Global().Get("Promise").Call("reject", native(rejection.expression))
			_, err := AwaitPromise(promise)
			if err == nil || !strings.HasPrefix(err.Error(), "promise rejected: ") || !strings.Contains(err.Error(), rejection.want) {
				t.Fatalf("want an error that describes %q, got %v", rejection.want, err)
			}
		})
	}
}

func TestDescribeNeverPanics(t *testing.T) {
	for _, rejection := range rejections {
		func() {
			defer func() {
				if failure := recover(); failure != nil {
					t.Errorf("Describe panicked on %s: %v", rejection.name, failure)
				}
			}()
			if text := Describe(native(rejection.expression)); !strings.Contains(text, rejection.want) {
				t.Errorf("Describe(%s) = %q, want it to contain %q", rejection.name, text, rejection.want)
			}
		}()
	}
	if Describe(js.Value{}) != "<undefined>" {
		t.Error("the zero Value must describe as undefined")
	}
}

func TestNewPromiseWithResolversStaysPendingUntilSettledOnce(t *testing.T) {
	probe := jsreentry.Install(t)
	promise, resolve, reject := NewPromiseWithResolvers()
	if resolve.Type() != js.TypeFunction || reject.Type() != js.TypeFunction {
		t.Fatalf("resolvers are not functions: %v %v", resolve.Type(), reject.Type())
	}
	// A pending promise loses a race against a promise that is already resolved.
	raced := native(`function (pending) { return Promise.race([pending, Promise.resolve("still pending")]); }`).Invoke(promise)
	if value, err := AwaitPromise(raced); err != nil || value.String() != "still pending" {
		t.Fatalf("the promise settled before its resolvers ran: %v %v", value, err)
	}
	go func() {
		time.Sleep(time.Millisecond)
		resolve.Invoke("first")
		resolve.Invoke("second")
		reject.Invoke(js.Global().Get("Error").New("ignored"))
	}()
	value, err := AwaitPromise(promise)
	if err != nil || value.String() != "first" {
		t.Fatalf("want the first settlement, got %v %v", value, err)
	}

	rejected, _, rejectLater := NewPromiseWithResolvers()
	rejectLater.Invoke(js.Global().Get("Error").New("declined"))
	if _, err := AwaitPromise(rejected); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("rejection lost: %v", err)
	}
	probe.AssertNone(t, "NewPromiseWithResolvers")
}

func TestAwaitPromiseWaitersNeverNestUnderConcurrency(t *testing.T) {
	probe := jsreentry.Install(t)
	// A thenable that calls its handlers inside then(), as promise-like objects
	// from some libraries do. Waiting on it must not run a Go handler there.
	eager := native(`function (value) { return { then(resolve) { resolve(value); return Promise.resolve(); } }; }`)
	const goroutines, rounds = 24, 30
	var wg sync.WaitGroup
	failures := make(chan string, goroutines*rounds)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				want := fmt.Sprintf("%d/%d", g, i)
				promise, resolve, reject := NewPromiseWithResolvers()
				switch i % 4 {
				case 3:
					js.Global().Call("setTimeout", reject, i%3, js.Global().Get("Error").New(want))
					if _, err := AwaitPromise(promise); err == nil || !strings.Contains(err.Error(), want) {
						failures <- fmt.Sprintf("%s: wrong rejection %v", want, err)
					}
				case 2:
					if value, err := AwaitPromise(eager.Invoke(want)); err != nil || value.String() != want {
						failures <- fmt.Sprintf("%s: wrong thenable value %v %v", want, value, err)
					}
				default:
					js.Global().Call("setTimeout", resolve, i%3, want)
					if value, err := AwaitPromise(promise); err != nil || value.String() != want {
						failures <- fmt.Sprintf("%s: wrong value %v %v", want, value, err)
					}
				}
			}
		}(g)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	probe.AssertNone(t, "concurrent AwaitPromise waits")
	if probe.MaxDepth() != 1 {
		t.Fatalf("calls into Go nested to depth %d", probe.MaxDepth())
	}
}
