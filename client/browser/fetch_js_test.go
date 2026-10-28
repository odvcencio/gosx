//go:build js && wasm

package browser

import (
	"context"
	"errors"
	"fmt"
	"m31labs.dev/gosx/client/jsutil"
	"m31labs.dev/gosx/internal/jsreentry"
	"runtime/debug"
	"strings"
	"sync"
	"syscall/js"
	"testing"
	"time"
)

func fetchTestPromise() (promise, resolve, reject js.Value) {
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any { resolve, reject = args[0], args[1]; return nil })
	promise = js.Global().Get("Promise").New(executor)
	executor.Release()
	return
}
func TestFetchPreservesBytesHeadersAndHTTPFailures(t *testing.T) {
	original := js.Global().Get("fetch")
	defer js.Global().Set("fetch", original)
	for _, scenario := range []string{"ok", "http", "transport", "body"} {
		t.Run(scenario, func(t *testing.T) {
			body := js.FuncOf(func(_ js.Value, _ []js.Value) any {
				if scenario == "body" {
					return js.Global().Get("Promise").Call("reject", js.Global().Get("Error").New("body unavailable"))
				}
				array := jsutil.NewUint8ArrayFromBytes([]byte{0, 128, 255})
				return js.Global().Get("Promise").Call("resolve", array.Get("buffer"))
			})
			defer body.Release()
			fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
				options := args[1]
				if args[0].String() != "/sample" || options.Get("method").String() != "POST" || options.Get("headers").Get("Content-Type").String() != "application/json" || options.Get("headers").Get("X-Queue").String() != "1" {
					t.Error("request headers changed")
				}
				bytes := make([]byte, 2)
				js.CopyBytesToGo(bytes, options.Get("body"))
				if string(bytes) != "{}" {
					t.Error("request body changed")
				}
				if scenario == "transport" {
					return js.Global().Get("Promise").Call("reject", "offline")
				}
				status, statusText := 200, "OK"
				if scenario == "http" {
					status, statusText = 503, "Unavailable"
				}
				return js.Global().Get("Promise").Call("resolve", map[string]any{"status": status, "statusText": statusText, "arrayBuffer": body})
			})
			defer fetch.Release()
			js.Global().Set("fetch", fetch)
			response, err := Fetch(context.Background(), FetchRequest{URL: "/sample", Method: "POST", ContentType: "application/json", Headers: map[string]string{"X-Queue": "1"}, Body: []byte("{}")})
			if scenario == "transport" || scenario == "body" {
				if err == nil {
					t.Fatal("promise failure lost")
				}
				if scenario == "body" && response.Status != 200 {
					t.Fatal("body failure lost received HTTP metadata")
				}
				return
			}
			if err != nil || string(response.Body) != string([]byte{0, 128, 255}) {
				t.Fatal("response bytes changed", err)
			}
			if scenario == "http" && (response.Status != 503 || response.StatusText != "Unavailable") {
				t.Fatal("HTTP error became transport failure")
			}
		})
	}
}

func TestFetchCancellationAbortsResponseAndBodyWaits(t *testing.T) {
	original := js.Global().Get("fetch")
	defer js.Global().Set("fetch", original)
	for _, bodyPending := range []bool{false, true} {
		promise, _, reject := fetchTestPromise()
		entered := make(chan struct{}, 1)
		body := js.FuncOf(func(_ js.Value, _ []js.Value) any { entered <- struct{}{}; return promise })
		defer body.Release()
		var signal js.Value
		fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
			signal = args[1].Get("signal")
			if !bodyPending {
				entered <- struct{}{}
				return promise
			}
			return js.Global().Get("Promise").Call("resolve", map[string]any{"status": 200, "statusText": "OK", "arrayBuffer": body})
		})
		defer fetch.Release()
		js.Global().Set("fetch", fetch)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { _, err := Fetch(ctx, FetchRequest{URL: "/pending"}); done <- err }()
		<-entered
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation identity lost", err)
			}
		case <-time.After(time.Second):
			t.Fatal("fetch retained never-settling promise")
		}
		if !signal.Get("aborted").Bool() {
			t.Fatal("network operation did not receive abort")
		}
		reject.Invoke(js.Global().Get("Error").New("late network rejection"))
		_, _ = jsutil.AwaitPromise(js.Global().Get("Promise").Call("resolve", nil))
	}
}

func TestFetchInvalidContextAndUnavailableTransport(t *testing.T) {
	original := js.Global().Get("fetch")
	defer js.Global().Set("fetch", original)
	js.Global().Set("fetch", js.Undefined())
	if _, err := Fetch(context.Background(), FetchRequest{}); !errors.Is(err, ErrFetchUnavailable) {
		t.Fatal(err)
	}
	if _, err := Fetch(nil, FetchRequest{}); err == nil || !strings.Contains(err.Error(), "context required") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Fetch(ctx, FetchRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// nativeFetch installs a fetch that is JavaScript from end to end, so that the
// only way into Go during a request is a callback that Fetch itself attached.
// A fetch written with js.FuncOf would be a call from JavaScript into Go.
//
// The URL picks the behavior. Everything settles on a later timer turn.
//
//	/ok           200 and the bytes 7, 1, 2
//	/http         503 and the bytes 7, 1, 2
//	/offline      the transport rejects with a string
//	/number       the transport rejects with a number
//	/symbol       the transport rejects with a Symbol
//	/bigint       the transport rejects with a BigInt
//	/body-fails   200, then the body rejects with an Error
//	/hang         the transport never settles unless the signal aborts
//	/body-hangs   200, then the body never settles
func nativeFetch(t testing.TB) (stats js.Value) {
	t.Helper()
	fixture := js.Global().Get("Function").New(`
		const stats = { calls: 0, aborted: 0 };
		function later(fn) { setTimeout(fn, 0); }
		const bytes = function () { return new Uint8Array([7, 1, 2]).buffer; };
		stats.fetch = function (url, options) {
			stats.calls++;
			return new Promise(function (resolve, reject) {
				const signal = options && options.signal;
				if (signal) {
					signal.addEventListener("abort", function () {
						stats.aborted++;
						reject(Object.assign(new Error("The operation was aborted"), { name: "AbortError" }));
					});
				}
				if (url === "/hang") return;
				if (url === "/offline") return later(function () { reject("offline"); });
				if (url === "/number") return later(function () { reject(42); });
				if (url === "/symbol") return later(function () { reject(Symbol("why")); });
				if (url === "/bigint") return later(function () { reject(10n); });
				later(function () {
					resolve({
						status: url === "/http" ? 503 : 200,
						statusText: url === "/http" ? "Unavailable" : "OK",
						arrayBuffer: function () {
							if (url === "/body-hangs") return new Promise(function () {});
							if (url === "/body-fails") return new Promise(function (_, fail) { later(function () { fail(new Error("body unavailable")); }); });
							return new Promise(function (done) { later(function () { done(bytes()); }); });
						},
					});
				});
			});
		};
		return stats;
	`).Invoke()
	original := js.Global().Get("fetch")
	js.Global().Set("fetch", fixture.Get("fetch"))
	t.Cleanup(func() { js.Global().Set("fetch", original) })
	return fixture
}

func TestFetchNeverRunsGoInsideAJavaScriptCall(t *testing.T) {
	stats := nativeFetch(t)
	probe := jsreentry.Install(t)
	live, stopLive := context.WithCancel(context.Background())
	defer stopLive()

	for _, c := range []struct{ url, name string }{{"/ok", "ok"}, {"/http", "HTTP failure"}} {
		response, err := Fetch(live, FetchRequest{URL: c.url, Method: "POST", Body: []byte("{}")})
		if err != nil || string(response.Body) != string([]byte{7, 1, 2}) {
			t.Fatalf("%s: bytes changed: %v %v", c.name, response.Body, err)
		}
		if c.url == "/http" && (response.Status != 503 || response.StatusText != "Unavailable") {
			t.Fatalf("%s: status changed: %+v", c.name, response)
		}
	}
	// With no cancellation channel Fetch takes a different path; cover it too.
	if response, err := Fetch(context.Background(), FetchRequest{URL: "/ok"}); err != nil || len(response.Body) != 3 {
		t.Fatalf("background context: %v %v", response.Body, err)
	}
	for url, want := range map[string]string{"/offline": "offline", "/body-fails": "body unavailable"} {
		if _, err := Fetch(live, FetchRequest{URL: url}); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: want an error that says %q, got %v", url, want, err)
		}
	}
	for _, url := range []string{"/hang", "/body-hangs"} {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(2*time.Millisecond, cancel)
		before := stats.Get("aborted").Int()
		if _, err := Fetch(ctx, FetchRequest{URL: url}); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: cancellation identity lost: %v", url, err)
		}
		if stats.Get("aborted").Int() != before+1 {
			t.Fatalf("%s: the request did not receive the abort", url)
		}
	}
	expiring, stop := context.WithTimeout(context.Background(), 3*time.Millisecond)
	defer stop()
	if _, err := Fetch(expiring, FetchRequest{URL: "/hang"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline identity lost: %v", err)
	}

	probe.AssertNone(t, "Fetch")
	if probe.Entries() == 0 {
		t.Fatal("the requests never reached their handlers")
	}
}

func TestFetchReportsATransportFailureThatIsNotAnError(t *testing.T) {
	nativeFetch(t)
	for url, want := range map[string]string{"/offline": "offline", "/number": "42", "/symbol": "Symbol(why)", "/bigint": "10"} {
		for _, ctx := range []context.Context{context.Background(), withCancel(t)} {
			_, err := Fetch(ctx, FetchRequest{URL: url})
			if err == nil || !strings.Contains(err.Error(), "browser: fetch transport: ") || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: want a transport error that says %q, got %v", url, want, err)
			}
		}
	}
}

func withCancel(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

// Many requests at once, with cancellation and a busy collector. Go's js/wasm
// runtime keeps the calls from JavaScript into Go that are in progress on one
// stack, and two goroutines that nest at the same time can corrupt it. The test
// requires that no request nests at all.
func TestFetchRequestsNeverNestUnderConcurrencyAndGCPressure(t *testing.T) {
	stats := nativeFetch(t)
	previous := debug.SetGCPercent(5)
	defer debug.SetGCPercent(previous)
	probe := jsreentry.Install(t)
	const goroutines, rounds = 24, 16
	var wg sync.WaitGroup
	failures := make(chan string, goroutines*rounds)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			var garbage [][]byte
			for i := 0; i < rounds; i++ {
				garbage = append(garbage[:0], make([]byte, 48<<10), make([]byte, 16<<10))
				name := fmt.Sprintf("%d/%d", g, i)
				ctx, cancel := context.WithCancel(context.Background())
				switch i % 4 {
				case 0:
					if response, err := Fetch(ctx, FetchRequest{URL: "/ok"}); err != nil || len(response.Body) != 3 {
						failures <- fmt.Sprintf("%s: wrong response %v %v", name, response.Body, err)
					}
				case 1:
					time.AfterFunc(time.Duration(i%3)*time.Millisecond, cancel)
					if _, err := Fetch(ctx, FetchRequest{URL: "/hang"}); !errors.Is(err, context.Canceled) {
						failures <- fmt.Sprintf("%s: wrong cancellation %v", name, err)
					}
				case 2:
					if _, err := Fetch(ctx, FetchRequest{URL: "/offline"}); err == nil || !strings.Contains(err.Error(), "offline") {
						failures <- fmt.Sprintf("%s: wrong transport failure %v", name, err)
					}
				default:
					timed, stop := context.WithTimeout(ctx, time.Duration(1+i%3)*time.Millisecond)
					if _, err := Fetch(timed, FetchRequest{URL: "/body-hangs"}); !errors.Is(err, context.DeadlineExceeded) {
						failures <- fmt.Sprintf("%s: wrong deadline %v", name, err)
					}
					stop()
				}
				cancel()
			}
			_ = garbage
		}(g)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	probe.AssertNone(t, "concurrent Fetch calls")
	if probe.MaxDepth() != 1 {
		t.Fatalf("calls into Go nested to depth %d", probe.MaxDepth())
	}
	if stats.Get("calls").Int() != goroutines*rounds {
		t.Fatalf("expected %d requests, saw %d", goroutines*rounds, stats.Get("calls").Int())
	}
}
