package auth

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestJSONObserverWritesStructuredLines(t *testing.T) {
	var out bytes.Buffer
	observer := NewJSONObserver(&out)

	observer.ObserveAuth(AuthEvent{
		Type:       "sign_in",
		Success:    true,
		UserID:     "u_123",
		Email:      "ada@example.com",
		Path:       "/login",
		Method:     "POST",
		OccurredAt: time.Date(2026, 4, 4, 21, 0, 0, 0, time.UTC),
	})

	lines := bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'})
	if len(lines) != 1 {
		t.Fatalf("expected one JSON line, got %d", len(lines))
	}

	var record AuthLogRecord
	if err := json.Unmarshal(lines[0], &record); err != nil {
		t.Fatalf("unmarshal auth log: %v", err)
	}
	if record.Type != "sign_in" || !record.Success || record.UserID != "u_123" || record.Email != "ada@example.com" {
		t.Fatalf("unexpected auth log record %+v", record)
	}
	if record.Path != "/login" || record.Method != "POST" {
		t.Fatalf("unexpected auth request metadata %+v", record)
	}
}

func TestJSONObserverIgnoresNilWriter(t *testing.T) {
	observer := NewJSONObserver(nil)
	observer.ObserveAuth(AuthEvent{Type: "sign_in"})
}
