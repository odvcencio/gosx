// Package telemetry provides optional, bounded application telemetry.
package telemetry

import (
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"m31labs.dev/gosx/internal/telemetryerr"
)

type ConfigError = telemetryerr.ConfigError

var (
	ErrAlreadyEnabled      = telemetryerr.ErrAlreadyEnabled
	ErrAfterBuild          = telemetryerr.ErrAfterBuild
	ErrInvalidOptions      = telemetryerr.ErrInvalidOptions
	ErrInsecureListener    = telemetryerr.ErrInsecureListener
	ErrUnsupportedPlatform = telemetryerr.ErrUnsupportedPlatform
	ErrClosed              = telemetryerr.ErrClosed
	ErrCapacity            = telemetryerr.ErrCapacity
	ErrQueueFull           = telemetryerr.ErrQueueFull
	ErrFieldBudget         = telemetryerr.ErrFieldBudget
	ErrConflict            = telemetryerr.ErrConflict
	ErrStorageUnavailable  = telemetryerr.ErrStorageUnavailable
	ErrNotDurable          = telemetryerr.ErrNotDurable
	ErrCorrupt             = telemetryerr.ErrCorrupt
	ErrObjectCollision     = telemetryerr.ErrObjectCollision
)

type Mode uint8

const (
	ModeAuto Mode = iota
	ModeServer
	ModeDevelopment
	ModeDesktop
)

type Options struct {
	Disabled     bool
	Mode         Mode
	Identity     Identity
	Listen       ListenOptions
	Metrics      MetricsOptions
	ClientEvents ClientEventOptions
	Sessions     SessionOptions
	Activities   ActivityOptions
	Persistence  PersistenceOptions
	Vitals       VitalsOptions
	Visitor      VisitorOptions
	Limits       Limits
	Desktop      *DesktopOptions
	Consent      func(*http.Request) Consent
	Logger       *slog.Logger
	Clock        Clock
	Entropy      io.Reader
}
type Identity struct{ App, Version, Revision string }
type Credential struct{ Token, TokenFile string }
type ListenOptions struct {
	Addr                                                string
	Metrics, Admin                                      Credential
	DangerouslyAllowUnauthenticatedMetricsOnNonLoopback bool
}
type MetricsOptions struct {
	DisableRequests, DisableOperations, DisableClientEvents                       bool
	DisableRuntime, DisableReadiness, DisableScheduled                            bool
	MaxSeries, MaxRoutePatterns                                                   int
	Operations                                                                    []Operation
	AuthTypes, AuthProviders, DegradedComponents, ScheduledTasks, ReadinessChecks []string
}
type Operation struct{ Component, Name string }
type ClientEventOptions struct {
	Categories, Codes                                     []string
	RatePerMin, EventsPerMin, BurstEvents, MaxLimiterKeys int
	MaxBodyBytes                                          int64
}
type SessionOptions struct {
	Enabled, Visits, HubSessions   bool
	MaxLiveVisits, MaxHubSessions  int
	IdleTimeout, HeartbeatInterval time.Duration
}
type ActivityOptions struct {
	Disabled                                                           bool
	MaxOpen, MaxRecordBytes, MaxFieldBytes, MaxEvents, MaxParticipants int
	CheckpointInterval, IdleTimeout                                    time.Duration
}

// PersistenceOptions enables an explicit record owner. Sink/spool configuration
// lands with those typed packages; selecting persistence currently fails closed.
type PersistenceOptions struct{ Enabled, ContinueOnUnavailable bool }
type VitalsOptions struct {
	SampleRate, EngineSampleRate, ClientHealthSampleRate float64
	Engines                                              []EngineKind
}
type EngineKind struct {
	Name     string
	Backends []string
}
type VisitorOptions struct {
	Enabled  bool
	Secret   []byte
	Key      func(*http.Request) []byte
	Rotation time.Duration
}
type Limits struct {
	MemoryBudgetBytes, MaxQueuedBytes int64
	MaxQueuedRecords                  int
}
type DesktopOptions struct {
	DataDir   string
	Authorize func(*http.Request) bool
}
type Consent uint8

const (
	ConsentUnknown Consent = iota
	ConsentGranted
	ConsentDeclined
)

// Defaults is pure: it neither reads environment nor allocates clocks/resources.
// Empty listener address is resolved at Enable; optional browser rates stay off.
func Defaults() Options {
	return Options{
		Identity:     Identity{"app", "unknown", "unknown"},
		Metrics:      MetricsOptions{MaxSeries: 32768, MaxRoutePatterns: 512},
		ClientEvents: ClientEventOptions{RatePerMin: 60, EventsPerMin: 600, BurstEvents: 100, MaxLimiterKeys: 4096, MaxBodyBytes: 64 << 10},
		Sessions:     SessionOptions{MaxLiveVisits: 4096, MaxHubSessions: 4096, IdleTimeout: 5 * time.Minute, HeartbeatInterval: time.Minute},
		Activities:   ActivityOptions{MaxOpen: 256, MaxRecordBytes: 16 << 10, MaxFieldBytes: 4 << 10, MaxEvents: 1024, MaxParticipants: 32, CheckpointInterval: 10 * time.Second, IdleTimeout: 6 * time.Hour},
		Visitor:      VisitorOptions{Rotation: 24 * time.Hour},
		Limits:       Limits{MemoryBudgetBytes: 32 << 20, MaxQueuedBytes: 4 << 20, MaxQueuedRecords: 4096},
	}
}

func invalid(field, code string) error { return &ConfigError{Field: field, Code: code} }

func telemetrySwitch(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off", "false", "0", "disabled", "no":
		return true, nil
	case "on", "true", "1", "yes", "":
		return false, nil
	default:
		return false, invalid("telemetry", "switch")
	}
}

// FromEnv overlays nonempty telemetry variables and validates the result.
// The environment can disable telemetry, but cannot reenable Disabled in code.
// An environment secret never enables visitor identity. Values stay out of errors.
// Credential files are read only at startup, not during this pure validation.
// A nonempty environment credential replaces the corresponding code source.
// Providing both token and token-file variables for one role remains an error.
func FromEnv(base Options) (Options, error) {
	var err error
	if value, ok := os.LookupEnv("GOSX_TELEMETRY"); ok {
		disabled, switchErr := telemetrySwitch(value)
		err = switchErr
		base.Disabled = base.Disabled || disabled
		if err != nil {
			return Options{}, err
		}
	}
	for _, item := range []struct {
		name string
		dst  *string
	}{
		{"GOSX_TELEMETRY_ADDR", &base.Listen.Addr},
	} {
		if value, ok := os.LookupEnv(item.name); ok && value != "" {
			*item.dst = value
		}
	}
	for _, item := range []struct {
		name string
		dst  *Credential
	}{{"GOSX_TELEMETRY_METRICS_TOKEN", &base.Listen.Metrics}, {"GOSX_TELEMETRY_ADMIN_TOKEN", &base.Listen.Admin}} {
		token, file := os.Getenv(item.name), os.Getenv(item.name+"_FILE")
		if token != "" && file != "" {
			return Options{}, invalid("credential", "duplicate_source")
		}
		if token != "" {
			*item.dst = Credential{Token: token}
		} else if file != "" {
			*item.dst = Credential{TokenFile: file}
		}
	}
	if value, ok := os.LookupEnv("GOSX_TELEMETRY_SECRET"); ok && value != "" && base.Visitor.Enabled && base.Visitor.Key != nil {
		base.Visitor.Secret = []byte(value)
	}
	if value, ok := os.LookupEnv("GOSX_TELEMETRY_SPOOL_DIR"); ok && value != "" {
		return Options{}, invalid("spool", "unsupported")
	}
	return normalize(base)
}

func normalize(o Options) (Options, error) {
	d := Defaults()
	if strings.EqualFold(o.Listen.Addr, "off") {
		o.Listen.Addr = "off"
	}
	if o.Mode > ModeDesktop {
		return Options{}, invalid("mode", "enum")
	}
	if o.Desktop != nil {
		if o.Mode != ModeAuto && o.Mode != ModeDesktop {
			return Options{}, invalid("desktop", "mode_conflict")
		}
		o.Mode = ModeDesktop
		if o.Desktop.DataDir == "" || o.Desktop.Authorize == nil {
			return Options{}, invalid("desktop", "owner_required")
		}
		copy := *o.Desktop
		o.Desktop = &copy
		if o.Listen.Addr != "" && o.Listen.Addr != "off" {
			return Options{}, invalid("listen", "desktop_network")
		}
	} else if o.Mode == ModeDesktop {
		return Options{}, invalid("desktop", "required")
	}
	for _, item := range []struct {
		dst   *string
		value string
		field string
	}{
		{&o.Identity.App, d.Identity.App, "app"}, {&o.Identity.Version, d.Identity.Version, "app_version"}, {&o.Identity.Revision, d.Identity.Revision, "revision"},
	} {
		if *item.dst == "" {
			*item.dst = item.value
		}
		if len(*item.dst) > 128 || !utf8.ValidString(*item.dst) {
			return Options{}, invalid(item.field, "text")
		}
		*item.dst = strings.Clone(*item.dst)
	}
	for _, item := range []struct {
		dst        *int
		value, max int
		field      string
	}{
		{&o.Metrics.MaxSeries, d.Metrics.MaxSeries, math.MaxInt, "max_series"}, {&o.Metrics.MaxRoutePatterns, 512, 512, "max_routes"},
		{&o.ClientEvents.RatePerMin, 60, math.MaxInt32, "batch_rate"}, {&o.ClientEvents.EventsPerMin, 600, math.MaxInt32, "event_rate"}, {&o.ClientEvents.BurstEvents, 100, 100, "event_burst"}, {&o.ClientEvents.MaxLimiterKeys, 4096, 4096, "limiter_keys"},
		{&o.Sessions.MaxLiveVisits, 4096, 4096, "visits"}, {&o.Sessions.MaxHubSessions, 4096, 4096, "hub_sessions"},
		{&o.Activities.MaxOpen, 256, 256, "activities"}, {&o.Activities.MaxRecordBytes, 16 << 10, 16 << 10, "record_bytes"}, {&o.Activities.MaxFieldBytes, 4 << 10, 4 << 10, "field_bytes"}, {&o.Activities.MaxEvents, 1024, 1024, "activity_events"}, {&o.Activities.MaxParticipants, 32, 32, "participants"},
		{&o.Limits.MaxQueuedRecords, 4096, 4096, "queue_records"},
	} {
		if *item.dst < 0 || *item.dst > item.max {
			return Options{}, invalid(item.field, "range")
		}
		if *item.dst == 0 {
			*item.dst = item.value
		}
	}
	for _, item := range []struct {
		dst        *int64
		value, max int64
		field      string
	}{
		{&o.ClientEvents.MaxBodyBytes, 64 << 10, 64 << 10, "body_bytes"}, {&o.Limits.MemoryBudgetBytes, 32 << 20, math.MaxInt64, "memory_bytes"}, {&o.Limits.MaxQueuedBytes, 4 << 20, math.MaxInt64, "queue_bytes"},
	} {
		if *item.dst < 0 || *item.dst > item.max {
			return Options{}, invalid(item.field, "range")
		}
		if *item.dst == 0 {
			*item.dst = item.value
		}
	}
	for _, item := range []struct {
		dst   *time.Duration
		value time.Duration
		field string
	}{
		{&o.Sessions.IdleTimeout, 5 * time.Minute, "session_idle"}, {&o.Sessions.HeartbeatInterval, time.Minute, "heartbeat"}, {&o.Activities.CheckpointInterval, 10 * time.Second, "checkpoint"}, {&o.Activities.IdleTimeout, 6 * time.Hour, "activity_idle"}, {&o.Visitor.Rotation, 24 * time.Hour, "visitor_rotation"},
	} {
		if *item.dst < 0 {
			return Options{}, invalid(item.field, "negative")
		}
		if *item.dst == 0 {
			*item.dst = item.value
		}
	}
	for _, rate := range []float64{o.Vitals.SampleRate, o.Vitals.EngineSampleRate, o.Vitals.ClientHealthSampleRate} {
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 1 {
			return Options{}, invalid("sample_rate", "range")
		}
	}
	if o.Visitor.Enabled && (len(o.Visitor.Secret) < 32 || o.Visitor.Key == nil || o.Visitor.Rotation < 24*time.Hour) {
		return Options{}, invalid("visitor", "key_required")
	}
	if o.Sessions.Enabled && !o.Sessions.Visits && !o.Sessions.HubSessions {
		o.Sessions.Visits = true
		o.Sessions.HubSessions = true
	}
	if err := validateListenerOptions(o.Listen); err != nil {
		return Options{}, err
	}
	for _, values := range []*[]string{&o.ClientEvents.Categories, &o.ClientEvents.Codes, &o.Metrics.AuthTypes, &o.Metrics.AuthProviders, &o.Metrics.DegradedComponents, &o.Metrics.ScheduledTasks, &o.Metrics.ReadinessChecks} {
		if len(*values) > 64 {
			return Options{}, invalid("enum", "capacity")
		}
		seen := make(map[string]bool, len(*values))
		for _, value := range *values {
			if value == "" || len(value) > 128 || !utf8.ValidString(value) || seen[value] {
				return Options{}, invalid("enum", "text_or_duplicate")
			}
			seen[value] = true
		}
		*values = append([]string(nil), (*values)...)
		for i, value := range *values {
			(*values)[i] = strings.Clone(value)
		}
	}
	if len(o.Metrics.Operations) > 64 || len(o.Vitals.Engines) > 16 {
		return Options{}, invalid("descriptors", "capacity")
	}
	o.Metrics.Operations = append([]Operation(nil), o.Metrics.Operations...)
	seenOperations := make(map[Operation]bool, len(o.Metrics.Operations))
	for i, op := range o.Metrics.Operations {
		if op.Component == "" || op.Name == "" || len(op.Component) > 128 || len(op.Name) > 128 || !utf8.ValidString(op.Component+op.Name) {
			return Options{}, invalid("operation", "text")
		}
		if seenOperations[op] {
			return Options{}, invalid("operation", "duplicate")
		}
		seenOperations[op] = true
		o.Metrics.Operations[i] = Operation{strings.Clone(op.Component), strings.Clone(op.Name)}
	}
	o.Vitals.Engines = append([]EngineKind(nil), o.Vitals.Engines...)
	seenEngines := make(map[string]bool, len(o.Vitals.Engines))
	for i, engine := range o.Vitals.Engines {
		if !kindName(engine.Name) || seenEngines[engine.Name] || len(engine.Backends) > 6 {
			return Options{}, invalid("engine", "descriptor")
		}
		seenEngines[engine.Name] = true
		seenBackends := make(map[string]bool, len(engine.Backends))
		for _, backend := range engine.Backends {
			if seenBackends[backend] {
				return Options{}, invalid("engine_backend", "duplicate")
			}
			seenBackends[backend] = true
			switch backend {
			case "wasm", "vm", "webgl", "webgpu", "canvas", "other":
			default:
				return Options{}, invalid("engine_backend", "enum")
			}
		}
		o.Vitals.Engines[i].Backends = append([]string(nil), engine.Backends...)
		o.Vitals.Engines[i].Name = strings.Clone(engine.Name)
		for j, backend := range engine.Backends {
			o.Vitals.Engines[i].Backends[j] = strings.Clone(backend)
		}
	}
	reserved := int64(16 << 20)
	if !o.Activities.Disabled {
		reserved += int64(o.Activities.MaxOpen) * 32768
	}
	if o.Sessions.Enabled {
		reserved += int64(o.Sessions.MaxLiveVisits+o.Sessions.MaxHubSessions) * 512
	}
	if o.Limits.MaxQueuedBytes > math.MaxInt64-reserved || reserved+o.Limits.MaxQueuedBytes > o.Limits.MemoryBudgetBytes || o.Limits.MaxQueuedBytes < int64(o.Activities.MaxRecordBytes)+256 {
		return Options{}, invalid("memory_bytes", "incompatible_reservations")
	}
	for _, feature := range []struct {
		field    string
		selected bool
	}{
		{"persistence", o.Persistence.Enabled || o.Persistence.ContinueOnUnavailable},
		{"sessions", o.Sessions.Enabled},
		{"vitals", o.Vitals.SampleRate > 0 || o.Vitals.EngineSampleRate > 0 || o.Vitals.ClientHealthSampleRate > 0 || len(o.Vitals.Engines) > 0},
		{"visitor", o.Visitor.Enabled},
		{"desktop", o.Desktop != nil || o.Mode == ModeDesktop},
	} {
		if feature.selected {
			return Options{}, invalid(feature.field, "unsupported")
		}
	}
	return o, nil
}

func kindName(name string) bool {
	if len(name) == 0 || len(name) > 32 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_') {
			return false
		}
	}
	return true
}
