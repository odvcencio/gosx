//go:build !js || !wasm

package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/server"
)

func TestOperationAuthAndDegradedAdapters(t *testing.T) {
	opts := aggregateCoreOptions(t)
	opts.Metrics.DisableOperations = false
	opts.Metrics.Operations = []Operation{{"match", "start"}}
	opts.Metrics.AuthProviders = []string{"local"}
	opts.Metrics.DegradedComponents = []string{"database"}
	tel, err := Enable(server.New(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tel.Close(context.Background()) })
	tel.ObserveOperation(server.OperationEvent{Component: "match", Operation: "start", Status: "ok", Duration: 20 * time.Millisecond, Target: "private-target-canary", Error: "private-error-canary"})
	tel.ObserveOperation(server.OperationEvent{Component: "private-component-canary", Operation: "private-operation-canary", Status: "private-status-canary", Duration: -time.Second})
	if got := hubSample(t, tel, "gosx_operations_total", "component", "match", "operation", "start", "status", "ok").Counter; got != 1 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_operation_duration_seconds", "component", "other", "operation", "other").Histogram; got.Count != 1 || got.Sum != 0 {
		t.Fatal(got)
	}
	observer := tel.AuthObserver()
	observer.ObserveAuth(auth.AuthEvent{Type: "sign_in", Success: true, Provider: "local", UserID: "private-user-canary", Email: "private-email-canary", Path: "private-path-canary", Error: "private-auth-error-canary"})
	observer.ObserveAuth(auth.AuthEvent{Type: "private-type-canary", Provider: "private-provider-canary"})
	observer.ObserveAuth(auth.AuthEvent{Type: "sign_out", Provider: "private-provider-canary"})
	if got := hubSample(t, tel, "gosx_auth_events_total", "type", "sign_in", "success", "true", "provider", "local").Counter; got != 1 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_auth_events_total", "type", "other", "success", "false", "provider", "other").Counter; got != 1 {
		t.Fatal(got)
	}
	if got := hubSample(t, tel, "gosx_auth_events_total", "type", "sign_out", "success", "false", "provider", "other").Counter; got != 1 {
		t.Fatal(got)
	}
	if err := tel.SetDegraded("database", true); err != nil {
		t.Fatal(err)
	}
	if got := hubSample(t, tel, "gosx_degraded", "component", "database").Gauge; got != 1 {
		t.Fatal(got)
	}
	if err := tel.SetDegraded("private-degraded-canary", true); err == nil {
		t.Fatal("undeclared degradation was accepted")
	}
	var text bytes.Buffer
	if err := tel.registry.WritePrometheus(&text); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.String(), "private-") {
		t.Fatal("raw callback data escaped into metrics")
	}
	if got := hubSample(t, tel, "gosx_telemetry_dropped_total", "reason", "unknown_label").Counter; got != 3 {
		t.Fatal(got)
	}
}

func TestAdapterEnumsIncludeFrameworkCapacity(t *testing.T) {
	for _, name := range []string{"operations", "auth", "degraded"} {
		t.Run(name, func(t *testing.T) {
			opts := Defaults()
			for i := 0; i < 64; i++ {
				value := fmt.Sprintf("app_%d", i)
				switch name {
				case "operations":
					opts.Metrics.Operations = append(opts.Metrics.Operations, Operation{"app", value})
				case "auth":
					if i < 15 {
						opts.Metrics.AuthTypes = append(opts.Metrics.AuthTypes, value)
					}
				case "degraded":
					opts.Metrics.DegradedComponents = append(opts.Metrics.DegradedComponents, value)
				}
			}
			if _, err := normalize(opts); err == nil {
				t.Fatal("framework values were omitted from the capacity check")
			}
		})
	}
}

func TestAdaptersAreInertAfterCloseAndDuringConcurrentClose(t *testing.T) {
	opts := aggregateCoreOptions(t)
	opts.Metrics.DisableOperations = false
	tel, err := Enable(server.New(), opts)
	if err != nil {
		t.Fatal(err)
	}
	observer := tel.AuthObserver()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 1000; n++ {
				tel.ObserveOperation(server.OperationEvent{Component: "isr", Operation: "refresh", Status: "ok"})
				observer.ObserveAuth(auth.AuthEvent{Type: "sign_in"})
			}
		}()
	}
	if err := tel.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	before := hubSample(t, tel, "gosx_auth_events_total", "type", "sign_in", "success", "false", "provider", "other").Counter
	observer.ObserveAuth(auth.AuthEvent{Type: "sign_in"})
	if after := hubSample(t, tel, "gosx_auth_events_total", "type", "sign_in", "success", "false", "provider", "other").Counter; after != before {
		t.Fatal("closed observer changed a counter")
	}
	for _, disabled := range []*Telemetry{nil, {}} {
		disabled.ObserveOperation(server.OperationEvent{})
		disabled.AuthObserver().ObserveAuth(auth.AuthEvent{})
		if err := disabled.SetDegraded("database", true); err != nil {
			t.Fatal(err)
		}
	}
}
