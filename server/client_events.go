package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ClientEventsRoute is the path served by ClientEventsHandler.
const ClientEventsRoute = "/_gosx/client-events"

const (
	defaultClientEventsMaxBody = 64 * 1024
	defaultClientEventsRate    = 60 // events per minute per remote addr
	clientEventsMaxEvents      = 100
	clientEventsMaxCatLen      = 64
	clientEventsMaxMsgLen      = 512
	clientEventsMaxFieldLen    = 2 * 1024
	clientEventsEnvVar         = "GOSX_TELEMETRY"
)

// ClientEventsOptions configures the /_gosx/client-events handler.
type ClientEventsOptions struct {
	// Logger receives one record per accepted event. If nil, Logger() is used.
	Logger *slog.Logger
	// MaxBodyBytes bounds the request body. <=0 uses defaultClientEventsMaxBody.
	MaxBodyBytes int64
	// RatePerMin is a per-remote-addr limit. <=0 uses defaultClientEventsRate.
	RatePerMin int
	// Enabled is consulted per-request; if it returns false the handler 404s.
	// When nil, the handler checks the GOSX_TELEMETRY env var — "off"/"false"/"0" disables.
	Enabled func() bool
}

type clientEventBatch struct {
	SID    string         `json:"sid"`
	SentAt int64          `json:"sent_at"`
	Events []clientEvent  `json:"events"`
	Meta   map[string]any `json:"meta,omitempty"`
}

type clientEvent struct {
	TS     int64          `json:"ts"`
	Level  string         `json:"lvl"`
	Cat    string         `json:"cat"`
	Msg    string         `json:"msg"`
	URL    string         `json:"url,omitempty"`
	UA     string         `json:"ua,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

// ClientEventsHandler returns an HTTP handler that accepts batched
// client-originated events and forwards each to the configured slog.Logger.
//
// The handler enforces:
//   - POST only (405 otherwise)
//   - body <= MaxBodyBytes (413)
//   - well-formed JSON matching clientEventBatch (400)
//   - per-remote-addr rate limit (429)
//   - kill switch via Enabled / GOSX_TELEMETRY (404)
//
// Unknown levels downgrade to info. Category, message, URL, UA, and individual
// field values are truncated to bounded lengths so a misbehaving client cannot
// emit unbounded log lines.
func ClientEventsHandler(opts ClientEventsOptions) http.Handler {
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultClientEventsMaxBody
	}
	rate := opts.RatePerMin
	if rate <= 0 {
		rate = defaultClientEventsRate
	}
	enabled := opts.Enabled
	if enabled == nil {
		enabled = clientEventsEnvEnabled
	}

	limiter := newClientEventsLimiter(rate)

	logger := func() *slog.Logger {
		if opts.Logger != nil {
			return opts.Logger
		}
		return Logger()
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !enabled() {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		remote := clientEventsRemoteAddr(r)
		if !limiter.Allow(remote) {
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var mbErr *http.MaxBytesError
			if errors.As(err, &mbErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}

		var batch clientEventBatch
		if err := json.Unmarshal(body, &batch); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}

		if len(batch.Events) > clientEventsMaxEvents {
			batch.Events = batch.Events[:clientEventsMaxEvents]
		}

		lg := logger()
		ctx := r.Context()
		for i := range batch.Events {
			logClientEvent(ctx, lg, &batch, &batch.Events[i])
		}

		w.WriteHeader(http.StatusNoContent)
	})
}

func clientEventsEnvEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(clientEventsEnvVar))) {
	case "off", "false", "0", "disabled", "no":
		return false
	default:
		return true
	}
}

func clientEventsRemoteAddr(r *http.Request) string {
	if r.RemoteAddr == "" {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func logClientEvent(ctx context.Context, lg *slog.Logger, batch *clientEventBatch, ev *clientEvent) {
	level := parseClientEventLevel(ev.Level)
	cat := truncate(ev.Cat, clientEventsMaxCatLen)
	msg := truncate(ev.Msg, clientEventsMaxMsgLen)

	attrs := make([]slog.Attr, 0, 6+len(ev.Fields))
	if cat != "" {
		attrs = append(attrs, slog.String("gosx.cat", cat))
	}
	if batch.SID != "" {
		attrs = append(attrs, slog.String("gosx.sid", truncate(batch.SID, clientEventsMaxCatLen)))
	}
	if ev.URL != "" {
		attrs = append(attrs, slog.String("gosx.url", truncate(ev.URL, clientEventsMaxMsgLen)))
	}
	if ev.UA != "" {
		attrs = append(attrs, slog.String("gosx.ua", truncate(ev.UA, clientEventsMaxMsgLen)))
	}
	if ev.TS != 0 {
		attrs = append(attrs, slog.Int64("gosx.ts", ev.TS))
	}
	for key, raw := range ev.Fields {
		if key == "" {
			continue
		}
		attrs = append(attrs, slog.Any("gosx."+truncate(key, clientEventsMaxCatLen), clampFieldValue(raw)))
	}

	lg.LogAttrs(ctx, level, msg, attrs...)
}

func parseClientEventLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

func clampFieldValue(v any) any {
	switch x := v.(type) {
	case string:
		return truncate(x, clientEventsMaxFieldLen)
	default:
		return v
	}
}

// clientEventsLimiter is a tiny per-key sliding-minute counter.
type clientEventsLimiter struct {
	rate int
	mu   sync.Mutex
	buck map[string]*limiterBucket
}

type limiterBucket struct {
	window time.Time
	count  int
}

func newClientEventsLimiter(rate int) *clientEventsLimiter {
	return &clientEventsLimiter{
		rate: rate,
		buck: make(map[string]*limiterBucket),
	}
}

func (l *clientEventsLimiter) Allow(key string) bool {
	if l == nil || l.rate <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buck[key]
	if !ok || now.Sub(b.window) >= time.Minute {
		l.buck[key] = &limiterBucket{window: now, count: 1}
		l.prune(now)
		return true
	}
	if b.count >= l.rate {
		return false
	}
	b.count++
	return true
}

// prune drops buckets older than two minutes to bound memory.
// Called under l.mu.
func (l *clientEventsLimiter) prune(now time.Time) {
	if len(l.buck) < 1024 {
		return
	}
	for k, b := range l.buck {
		if now.Sub(b.window) >= 2*time.Minute {
			delete(l.buck, k)
		}
	}
}
