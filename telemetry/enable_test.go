//go:build !js || !wasm

package telemetry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/telemetry/metric"
	"m31labs.dev/gosx/telemetry/telemetrytest"
)

func aggregateCoreOptions(t *testing.T) Options {
	t.Helper()
	t.Setenv("GOSX_TELEMETRY", "on")
	t.Setenv("GOSX_DEV", "0")
	o := Defaults()
	o.Listen.Addr = "off"
	o.Metrics.DisableRequests = true
	o.Metrics.DisableOperations = true
	o.Metrics.DisableClientEvents = true
	o.Metrics.DisableRuntime = true
	o.Metrics.DisableReadiness = true
	o.Metrics.DisableScheduled = true
	o.Activities.Disabled = true
	return o
}

func TestEnableDisabledAndZeroHandles(t *testing.T) {
	o := Defaults()
	o.Disabled = true
	t.Setenv("GOSX_TELEMETRY", "on")
	a := server.New()
	tel, err := Enable(a, o)
	if err != nil || !a.ConfigurationOpen() {
		t.Fatal(err)
	}
	for _, handle := range []*Telemetry{nil, {}, tel} {
		if handle.Enabled() || handle.Addr() != nil || handle.Metrics() != nil || handle.Flush(nil) != nil || handle.Close(nil) != nil {
			t.Fatal("disabled handle owns resources")
		}
		for _, handler := range []interface {
			ServeHTTP(http.ResponseWriter, *http.Request)
		}{handle.MetricsHandler(), handle.AdminHandler()} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
			if w.Code != 404 {
				t.Fatal(w.Code)
			}
		}
		if _, err := handle.Metrics().NewCounter(metric.CounterOptions{}); !errors.Is(err, ErrInvalidOptions) {
			t.Fatal("disabled descriptor was not validated", err)
		}
	}
	o.Disabled = false
	t.Setenv("GOSX_TELEMETRY", "off")
	if tel, err := Enable(a, o); err != nil || tel.Enabled() {
		t.Fatal(err)
	}
}

type failingReader struct {
	err    error
	panics bool
}

func (r failingReader) Read([]byte) (int, error) {
	if r.panics {
		panic("private-canary")
	}
	return 0, r.err
}

type invalidTickerClock struct{}

func (invalidTickerClock) Now() Instant                   { return Instant{Wall: time.Unix(0, 0)} }
func (invalidTickerClock) NewTicker(time.Duration) Ticker { return nil }

func TestEnableRollbackAndOwnership(t *testing.T) {
	for _, failure := range []string{"entropy", "entropy_panic", "clock", "reservation"} {
		t.Run(failure, func(t *testing.T) {
			o := aggregateCoreOptions(t)
			a := server.New()
			c := telemetrytest.NewClock(time.Unix(1234, 0))
			o.Clock = c
			cause := errors.New("private-canary")
			switch failure {
			case "entropy":
				o.Entropy = failingReader{err: cause}
			case "entropy_panic":
				o.Entropy = failingReader{panics: true}
			case "clock":
				o.Clock = invalidTickerClock{}
			case "reservation":
				o.Metrics.MaxSeries = 1
			}
			if tel, err := Enable(a, o); tel != nil || err == nil || strings.Contains(err.Error(), "private-canary") {
				t.Fatalf("failed startup: %v %v", tel, err)
			} else if failure == "entropy" && !errors.Is(err, cause) {
				t.Fatal("entropy cause lost")
			}
			if c.PendingTimers() != 0 || !a.ConfigurationOpen() {
				t.Fatal("failed startup retained resources")
			}
			o = aggregateCoreOptions(t)
			o.Clock = c
			tel, err := Enable(a, o)
			if err != nil {
				t.Fatal("rollback retained hook", err)
			}
			if c.PendingTimers() != 1 || !tel.Enabled() {
				t.Fatal("worker ticker ownership")
			}
			if _, err = Enable(a, o); !errors.Is(err, ErrAlreadyEnabled) {
				t.Fatal(err)
			}
			public := tel.Metrics()
			if _, err = public.NewGauge(metric.GaugeOptions{Name: "gosx_build_info"}); !errors.Is(err, ErrInvalidOptions) {
				t.Fatal("reserved view escaped", err)
			}
			a.Build()
			if !public.Usage().Sealed {
				t.Fatal("catalog did not seal registry")
			}
			if _, err = Enable(a, o); !errors.Is(err, ErrAfterBuild) {
				t.Fatal(err)
			}
			if err = tel.Close(context.Background()); err != nil || c.PendingTimers() != 0 {
				t.Fatal(err)
			}
		})
	}
}

func TestEnableEntropyAndUnsupportedFeatures(t *testing.T) {
	o := aggregateCoreOptions(t)
	if _, err := Enable(nil, o); !errors.Is(err, ErrInvalidOptions) {
		t.Fatal(err)
	}
	o.Entropy = bytes.NewReader(make([]byte, 16))
	if _, err := Enable(server.New(), o); !errors.Is(err, io.EOF) {
		t.Fatal("partial entropy accepted", err)
	}
	for _, selectFeature := range []func(*Options){
		func(o *Options) { o.Listen.Addr = "127.0.0.1:0" }, func(o *Options) { o.Metrics.DisableRequests = false },
		func(o *Options) { o.Activities.Disabled = false }, func(o *Options) { o.Sessions.Enabled = true }, func(o *Options) { o.Persistence.Enabled = true },
		func(o *Options) { o.Vitals.SampleRate = .1 },
	} {
		o = aggregateCoreOptions(t)
		selectFeature(&o)
		_, err := Enable(server.New(), o)
		var config *ConfigError
		if !errors.Is(err, ErrInvalidOptions) || !errors.As(err, &config) || config.Code != "unsupported" {
			t.Fatal("unimplemented feature must return the unsupported class", err)
		}
	}
}

func TestAppShutdownClosesTelemetryWorker(t *testing.T) {
	o := aggregateCoreOptions(t)
	c := telemetrytest.NewClock(time.Unix(1234, 0))
	o.Clock = c
	a := server.New()
	tel, err := Enable(a, o)
	if err != nil {
		t.Fatal(err)
	}
	r := tel.Metrics()
	a.Build()
	if err = tel.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = a.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tel.Enabled() || c.PendingTimers() != 0 || !r.Usage().Sealed {
		t.Fatal("shutdown did not release worker")
	}
	if err = tel.Flush(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
}

func TestBuildSealsInactiveTelemetryRegistry(t *testing.T) {
	for _, state := range []string{"closed", "worker_failed"} {
		t.Run(state, func(t *testing.T) {
			o := aggregateCoreOptions(t)
			tick := &controlledTicker{ch: make(chan time.Time)}
			o.Clock = controlledClock{tick}
			a := server.New()
			tel, err := Enable(a, o)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tel.Close(context.Background()) })
			registry := tel.Metrics()
			counter, err := registry.NewCounter(metric.CounterOptions{
				Name: "turns_total", Labels: []metric.Label{{Name: "phase", Values: []string{"before", "after"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := counter.Bind("before"); err != nil {
				t.Fatal(err)
			}
			if state == "closed" {
				if err := tel.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				close(tick.ch)
				select {
				case <-tel.done:
				case <-time.After(time.Second):
					t.Fatal("failed clock worker did not stop")
				}
				if err := tel.Close(context.Background()); !errors.Is(err, ErrInvalidOptions) {
					t.Fatal("worker failure was not reported", err)
				}
			}
			if tel.Enabled() || registry.Usage().Sealed {
				t.Fatal("expected inactive telemetry with registration still open before Build")
			}
			a.Build()
			if !registry.Usage().Sealed {
				t.Fatal("Build did not seal the inactive telemetry registry")
			}
			if _, err := registry.NewCounter(metric.CounterOptions{Name: "late_total"}); !errors.Is(err, ErrAfterBuild) {
				t.Fatal("Build allowed a new metric", err)
			}
			if _, err := counter.Bind("after"); !errors.Is(err, ErrAfterBuild) {
				t.Fatal("Build allowed a new tuple", err)
			}
			if _, err := counter.Bind("before"); err != nil {
				t.Fatal("Build rejected an existing tuple", err)
			}
		})
	}
}

func TestCoreMetricsUseElapsedTimeAndPublicRegistry(t *testing.T) {
	o := aggregateCoreOptions(t)
	c := telemetrytest.NewClock(time.Unix(1234, 500000000))
	o.Clock = c
	a := server.New()
	tel, err := Enable(a, o)
	if err != nil {
		t.Fatal(err)
	}
	defer tel.Close(context.Background())
	custom, err := tel.Metrics().NewCounter(metric.CounterOptions{Name: "game_turns_total"})
	if err != nil {
		t.Fatal(err)
	}
	turns, err := custom.Bind()
	if err != nil {
		t.Fatal(err)
	}
	turns.Add(3)
	a.Build()
	c.JumpWall(2 * time.Hour)
	if err := c.Advance(10 * time.Second); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		values := map[string]float64{}
		if err := tel.Metrics().WithSnapshot(context.Background(), func(s metric.Snapshot) error {
			for _, f := range s.Families {
				if len(f.Series) == 1 {
					values[f.Name] = f.Series[0].Gauge
					if f.Name == "game_turns_total" && f.Series[0].Counter != 3 {
						t.Fatal("application metric was replaced")
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if values["gosx_process_uptime_seconds"] == 10 {
			if values["process_start_time_seconds"] != 1234.5 || values["gosx_build_info"] != 1 || values["gosx_telemetry_series"] != float64(tel.registry.Usage().Samples) || values["gosx_telemetry_memory_bytes"] > float64(o.Limits.MemoryBudgetBytes) {
				t.Fatal(values)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker used a missed deadline instead of current elapsed time", values)
		}
		runtime.Gosched()
	}
}
