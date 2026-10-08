package telemetry

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefaultsPureAndZeroNormalization(t *testing.T) {
	t.Setenv("GOSX_TELEMETRY", "off")
	t.Setenv("GOSX_TELEMETRY_ADDR", "private-canary")
	d := Defaults()
	if d.Disabled || d.Listen.Addr != "" || d.Clock != nil || d.Entropy != nil || d.Sessions.Enabled || d.Persistence.Enabled || d.Visitor.Enabled || d.Vitals.SampleRate != 0 {
		t.Fatalf("impure or identifying defaults: %#v", d)
	}
	z, err := normalize(Options{})
	if err != nil || !reflect.DeepEqual(z, d) {
		t.Fatalf("zero=%#v error=%v", z, err)
	}
	if d.Limits.MemoryBudgetBytes != 32<<20 || d.Activities.MaxOpen != 256 || d.Activities.MaxEvents != 1024 || d.Metrics.MaxSeries != 32768 {
		t.Fatal("default capacity changed")
	}
	if d.Identity.App != "app" || d.Identity.Version != "unknown" || d.Identity.Revision != "unknown" {
		t.Fatal(d.Identity)
	}
}

func TestInvalidOptionsAreRedacted(t *testing.T) {
	cases := []func(*Options){
		func(o *Options) { o.Mode = 255 }, func(o *Options) { o.Metrics.MaxSeries = -1 },
		func(o *Options) { o.Metrics.MaxRoutePatterns = 513 }, func(o *Options) { o.ClientEvents.MaxBodyBytes = 65537 },
		func(o *Options) { o.Sessions.IdleTimeout = -1 }, func(o *Options) { o.Activities.MaxParticipants = 33 },
		func(o *Options) { o.Vitals.SampleRate = math.NaN() }, func(o *Options) { o.Vitals.EngineSampleRate = math.Inf(1) },
		func(o *Options) { o.Vitals.ClientHealthSampleRate = 1.01 }, func(o *Options) { o.Limits.MemoryBudgetBytes = 1 << 20 },
		func(o *Options) { o.Limits.MaxQueuedBytes = math.MaxInt64 }, func(o *Options) { o.Limits.MaxQueuedBytes = 1 },
		func(o *Options) { o.Identity.App = strings.Repeat("private-canary", 20) },
		func(o *Options) { o.Listen.Metrics = Credential{Token: "private-canary", TokenFile: "private-canary"} },
		func(o *Options) { o.ClientEvents.Categories = []string{"private-canary", "private-canary"} },
		func(o *Options) { o.Metrics.Operations = []Operation{{}} },
		func(o *Options) { o.Metrics.Operations = []Operation{{"game", "tick"}, {"game", "tick"}} },
		func(o *Options) { o.Vitals.Engines = []EngineKind{{Name: "invalid engine"}} },
		func(o *Options) { o.Vitals.Engines = []EngineKind{{Name: "game", Backends: []string{"vm", "vm"}}} },
		func(o *Options) {
			o.Vitals.Engines = []EngineKind{{Name: "engine", Backends: []string{"private-canary"}}}
		},
		func(o *Options) { o.Mode = ModeDesktop }, func(o *Options) { o.Visitor.Enabled = true },
	}
	for i, configure := range cases {
		o := Defaults()
		configure(&o)
		_, err := normalize(o)
		var config *ConfigError
		if !errors.Is(err, ErrInvalidOptions) || !errors.As(err, &config) || strings.Contains(err.Error(), "private-canary") {
			t.Fatalf("case %d: %v", i, err)
		}
	}
}

func TestOptionsCopiesAndReservations(t *testing.T) {
	o := Defaults()
	o.ClientEvents.Categories = []string{"game"}
	o.Metrics.Operations = []Operation{{"game", "tick"}}
	n, err := normalize(o)
	if err != nil {
		t.Fatal(err)
	}
	o.ClientEvents.Categories[0] = "changed"
	o.Metrics.Operations[0].Name = "changed"
	if n.ClientEvents.Categories[0] != "game" || n.Metrics.Operations[0].Name != "tick" {
		t.Fatal("option slices alias caller")
	}
	o = Defaults()
	o.Sessions.Enabled = true
	n, err = normalize(o)
	var unsupported *ConfigError
	if !errors.As(err, &unsupported) || unsupported.Code != "unsupported" {
		t.Fatalf("unimplemented sessions accepted: %v", err)
	}
	o = Defaults()
	o.Activities.MaxOpen = 1
	o.Limits.MemoryBudgetBytes = 21 << 20
	if _, err = normalize(o); err != nil {
		t.Fatal("compatible reduced caps rejected", err)
	}
	o.Activities.MaxOpen = 256
	if _, err = normalize(o); err == nil {
		t.Fatal("incompatible caps silently scaled")
	}
	o = Defaults()
	o.Visitor.Rotation = time.Hour
	o.Visitor.Enabled = true
	if _, err = normalize(o); err == nil {
		t.Fatal("invalid visitor accepted")
	}
}

func TestFromEnvExplicitOverlayAndNoImplicitVisitor(t *testing.T) {
	for _, name := range []string{"GOSX_TELEMETRY", "GOSX_TELEMETRY_ADDR", "GOSX_TELEMETRY_METRICS_TOKEN", "GOSX_TELEMETRY_METRICS_TOKEN_FILE", "GOSX_TELEMETRY_ADMIN_TOKEN", "GOSX_TELEMETRY_ADMIN_TOKEN_FILE", "GOSX_TELEMETRY_SPOOL_DIR", "GOSX_TELEMETRY_SECRET"} {
		t.Setenv(name, "")
	}
	t.Setenv("GOSX_TELEMETRY", "off")
	t.Setenv("GOSX_TELEMETRY_ADDR", "off")
	t.Setenv("GOSX_TELEMETRY_SECRET", "private-canary")
	n, err := FromEnv(Defaults())
	if err != nil || !n.Disabled || n.Listen.Addr != "off" || n.Visitor.Enabled || len(n.Visitor.Secret) != 0 {
		t.Fatalf("overlay: %#v %v", n, err)
	}
	t.Setenv("GOSX_TELEMETRY", "private-canary")
	_, err = FromEnv(Options{})
	if !errors.Is(err, ErrInvalidOptions) || strings.Contains(err.Error(), "private-canary") {
		t.Fatal(err)
	}
	for _, value := range []string{"off", "false", "0", "disabled", "no", "on", "true", "1", "yes", ""} {
		if _, err := telemetrySwitch(value); err != nil {
			t.Fatal(value, err)
		}
	}
}
