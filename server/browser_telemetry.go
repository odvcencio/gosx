package server

import (
	"encoding/json"
	"m31labs.dev/gosx"
)

// BrowserTelemetryConfig sets browser reporting consent before the runtime starts.
// Endpoint is optional; omit it to retain the runtime's default endpoint.
type BrowserTelemetryConfig struct {
	Enabled  bool   `json:"enabled"`
	Endpoint string `json:"endpoint,omitempty"`
}

// TelemetryConfigHead returns an early, nonce-bearing telemetry configuration.
// The application owns consent persistence and response caching policy. Add this
// node before the runtime bootstrap so an opt-out applies to startup failures too.
func (c *Context) TelemetryConfigHead(config BrowserTelemetryConfig) gosx.Node {
	data, _ := json.Marshal(config)
	attrs := gosx.Attrs()
	if c != nil {
		if nonce := c.Nonce(); nonce != "" {
			attrs = gosx.Attrs(gosx.Attr("nonce", nonce))
		}
	}
	return gosx.El("script", attrs, gosx.RawHTML("window.__gosx_telemetry_config=Object.assign({},window.__gosx_telemetry_config||{},"+string(data)+");"))
}
