package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClientEventsHandler(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	handler := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	logger := slog.New(handler)
	opts := ClientEventsOptions{
		Logger:       logger,
		MaxBodyBytes: 64 * 1024,
		RatePerMin:   1000,
	}
	return ClientEventsHandler(opts), buf
}

func decodeClientEventsLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			t.Fatalf("decode slog line %q: %v", raw, err)
		}
		lines = append(lines, obj)
	}
	return lines
}

func TestClientEventsHandlerAcceptsWellFormedBatch(t *testing.T) {
	handler, buf := newTestClientEventsHandler(t)

	body := `{
		"sid": "s_abc123",
		"sent_at": 1700000000000,
		"events": [
			{
				"ts": 1699999999000,
				"lvl": "warn",
				"cat": "scene3d",
				"msg": "webgl-context-restored",
				"url": "/",
				"ua": "Mozilla/5.0 test",
				"fields": {"swapped": true, "previousKind": "canvas", "nextKind": "webgl"}
			},
			{
				"ts": 1699999999500,
				"lvl": "error",
				"cat": "runtime",
				"msg": "uncaught: boom",
				"url": "/",
				"fields": {"filename": "bootstrap.js", "lineno": 42}
			}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}

	lines := decodeClientEventsLogLines(t, buf)
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d: %s", len(lines), buf.String())
	}

	first := lines[0]
	if first["level"] != "WARN" {
		t.Fatalf("first level = %v, want WARN", first["level"])
	}
	if first["msg"] != "webgl-context-restored" {
		t.Fatalf("first msg = %v", first["msg"])
	}
	if first["gosx.cat"] != "scene3d" {
		t.Fatalf("first gosx.cat = %v", first["gosx.cat"])
	}
	if first["gosx.sid"] != "s_abc123" {
		t.Fatalf("first gosx.sid = %v", first["gosx.sid"])
	}
	if first["gosx.url"] != "/" {
		t.Fatalf("first gosx.url = %v", first["gosx.url"])
	}
	if first["gosx.swapped"] != true {
		t.Fatalf("first gosx.swapped = %v", first["gosx.swapped"])
	}
	if first["gosx.previousKind"] != "canvas" {
		t.Fatalf("first gosx.previousKind = %v", first["gosx.previousKind"])
	}

	second := lines[1]
	if second["level"] != "ERROR" {
		t.Fatalf("second level = %v, want ERROR", second["level"])
	}
	if second["gosx.cat"] != "runtime" {
		t.Fatalf("second gosx.cat = %v", second["gosx.cat"])
	}
	if second["gosx.filename"] != "bootstrap.js" {
		t.Fatalf("second gosx.filename = %v", second["gosx.filename"])
	}
}

func TestClientEventsHandlerRejectsOversizedBody(t *testing.T) {
	handler, _ := newTestClientEventsHandler(t)

	big := strings.Repeat("x", 65*1024)
	body := `{"sid":"s","sent_at":0,"events":[{"ts":0,"lvl":"info","cat":"test","msg":"` + big + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
}

func TestClientEventsHandlerRejectsInvalidJSON(t *testing.T) {
	handler, _ := newTestClientEventsHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestClientEventsHandlerRejectsNonPost(t *testing.T) {
	handler, _ := newTestClientEventsHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/_gosx/client-events", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestClientEventsHandlerRateLimit(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	opts := ClientEventsOptions{
		Logger:       logger,
		MaxBodyBytes: 64 * 1024,
		RatePerMin:   2,
	}
	handler := ClientEventsHandler(opts)

	body := `{"sid":"s","sent_at":0,"events":[{"ts":0,"lvl":"info","cat":"test","msg":"ok"}]}`

	send := func() int {
		req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", strings.NewReader(body))
		req.RemoteAddr = "192.0.2.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send(); code != http.StatusNoContent {
		t.Fatalf("first request status = %d", code)
	}
	if code := send(); code != http.StatusNoContent {
		t.Fatalf("second request status = %d", code)
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("third request status = %d, want 429", code)
	}
}

func TestClientEventsHandlerRateLimitCountsEventsAtomically(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	handler := ClientEventsHandler(ClientEventsOptions{
		Logger:       logger,
		MaxBodyBytes: 64 * 1024,
		RatePerMin:   3,
	})

	send := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", strings.NewReader(body))
		req.RemoteAddr = "192.0.2.1:1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	twoEvents := `{"events":[
		{"lvl":"info","cat":"test","msg":"first"},
		{"lvl":"info","cat":"test","msg":"second"}
	]}`
	oneEvent := `{"events":[{"lvl":"info","cat":"test","msg":"third"}]}`

	if code := send("{not json"); code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d, want 400", code)
	}
	if code := send(twoEvents); code != http.StatusNoContent {
		t.Fatalf("first two-event batch status = %d", code)
	}
	if code := send(twoEvents); code != http.StatusTooManyRequests {
		t.Fatalf("over-quota two-event batch status = %d, want 429", code)
	}
	if lines := decodeClientEventsLogLines(t, buf); len(lines) != 2 {
		t.Fatalf("rejected batch logged events: got %d lines, want 2", len(lines))
	}
	if code := send(oneEvent); code != http.StatusNoContent {
		t.Fatalf("one-event batch after rejected batch status = %d", code)
	}
	if lines := decodeClientEventsLogLines(t, buf); len(lines) != 3 {
		t.Fatalf("accepted event count = %d log lines, want 3", len(lines))
	}
}

func TestClientEventsHandlerRateLimitCountsEventsAfterBatchCap(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	handler := ClientEventsHandler(ClientEventsOptions{
		Logger:       logger,
		MaxBodyBytes: 64 * 1024,
		RatePerMin:   clientEventsMaxEvents,
	})

	events := make([]clientEvent, clientEventsMaxEvents+1)
	for i := range events {
		events[i] = clientEvent{Level: "info", Cat: "test", Msg: "event"}
	}
	body, err := json.Marshal(clientEventBatch{Events: events})
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.1:1234"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("capped batch status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if lines := decodeClientEventsLogLines(t, buf); len(lines) != clientEventsMaxEvents {
		t.Fatalf("logged capped event count = %d, want %d", len(lines), clientEventsMaxEvents)
	}

	req = httptest.NewRequest(
		http.MethodPost,
		"/_gosx/client-events",
		strings.NewReader(`{"events":[{"lvl":"info","cat":"test","msg":"over quota"}]}`),
	)
	req.RemoteAddr = "192.0.2.1:1234"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("event after capped batch status = %d, want 429", rec.Code)
	}
	if lines := decodeClientEventsLogLines(t, buf); len(lines) != clientEventsMaxEvents {
		t.Fatalf("rejected event changed log count to %d, want %d", len(lines), clientEventsMaxEvents)
	}
}

func TestClientEventsHandlerKillSwitch(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buf, nil))
	opts := ClientEventsOptions{
		Logger:     logger,
		RatePerMin: 1000,
		Enabled:    func() bool { return false },
	}
	handler := ClientEventsHandler(opts)

	body := `{"sid":"s","sent_at":0,"events":[{"ts":0,"lvl":"info","cat":"test","msg":"ok"}]}`
	req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no log lines when disabled, got: %s", buf.String())
	}
}

func TestClientEventsHandlerClampsFieldsAndDropsUnknownLevels(t *testing.T) {
	handler, buf := newTestClientEventsHandler(t)

	body := `{
		"sid": "s",
		"events": [
			{"ts": 1, "lvl": "fatal", "cat": "x", "msg": "unknown-level-downgrades"},
			{"ts": 2, "lvl": "debug", "cat": "x", "msg": "debug-kept"},
			{"ts": 3, "lvl": "info", "cat": "` + strings.Repeat("c", 256) + `", "msg": "cat-truncated"}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}

	lines := decodeClientEventsLogLines(t, buf)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0]["level"] != "INFO" {
		t.Fatalf("unknown level should downgrade to INFO, got %v", lines[0]["level"])
	}
	if lines[1]["level"] != "DEBUG" {
		t.Fatalf("debug level should be kept, got %v", lines[1]["level"])
	}
	if cat, _ := lines[2]["gosx.cat"].(string); len(cat) > 64 {
		t.Fatalf("cat should be truncated to <=64 chars, got len=%d", len(cat))
	}
}

// Smoke test: package-level logger default is non-nil and doesn't panic.
func TestGosxLoggerDefaultIsUsable(t *testing.T) {
	if Logger() == nil {
		t.Fatal("Logger() returned nil")
	}
	Logger().Info("smoke", "k", "v")
}

// Integration: app-wired endpoint should exist when telemetry defaults on.
func TestAppRegistersClientEventsEndpoint(t *testing.T) {
	t.Setenv("GOSX_TELEMETRY", "")
	app := New()
	app.SetClientEventsLogger(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	handler := app.Build()

	body := `{"sid":"s","events":[{"ts":1,"lvl":"info","cat":"x","msg":"ok"}]}`
	req := httptest.NewRequest(http.MethodPost, "/_gosx/client-events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
}
