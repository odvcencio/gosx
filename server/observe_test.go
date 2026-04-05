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

func TestJSONOperationObserverWritesStructuredLines(t *testing.T) {
	var out bytes.Buffer
	observer := NewJSONOperationObserver(&out)

	observer.ObserveOperation(OperationEvent{
		Component: "isr",
		Operation: "refresh",
		Target:    "/docs",
		Status:    "error",
		Duration:  250 * time.Millisecond,
		Error:     "unexpected status 500",
	})

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'})
	if len(lines) != 1 {
		t.Fatalf("expected one JSON line, got %d", len(lines))
	}

	var record OperationLogRecord
	if err := json.Unmarshal(lines[0], &record); err != nil {
		t.Fatalf("unmarshal operation log: %v", err)
	}
	if record.Component != "isr" || record.Operation != "refresh" || record.Target != "/docs" {
		t.Fatalf("unexpected operation log record %+v", record)
	}
	if record.Status != "error" || record.DurationMS != 250 || record.Error == "" {
		t.Fatalf("unexpected operation payload %+v", record)
	}
}

func TestJSONOperationObserverIgnoresNilWriter(t *testing.T) {
	observer := NewJSONOperationObserver(nil)
	observer.ObserveOperation(OperationEvent{Component: "isr"})
}
