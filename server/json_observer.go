package server

import (
	"encoding/json"
	"io"
	"sync"
)

// JSONRequestObserver writes one JSON record per observed request.
type JSONRequestObserver struct {
	mu     sync.Mutex
	writer io.Writer
}

// RequestLogRecord is the structured log payload emitted by JSONRequestObserver.
type RequestLogRecord struct {
	ID         string `json:"id,omitempty"`
	Method     string `json:"method,omitempty"`
	Path       string `json:"path,omitempty"`
	Pattern    string `json:"pattern,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Status     int    `json:"status"`
	DurationMS int64  `json:"duration_ms"`
}

// NewJSONRequestObserver creates a structured request observer that emits one
// JSON line per request to w.
func NewJSONRequestObserver(w io.Writer) *JSONRequestObserver {
	return &JSONRequestObserver{writer: w}
}

// Observe records a request as a JSON log line.
func (o *JSONRequestObserver) Observe(event RequestEvent) {
	if o == nil || o.writer == nil {
		return
	}
	line, err := json.Marshal(requestLogRecord(event))
	if err != nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = o.writer.Write(append(line, '\n'))
}

func requestLogRecord(event RequestEvent) RequestLogRecord {
	return RequestLogRecord{
		ID:         event.ID,
		Method:     event.Method,
		Path:       event.Path,
		Pattern:    event.Pattern,
		Kind:       event.Kind,
		Status:     event.Status,
		DurationMS: event.Duration.Milliseconds(),
	}
}
