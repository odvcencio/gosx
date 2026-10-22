package hydrate

import (
	"fmt"
	"strings"
)

// HubResyncConfig asks the hub for its current state again after the page
// was not running: tab shown, window focused, network back, back-forward
// cache restore, a wall-clock schedule, and once after the socket opens.
type HubResyncConfig struct {
	Event          string   `json:"event,omitempty"`          // sent to the hub; default "resync"; the server registers hub.On(event, ...)
	MinIntervalMS  int      `json:"minIntervalMs,omitempty"`  // at most one request per interval; default 1200; range 250..600000
	On             []string `json:"on,omitempty"`             // subset of "visible", "focus", "online", "pageshow"; empty means all four
	PeriodMS       int      `json:"periodMs,omitempty"`       // also request at each wall-clock multiple of PeriodMS plus OffsetMS; 0 disables
	OffsetMS       int      `json:"offsetMs,omitempty"`       // delay after each period boundary; range 0..PeriodMS
	InitialDelayMS int      `json:"initialDelayMs,omitempty"` // one request this long after the socket opens; 0 disables
}

// HubLifecycle publishes connection state and configures resync for one hub entry.
type HubLifecycle struct {
	// ConnectionSignal is a shared signal that receives
	// {"state":"connecting|open|reconnecting|error","attempt":n,"at":ms}.
	ConnectionSignal string
	Resync           *HubResyncConfig
}

var hubResyncTriggers = map[string]bool{"visible": true, "focus": true, "online": true, "pageshow": true}

// SetHubLifecycle sets the connection signal and resync options of the hub
// entry with the given ID (the value AddHub returned).
func (m *Manifest) SetHubLifecycle(hubID string, lifecycle HubLifecycle) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	index := -1
	for i := range m.Hubs {
		if m.Hubs[i].ID == hubID {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("hub %q is not in this manifest", hubID)
	}
	signal := strings.TrimSpace(lifecycle.ConnectionSignal)
	if len(signal) > 128 || strings.IndexFunc(signal, isSpaceOrControl) >= 0 {
		return fmt.Errorf("hub %q connectionSignal %q must be 1..128 characters with no whitespace", hubID, signal)
	}
	var resync *HubResyncConfig
	if lifecycle.Resync != nil {
		cfg := *lifecycle.Resync
		cfg.Event = strings.TrimSpace(cfg.Event)
		if cfg.Event == "" {
			cfg.Event = "resync"
		}
		if len(cfg.Event) > 128 || hasControlChar(cfg.Event) || strings.HasPrefix(cfg.Event, "__") {
			return fmt.Errorf("hub %q resync event %q must be at most 128 characters and must not start with __", hubID, cfg.Event)
		}
		if cfg.MinIntervalMS == 0 {
			cfg.MinIntervalMS = 1200
		}
		if cfg.MinIntervalMS < 250 || cfg.MinIntervalMS > 600000 {
			return fmt.Errorf("hub %q resync minIntervalMs %d is outside 250..600000", hubID, cfg.MinIntervalMS)
		}
		var on []string
		seen := map[string]bool{}
		for _, trigger := range cfg.On {
			if !hubResyncTriggers[trigger] {
				return fmt.Errorf("hub %q resync trigger %q is not one of visible, focus, online, pageshow", hubID, trigger)
			}
			if !seen[trigger] {
				seen[trigger] = true
				on = append(on, trigger)
			}
		}
		cfg.On = on
		if cfg.PeriodMS != 0 && (cfg.PeriodMS < 1000 || cfg.PeriodMS > 86400000) {
			return fmt.Errorf("hub %q resync periodMs %d is outside 1000..86400000", hubID, cfg.PeriodMS)
		}
		if cfg.OffsetMS < 0 || (cfg.OffsetMS > 0 && cfg.PeriodMS == 0) || cfg.OffsetMS > cfg.PeriodMS {
			return fmt.Errorf("hub %q resync offsetMs %d needs periodMs and must not exceed it", hubID, cfg.OffsetMS)
		}
		if cfg.InitialDelayMS < 0 || cfg.InitialDelayMS > 600000 {
			return fmt.Errorf("hub %q resync initialDelayMs %d is outside 0..600000", hubID, cfg.InitialDelayMS)
		}
		resync = &cfg
	}
	m.Hubs[index].ConnectionSignal = signal
	m.Hubs[index].Resync = resync
	return nil
}
