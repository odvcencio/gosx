package perf

import (
	"context"
	"errors"
	"testing"
	"time"
)

type driverContextValueKey string

func TestWithOperationContextEffectiveDeadlineAndCancellation(t *testing.T) {
	tests := []struct {
		name          string
		driverTimeout time.Duration
		parentTimeout time.Duration
		opTimeout     time.Duration
		cancelParent  bool
		cancelDriver  bool
		wantErr       error
		maxWait       time.Duration
	}{
		{
			name:          "timeout zero inherits parent deadline",
			parentTimeout: 25 * time.Millisecond,
			wantErr:       context.DeadlineExceeded,
			maxWait:       250 * time.Millisecond,
		},
		{
			name:          "parent deadline beats longer operation timeout",
			parentTimeout: 25 * time.Millisecond,
			opTimeout:     time.Second,
			wantErr:       context.DeadlineExceeded,
			maxWait:       250 * time.Millisecond,
		},
		{
			name:          "shorter operation timeout beats parent deadline",
			parentTimeout: time.Second,
			opTimeout:     25 * time.Millisecond,
			wantErr:       context.DeadlineExceeded,
			maxWait:       250 * time.Millisecond,
		},
		{
			name:          "driver deadline beats parent and operation timeout",
			driverTimeout: 25 * time.Millisecond,
			parentTimeout: time.Second,
			opTimeout:     time.Second,
			wantErr:       context.DeadlineExceeded,
			maxWait:       250 * time.Millisecond,
		},
		{
			name:         "manual parent cancellation wins promptly",
			opTimeout:    time.Second,
			cancelParent: true,
			wantErr:      context.Canceled,
			maxWait:      250 * time.Millisecond,
		},
		{
			name:          "driver cancellation always wins",
			parentTimeout: time.Second,
			opTimeout:     time.Second,
			cancelDriver:  true,
			wantErr:       context.Canceled,
			maxWait:       250 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			driverCtx := context.WithValue(context.Background(), driverContextValueKey("driver"), "preserved")
			driverCancel := func() {}
			if tt.driverTimeout > 0 {
				driverCtx, driverCancel = context.WithTimeout(driverCtx, tt.driverTimeout)
			} else {
				driverCtx, driverCancel = context.WithCancel(driverCtx)
			}
			defer driverCancel()

			parentCtx := context.WithValue(context.Background(), driverContextValueKey("parent"), "external")
			parentCancel := func() {}
			if tt.parentTimeout > 0 {
				parentCtx, parentCancel = context.WithTimeout(parentCtx, tt.parentTimeout)
			} else {
				parentCtx, parentCancel = context.WithCancel(parentCtx)
			}
			defer parentCancel()

			d := &Driver{ctx: driverCtx}
			opD, cancel := d.WithOperationContext(parentCtx, tt.opTimeout)
			defer cancel()

			if got := opD.Context().Value(driverContextValueKey("driver")); got != "preserved" {
				t.Fatalf("operation context lost driver/chromedp values: got %v", got)
			}
			if got := opD.Context().Value(driverContextValueKey("parent")); got != nil {
				t.Fatalf("operation context should not be derived from external parent values, got %v", got)
			}

			if tt.cancelParent {
				parentCancel()
			}
			if tt.cancelDriver {
				driverCancel()
			}

			err := waitForContextErr(t, opD.Context(), tt.maxWait)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("operation context err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestWithOperationContextPreviousParentDeadlineProbe(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer parentCancel()

	d := &Driver{ctx: context.Background()}
	opD, cancel := d.WithOperationContext(parent, time.Second)
	defer cancel()

	err := waitForContextErr(t, opD.Context(), 250*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("operation context err = %v, want deadline exceeded", err)
	}
}

func TestWithOperationContextReturnedCancelStopsOperation(t *testing.T) {
	d := &Driver{ctx: context.Background()}
	opD, cancel := d.WithOperationContext(context.Background(), time.Second)
	cancel()

	err := waitForContextErr(t, opD.Context(), 250*time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("operation context err = %v, want canceled", err)
	}
}

func waitForContextErr(t *testing.T, ctx context.Context, maxWait time.Duration) error {
	t.Helper()
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		t.Fatalf("operation context still live after %s", maxWait)
		return nil
	}
}
