package server

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestJSONRequestObserverWritesStructuredLines(t *testing.T) {
	var out bytes.Buffer
	observer := NewJSONRequestObserver(&out)

	observer.Observe(RequestEvent{
		ID:       "gosx-123",
		Method:   "GET",
		Path:     "/docs/runtime",
		Pattern:  "GET /docs/runtime",
		Kind:     "page",
		Status:   200,
		Duration: 1250 * time.Millisecond,
	})

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'})
	if len(lines) != 1 {
		t.Fatalf("expected one JSON line, got %d", len(lines))
	}

	var record RequestLogRecord
	if err := json.Unmarshal(lines[0], &record); err != nil {
		t.Fatalf("unmarshal request log: %v", err)
	}
	if record.ID != "gosx-123" || record.Method != "GET" || record.Path != "/docs/runtime" {
		t.Fatalf("unexpected request log record %+v", record)
	}
	if record.Pattern != "GET /docs/runtime" || record.Kind != "page" {
		t.Fatalf("unexpected route metadata %+v", record)
	}
	if record.Status != 200 || record.DurationMS != 1250 {
		t.Fatalf("unexpected status/duration %+v", record)
	}
}

func TestJSONRequestObserverIgnoresNilWriter(t *testing.T) {
	observer := NewJSONRequestObserver(nil)
	observer.Observe(RequestEvent{Status: 200})
}
