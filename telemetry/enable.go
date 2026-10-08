package telemetry

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"m31labs.dev/gosx/internal/clock"
	"m31labs.dev/gosx/server"
	"m31labs.dev/gosx/telemetry/metric"
)

// Telemetry has one worker and one named App hook. Nil and zero handles are
// disabled; configure on the application's startup goroutine before Build.
type Telemetry struct {
	opts                Options
	registry, authority *metric.Registry
	active              atomic.Bool
	mu                  sync.Mutex
	closeContext        context.Context
	closeSource         context.Context
	closeCancel         context.CancelFunc
	closeResult         error
	wake, done          chan struct{}
	ticker              Ticker
	ticks               <-chan time.Time
	start               Instant
	boot, limiterSalt   [16]byte
	core                coreMetrics
	adapters            adapterVectors
	operations          map[Operation]operationMeters
	auth                map[authLabels]*metric.Counter
	degraded            map[string]*metric.Gauge
	requests            *requestState
	adapterBytes        atomic.Int64
	hubs                *hubState
	loops               *loopState
	ownerBytes          int64
	faultOnce           sync.Once
}

// Enable validates and acquires everything before attaching the catalog
// callback. Failed startup removes the reserved hook and starts no worker.
func Enable(app *server.App, opts Options) (*Telemetry, error) {
	if app == nil {
		return nil, invalid("app", "required")
	}
	if !app.ConfigurationOpen() {
		return nil, ErrAfterBuild
	}
	o, err := normalize(opts)
	if err != nil {
		return nil, err
	}
	if value, ok := os.LookupEnv("GOSX_TELEMETRY"); ok {
		disabled, err := telemetrySwitch(value)
		if err != nil {
			return nil, err
		}
		o.Disabled = o.Disabled || disabled
	}
	if o.Disabled {
		return &Telemetry{}, nil
	}
	if err := platformEnable(); err != nil {
		return nil, err
	}
	if o.Mode == ModeAuto {
		o.Mode = ModeServer
		if os.Getenv("GOSX_DEV") == "1" {
			o.Mode = ModeDevelopment
		}
	}
	if o.Listen.Addr == "" {
		o.Listen.Addr = "127.0.0.1:9464"
		if o.Mode == ModeDevelopment {
			o.Listen.Addr = "127.0.0.1:0"
		}
		if o.Mode == ModeDesktop {
			o.Listen.Addr = "off"
		}
	}
	if err := availableFeatures(o); err != nil {
		return nil, err
	}
	t := &Telemetry{opts: o, wake: make(chan struct{}, 1), done: make(chan struct{})}
	remove, err := app.UseShutdownHook("telemetry", server.ShutdownHooks{Signal: t.prepareShutdown, Flush: t.Close})
	if err != nil {
		var config *ConfigError
		if errors.As(err, &config) && config.Code == "duplicate_name" {
			return nil, ErrAlreadyEnabled
		}
		return nil, err
	}
	attached := false
	defer func() {
		if !attached {
			if t.ticker != nil {
				_ = stopTicker(t.ticker)
			}
			remove()
		}
	}()
	if o.Entropy == nil {
		o.Entropy = rand.Reader
	}
	if err := initializeEntropy(o.Entropy, &t.boot, &t.limiterSalt); err != nil {
		return nil, err
	}
	if o.Clock == nil {
		o.Clock = clock.New()
	}
	t.opts.Clock = o.Clock
	t.opts.Entropy = o.Entropy
	t.start, t.ticker, t.ticks, err = initializeClock(o.Clock)
	if err != nil {
		return nil, err
	}
	t.ownerBytes = configurationBytes(o)
	t.opts.Listen.Addr = "off"
	t.opts.Visitor.Secret = nil
	if err = t.initializeRegistry(); err != nil {
		return nil, err
	}
	if err = app.UseObservationCatalogObserver(t); err != nil {
		return nil, err
	}
	if !o.Metrics.DisableOperations {
		app.UseOperationObserver(t)
	}
	t.active.Store(true)
	attached = true
	go t.run()
	return t, nil
}

// These implementations land at their own delivery seams. Selecting them
// before they exist returns a fixed class rather than a partially working handle.
func availableFeatures(o Options) error {
	switch {
	case o.Mode == ModeDesktop:
		return invalid("desktop", "unsupported")
	case o.Listen.Addr != "off" || o.Listen.Metrics != (Credential{}) || o.Listen.Admin != (Credential{}) || o.Listen.DangerouslyAllowUnauthenticatedMetricsOnNonLoopback:
		return invalid("listener", "unsupported")
	case !o.Metrics.DisableRequests || !o.Metrics.DisableClientEvents || !o.Metrics.DisableRuntime || !o.Metrics.DisableReadiness || !o.Metrics.DisableScheduled:
		return invalid("metric_adapters", "unsupported")
	case !o.Activities.Disabled:
		return invalid("activities", "unsupported")
	case o.Sessions.Enabled:
		return invalid("sessions", "unsupported")
	case o.Persistence.Enabled:
		return invalid("persistence", "unsupported")
	case o.Visitor.Enabled:
		return invalid("visitor", "unsupported")
	case o.Vitals.SampleRate > 0 || o.Vitals.EngineSampleRate > 0 || o.Vitals.ClientHealthSampleRate > 0:
		return invalid("browser_sampling", "unsupported")
	}
	return nil
}

func initializeEntropy(reader io.Reader, boot, salt *[16]byte) (err error) {
	defer func() {
		if recover() != nil {
			err = invalid("entropy", "unavailable")
		}
	}()
	if _, err = io.ReadFull(reader, boot[:]); err != nil {
		return &setupError{config: &ConfigError{Field: "entropy", Code: "unavailable"}, cause: err}
	}
	if _, err = io.ReadFull(reader, salt[:]); err != nil {
		return &setupError{config: &ConfigError{Field: "entropy", Code: "unavailable"}, cause: err}
	}
	return nil
}

type setupError struct {
	config *ConfigError
	cause  error
}

func (e *setupError) Error() string   { return e.config.Error() }
func (e *setupError) Unwrap() []error { return []error{e.config, e.cause} }

func initializeClock(c Clock) (start Instant, ticker Ticker, ticks <-chan time.Time, err error) {
	defer func() {
		if recover() != nil {
			err = invalid("clock", "unavailable")
		}
	}()
	start, err = readClock(c)
	if err != nil {
		return start, nil, nil, err
	}
	ticker = c.NewTicker(time.Second)
	if ticker != nil {
		ticks = ticker.C()
	}
	if ticks == nil {
		return start, ticker, ticks, invalid("clock", "ticker_required")
	}
	return start, ticker, ticks, nil
}

func configurationBytes(o Options) int64 {
	bytes := int64(4096 + len(o.Identity.App) + len(o.Identity.Version) + len(o.Identity.Revision))
	for _, values := range [][]string{o.ClientEvents.Categories, o.ClientEvents.Codes, o.Metrics.AuthTypes, o.Metrics.AuthProviders, o.Metrics.DegradedComponents, o.Metrics.ScheduledTasks, o.Metrics.ReadinessChecks} {
		for _, value := range values {
			bytes += int64(16 + len(value))
		}
	}
	for _, op := range o.Metrics.Operations {
		bytes += int64(32 + len(op.Component) + len(op.Name))
	}
	for _, engine := range o.Vitals.Engines {
		bytes += int64(40 + len(engine.Name))
		for _, backend := range engine.Backends {
			bytes += int64(16 + len(backend))
		}
	}
	return bytes
}

func (t *Telemetry) Enabled() bool  { return t != nil && t.active.Load() }
func (t *Telemetry) Addr() net.Addr { return nil }
func (t *Telemetry) Metrics() *metric.Registry {
	if !t.Enabled() {
		return nil
	}
	return t.registry
}
func (t *Telemetry) MetricsHandler() http.Handler { return http.NotFoundHandler() }
func (t *Telemetry) AdminHandler() http.Handler   { return http.NotFoundHandler() }

// ObserveCatalog seals registration before public serving, including registries
// retained after Close or a worker failure. Only active adapters admit routes.
func (t *Telemetry) ObserveCatalog(rows []server.ObservationPattern) {
	if t.Enabled() {
		t.admitRequestCatalog(rows)
	}
	if t != nil && t.registry != nil {
		t.registry.Seal()
	}
}
