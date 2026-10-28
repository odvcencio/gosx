//go:build js && wasm

package jsutil

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"syscall/js"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/jsreentry"
)

func pendingPromise() (promise, resolve, reject js.Value) {
	executor := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolve, reject = args[0], args[1]
		return nil
	})
	promise = js.Global().Get("Promise").New(executor)
	executor.Release()
	return
}

func TestAwaitPromiseContextResolvedRejectedAndInvalid(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	value, err := AwaitPromiseContext(ctx, js.Global().Get("Promise").Call("resolve", map[string]any{"answer": 42}))
	if err != nil || value.Get("answer").Int() != 42 {
		t.Fatalf("resolved value lost: %v %v", value, err)
	}
	_, err = AwaitPromiseContext(ctx, js.Global().Get("Promise").Call("reject", js.Global().Get("Error").New("SDK unavailable")))
	if err == nil || !strings.Contains(err.Error(), "SDK unavailable") {
		t.Fatalf("rejection lost: %v", err)
	}
	if _, err := AwaitPromiseContext(ctx, js.Undefined()); err == nil {
		t.Fatal("undefined promise accepted")
	}
	if _, err := AwaitPromiseContext(nil, js.Undefined()); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestAwaitPromiseContextCancelsNeverSettlingPromise(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	promise, _, _ := pendingPromise()
	done := make(chan error, 1)
	go func() {
		_, err := AwaitPromiseContext(ctx, promise)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation identity lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation remained blocked on a third-party promise")
	}
}

func TestAwaitPromiseContextDeadlineAndLateSettlement(t *testing.T) {
	for _, rejectLate := range []bool{false, true} {
		promise, resolve, reject := pendingPromise()
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		_, err := AwaitPromiseContext(ctx, promise)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline identity lost: %v", err)
		}
		// These callbacks must reach only native Promise.race handlers, never a
		// released Go js.Func. In Node an unobserved late rejection also fails the
		// test process; the race observes it after our Go wait has ended.
		if rejectLate {
			reject.Invoke(js.Global().Get("Error").New("late SDK rejection"))
		} else {
			resolve.Invoke("late SDK result")
		}
		if _, err := AwaitPromise(js.Global().Get("Promise").Call("resolve", nil)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAwaitPromiseContextNeverRunsGoInsideAJavaScriptCall(t *testing.T) {
	probe := jsreentry.Install(t)
	live, stopLive := context.WithCancel(context.Background())
	defer stopLive()

	if value, err := AwaitPromiseContext(live, native(`Promise.resolve({ answer: 42 })`)); err != nil || value.Get("answer").Int() != 42 {
		t.Fatalf("resolved value lost: %v %v", value, err)
	}
	if _, err := AwaitPromiseContext(live, native(`Promise.reject(new Error("SDK unavailable"))`)); err == nil || !strings.Contains(err.Error(), "SDK unavailable") {
		t.Fatalf("rejection lost: %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	time.AfterFunc(time.Millisecond, cancel)
	if _, err := AwaitPromiseContext(cancelled, native(`new Promise(function () {})`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity lost: %v", err)
	}
	expiring, stopExpiring := context.WithTimeout(context.Background(), 2*time.Millisecond)
	defer stopExpiring()
	if _, err := AwaitPromiseContext(expiring, native(`new Promise(function () {})`)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline identity lost: %v", err)
	}
	done, finish := context.WithCancel(context.Background())
	finish()
	if _, err := AwaitPromiseContext(done, native(`new Promise(function () {})`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("a context that was already cancelled waited: %v", err)
	}

	probe.AssertNone(t, "AwaitPromiseContext")
	if probe.Entries() == 0 {
		t.Fatal("the waits never reached their handlers")
	}
}

func TestPromiseWithResolversCancellationLeavesNoGoCallbackBehind(t *testing.T) {
	probe := jsreentry.Install(t)
	promise, resolve, reject := NewPromiseWithResolvers()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(time.Millisecond, cancel)
	if _, err := AwaitPromiseContext(ctx, promise); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity lost: %v", err)
	}
	// The cancelled wait released its Go handlers. A late settlement must reach
	// only the native race, never a released Go function.
	resolve.Invoke("late result")
	reject.Invoke(js.Global().Get("Error").New("late rejection"))
	if _, err := AwaitPromise(js.Global().Get("Promise").Call("resolve", nil)); err != nil {
		t.Fatal(err)
	}
	probe.AssertNone(t, "late settlement after cancellation")
}

// A stress run with many goroutines that wait with cancellation while the
// collector is busy. Before the waits stopped nesting, every one of these calls
// ran a Go executor inside Promise construction. A nested entry can stop Go's
// js/wasm runtime when another entry from JavaScript is paused in its
// allocation, so the test requires that no wait nests at all, and it counts the
// depth of the nesting to show that too.
func TestAwaitPromiseContextWaitsNeverNestUnderConcurrencyAndGCPressure(t *testing.T) {
	previous := debug.SetGCPercent(5)
	defer debug.SetGCPercent(previous)
	probe := jsreentry.Install(t)
	const goroutines, rounds = 32, 24
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
				switch i % 4 {
				case 0: // resolves before the context ends
					ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
					promise, resolve, _ := NewPromiseWithResolvers()
					js.Global().Call("setTimeout", resolve, i%3, name)
					if value, err := AwaitPromiseContext(ctx, promise); err != nil || value.String() != name {
						failures <- fmt.Sprintf("%s: wrong value %v %v", name, value, err)
					}
					cancel()
				case 1: // cancelled while pending
					ctx, cancel := context.WithCancel(context.Background())
					promise, _, _ := NewPromiseWithResolvers()
					time.AfterFunc(time.Duration(i%3)*time.Millisecond, cancel)
					if _, err := AwaitPromiseContext(ctx, promise); !errors.Is(err, context.Canceled) {
						failures <- fmt.Sprintf("%s: wrong cancellation %v", name, err)
					}
				case 2: // rejects
					ctx, cancel := context.WithCancel(context.Background())
					promise, _, reject := NewPromiseWithResolvers()
					js.Global().Call("setTimeout", reject, i%3, js.Global().Get("Error").New(name))
					if _, err := AwaitPromiseContext(ctx, promise); err == nil || !strings.Contains(err.Error(), name) {
						failures <- fmt.Sprintf("%s: wrong rejection %v", name, err)
					}
					cancel()
				default: // runs out of time
					ctx, cancel := context.WithTimeout(context.Background(), time.Duration(1+i%3)*time.Millisecond)
					promise, _, _ := NewPromiseWithResolvers()
					if _, err := AwaitPromiseContext(ctx, promise); !errors.Is(err, context.DeadlineExceeded) {
						failures <- fmt.Sprintf("%s: wrong deadline %v", name, err)
					}
					cancel()
				}
			}
			_ = garbage
		}(g)
	}
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	probe.AssertNone(t, "concurrent AwaitPromiseContext waits")
	if probe.MaxDepth() != 1 {
		t.Fatalf("calls into Go nested to depth %d", probe.MaxDepth())
	}
}
