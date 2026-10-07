package telemetry

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func clearTelemetryEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"GOSX_TELEMETRY", "GOSX_TELEMETRY_ADDR", "GOSX_TELEMETRY_METRICS_TOKEN", "GOSX_TELEMETRY_METRICS_TOKEN_FILE", "GOSX_TELEMETRY_ADMIN_TOKEN", "GOSX_TELEMETRY_ADMIN_TOKEN_FILE", "GOSX_TELEMETRY_SECRET", "GOSX_TELEMETRY_SPOOL_DIR"} {
		t.Setenv(name, "")
	}
}

func TestFromEnvRejectsUnsupportedFeatures(t *testing.T) {
	clearTelemetryEnv(t)
	for _, tc := range []struct {
		name      string
		configure func(*Options)
	}{
		{"persistence", func(o *Options) { o.Persistence.Enabled = true }},
		{"persistence_fallback", func(o *Options) { o.Persistence.ContinueOnUnavailable = true }},
		{"sessions", func(o *Options) { o.Sessions.Enabled = true }},
		{"vitals", func(o *Options) { o.Vitals.SampleRate = .1 }},
		{"engine_vitals", func(o *Options) { o.Vitals.EngineSampleRate = .1 }},
		{"client_health", func(o *Options) { o.Vitals.ClientHealthSampleRate = .1 }},
		{"engines", func(o *Options) { o.Vitals.Engines = []EngineKind{{Name: "game", Backends: []string{"wasm"}}} }},
		{"visitor", func(o *Options) {
			o.Visitor.Enabled = true
			o.Visitor.Secret = []byte(strings.Repeat("s", 32))
			o.Visitor.Key = func(*http.Request) []byte { return []byte("synthetic") }
			o.Consent = func(*http.Request) Consent { return ConsentGranted }
		}},
		{"desktop", func(o *Options) {
			o.Desktop = &DesktopOptions{DataDir: "synthetic", Authorize: func(*http.Request) bool { return true }}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := Defaults()
			tc.configure(&o)
			_, err := FromEnv(o)
			var config *ConfigError
			if !errors.Is(err, ErrInvalidOptions) || !errors.As(err, &config) || config.Code != "unsupported" {
				t.Fatalf("unsupported feature accepted or unclassified: %v", err)
			}
		})
	}
	t.Setenv("GOSX_TELEMETRY_SPOOL_DIR", "private-canary")
	_, err := FromEnv(Defaults())
	var config *ConfigError
	if !errors.As(err, &config) || config.Code != "unsupported" || strings.Contains(err.Error(), "private-canary") {
		t.Fatal(err)
	}
}

func TestCredentialAndListenerValidation(t *testing.T) {
	clearTelemetryEnv(t)
	token := strings.Repeat("m", 32)
	admin := strings.Repeat("a", 32)
	for _, tc := range []struct {
		name     string
		listen   ListenOptions
		insecure bool
	}{
		{"short", ListenOptions{Metrics: Credential{Token: "x"}}, false},
		{"space", ListenOptions{Metrics: Credential{Token: token + " "}}, false},
		{"control", ListenOptions{Metrics: Credential{Token: token + "\n"}}, false},
		{"padding", ListenOptions{Metrics: Credential{Token: token + "=x"}}, false},
		{"only_padding", ListenOptions{Metrics: Credential{Token: strings.Repeat("=", 32)}}, false},
		{"non_ascii", ListenOptions{Metrics: Credential{Token: token + "é"}}, false},
		{"large", ListenOptions{Metrics: Credential{Token: strings.Repeat("m", 2049)}}, false},
		{"sources", ListenOptions{Metrics: Credential{Token: token, TokenFile: "private-canary"}}, false},
		{"same_tokens", ListenOptions{Metrics: Credential{Token: token}, Admin: Credential{Token: token}}, false},
		{"same_files", ListenOptions{Metrics: Credential{TokenFile: "private-canary"}, Admin: Credential{TokenFile: "./private-canary"}}, false},
		{"wildcard4", ListenOptions{Addr: "0.0.0.0:9464"}, true},
		{"wildcard6", ListenOptions{Addr: "[::]:9464"}, true},
		{"private", ListenOptions{Addr: "192.0.2.10:9464", Admin: Credential{Token: admin}}, true},
		{"dns", ListenOptions{Addr: "private-canary:9464"}, false},
		{"localhost", ListenOptions{Addr: "localhost:9464"}, false},
		{"empty_host", ListenOptions{Addr: ":9464"}, false},
		{"wildcard_alias", ListenOptions{Addr: "*:9464"}, false},
		{"port", ListenOptions{Addr: "127.0.0.1:65536"}, false},
		{"signed_port", ListenOptions{Addr: "127.0.0.1:+1"}, false},
		{"remote_zero", ListenOptions{Addr: "192.0.2.10:0", Metrics: Credential{Token: token}}, false},
		{"relative_socket", ListenOptions{Addr: "unix:private-canary"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := Defaults()
			o.Listen = tc.listen
			_, err := FromEnv(o)
			var config *ConfigError
			if !errors.Is(err, ErrInvalidOptions) || !errors.As(err, &config) || strings.Contains(err.Error(), "private-canary") || strings.Contains(err.Error(), token) {
				t.Fatalf("validation: %v", err)
			}
			if tc.insecure && !errors.Is(err, ErrInsecureListener) {
				t.Fatalf("insecure listener not classified: %v", err)
			}
		})
	}
	for _, listen := range []ListenOptions{
		{Addr: "off"}, {Addr: "127.0.0.1:9464"}, {Addr: "[::1]:0"},
		{Addr: "0.0.0.0:9464", Metrics: Credential{Token: token}, Admin: Credential{Token: admin}},
		{Addr: "[::]:9464", Metrics: Credential{TokenFile: "synthetic-token-file"}},
		{Addr: "192.0.2.10:9464", DangerouslyAllowUnauthenticatedMetricsOnNonLoopback: true},
	} {
		o := Defaults()
		o.Listen = listen
		if _, err := FromEnv(o); err != nil {
			t.Fatalf("valid listener rejected: %v", err)
		}
	}
}

func TestEmptyEnvPreservesExplicitSettingsAndDisable(t *testing.T) {
	clearTelemetryEnv(t)
	base := Defaults()
	base.Disabled = true
	base.Listen = ListenOptions{Addr: "off", Metrics: Credential{Token: strings.Repeat("m", 32)}, Admin: Credential{Token: strings.Repeat("a", 32)}}
	for _, value := range []string{"", "on", "true", "1", "yes"} {
		t.Setenv("GOSX_TELEMETRY", value)
		n, err := FromEnv(base)
		if err != nil || !n.Disabled || n.Listen != base.Listen {
			t.Fatalf("empty environment cleared explicit privacy settings: %v", err)
		}
	}
	t.Setenv("GOSX_TELEMETRY", "off")
	base.Disabled = false
	n, err := FromEnv(base)
	if err != nil || !n.Disabled {
		t.Fatalf("environment could not disable telemetry: %v", err)
	}
}
