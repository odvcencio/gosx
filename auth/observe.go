package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

// AuthEvent describes an authentication lifecycle event.
type AuthEvent struct {
	Type       string
	Success    bool
	UserID     string
	Email      string
	Provider   string
	Method     string
	Path       string
	Error      string
	OccurredAt time.Time
}

// Observer records authentication events.
type Observer interface {
	ObserveAuth(AuthEvent)
}

// ObserverFunc adapts a function into an auth observer.
type ObserverFunc func(AuthEvent)

// ObserveAuth records an auth event.
func (fn ObserverFunc) ObserveAuth(event AuthEvent) {
	if fn != nil {
		fn(event)
	}
}

// AuthLogRecord is the structured log payload emitted by JSONObserver.
type AuthLogRecord struct {
	Type       string    `json:"type,omitempty"`
	Success    bool      `json:"success"`
	UserID     string    `json:"user_id,omitempty"`
	Email      string    `json:"email,omitempty"`
	Provider   string    `json:"provider,omitempty"`
	Method     string    `json:"method,omitempty"`
	Path       string    `json:"path,omitempty"`
	Error      string    `json:"error,omitempty"`
	OccurredAt time.Time `json:"occurred_at,omitempty"`
}

// JSONObserver writes one JSON record per observed auth event.
type JSONObserver struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewJSONObserver creates a structured auth observer that emits one JSON line
// per event to w.
func NewJSONObserver(w io.Writer) *JSONObserver {
	return &JSONObserver{writer: w}
}

// ObserveAuth records an auth event as a JSON log line.
func (o *JSONObserver) ObserveAuth(event AuthEvent) {
	if o == nil || o.writer == nil {
		return
	}
	line, err := json.Marshal(authLogRecord(event))
	if err != nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = o.writer.Write(append(line, '\n'))
}

func authLogRecord(event AuthEvent) AuthLogRecord {
	return AuthLogRecord{
		Type:       event.Type,
		Success:    event.Success,
		UserID:     event.UserID,
		Email:      event.Email,
		Provider:   event.Provider,
		Method:     event.Method,
		Path:       event.Path,
		Error:      event.Error,
		OccurredAt: event.OccurredAt,
	}
}

func newAuthEvent(r *http.Request, eventType string) AuthEvent {
	event := AuthEvent{
		Type:       eventType,
		OccurredAt: time.Now().UTC(),
	}
	if r != nil {
		event.Method = r.Method
		if r.URL != nil {
			event.Path = r.URL.Path
		}
	}
	return event
}

func observeAuthEvent(manager *Manager, event AuthEvent) {
	if manager == nil {
		return
	}
	manager.observe(event)
}
