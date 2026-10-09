package telemetry

import (
	"m31labs.dev/gosx/auth"
	"m31labs.dev/gosx/telemetry/metric"
)

type authLabels struct {
	kind, provider string
	success        bool
}

func (t *Telemetry) initializeAuth() error {
	types := uniqueHubValues(append([]string{"sign_in", "sign_out", "other"}, t.opts.Metrics.AuthTypes...))
	providers := uniqueHubValues(append([]string{"other"}, t.opts.Metrics.AuthProviders...))
	v, err := t.counter("gosx_auth_events_total", enumLabel("type", types...), enumLabel("success", "true", "false"), enumLabel("provider", providers...))
	if err != nil {
		return err
	}
	if err := t.declareProduct(v, types, []string{"true", "false"}, providers); err != nil {
		return err
	}
	t.auth = make(map[authLabels]*metric.Counter, len(types)*len(providers)*2)
	for _, kind := range types {
		for _, provider := range providers {
			for _, success := range []bool{false, true} {
				label := "false"
				if success {
					label = "true"
				}
				t.auth[authLabels{kind, provider, success}], _ = v.Bind(kind, label, provider)
			}
		}
	}
	return nil
}

// AuthObserver must be attached explicitly to the application's auth.Manager
// before serving. It never reads identities, emails, paths or error messages.
func (t *Telemetry) AuthObserver() auth.Observer { return auth.ObserverFunc(t.observeAuth) }

func (t *Telemetry) observeAuth(event auth.AuthEvent) {
	if !t.Enabled() {
		return
	}
	key := authLabels{event.Type, event.Provider, event.Success}
	c := t.auth[key]
	if c == nil {
		if t.auth[authLabels{key.kind, "other", key.success}] == nil {
			key.kind = "other"
		}
		if t.auth[authLabels{"other", key.provider, key.success}] == nil {
			key.provider = "other"
		}
		c = t.auth[key]
		t.core.dropped["unknown_label"].Add(1)
	}
	c.Add(1)
}
