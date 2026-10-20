//go:build js && wasm

package jsutil

import (
	"context"
	"errors"
	"strings"
	"syscall/js"
	"testing"
	"time"
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
