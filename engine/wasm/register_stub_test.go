//go:build !js || !wasm

package wasm

import (
	"context"
	"errors"
	"testing"
)

func TestRegisterNativeStub(t *testing.T) {
	if err := Register("", func(Context) (Handle, error) { return nil, nil }); err == nil {
		t.Fatal("expected an empty component to fail")
	}
	if err := Register("Fixture", nil); err == nil {
		t.Fatal("expected a nil factory to fail")
	}
	if err := Register("Fixture", func(Context) (Handle, error) { return nil, nil }); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestSharedSignalNativeStubs(t *testing.T) {
	if err := (Context{}).SetSignal("$intent", nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SetSignal error = %v", err)
	}
	if _, err := SubscribeSignal[int](Context{}, "$input", func(int) {}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SubscribeSignal error = %v", err)
	}
}

func TestNativeRuntimeAPIsAreExplicitlyUnsupported(t *testing.T) {
	ctx := Context{}
	if _, err := ReadSignal[string](ctx, "$value"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("read=%v", err)
	}
	if err := ctx.SetSignals(map[string]any{"$value": 1}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("batch=%v", err)
	}
	if err := ctx.Navigate(context.Background(), "/", NavigationOptions{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("navigation=%v", err)
	}
	target := ctx.Scene3D(nil)
	for _, err := range []error{target.Ready(context.Background()), target.Dispatch(context.Background(), nil), target.SetCamera(context.Background(), nil), target.SetAnimationClock(context.Background(), 0, true)} {
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("scene=%v", err)
		}
	}
	if ctx.IsCurrent() {
		t.Fatal("native engine reported live")
	}
}
