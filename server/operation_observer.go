package server

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// OperationEvent describes a non-request operational event such as background
// ISR refresh work.
type OperationEvent struct {
	Component string
	Operation string
	Target    string
	Status    string
	Duration  time.Duration
	Error     string
}

// OperationObserver records framework operation events.
type OperationObserver interface {
	ObserveOperation(OperationEvent)
}

// OperationObserverFunc adapts a function into an operation observer.
type OperationObserverFunc func(OperationEvent)

// ObserveOperation records an operation event.
func (fn OperationObserverFunc) ObserveOperation(event OperationEvent) {
	if fn != nil {
		fn(event)
	}
}

// OperationLogRecord is the structured log payload emitted by JSONOperationObserver.
type OperationLogRecord struct {
	Component  string `json:"component,omitempty"`
	Operation  string `json:"operation,omitempty"`
	Target     string `json:"target,omitempty"`
	Status     string `json:"status,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// JSONOperationObserver writes one JSON record per observed operation event.
type JSONOperationObserver struct {
	mu     sync.Mutex
	writer io.Writer
}

// NewJSONOperationObserver creates a structured operation observer that emits one
// JSON line per event to w.
func NewJSONOperationObserver(w io.Writer) *JSONOperationObserver {
	return &JSONOperationObserver{writer: w}
}

// ObserveOperation records an operation event as a JSON log line.
func (o *JSONOperationObserver) ObserveOperation(event OperationEvent) {
	if o == nil || o.writer == nil {
		return
	}
	line, err := json.Marshal(operationLogRecord(event))
	if err != nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = o.writer.Write(append(line, '\n'))
}

func operationLogRecord(event OperationEvent) OperationLogRecord {
	return OperationLogRecord{
		Component:  event.Component,
		Operation:  event.Operation,
		Target:     event.Target,
		Status:     event.Status,
		DurationMS: event.Duration.Milliseconds(),
		Error:      event.Error,
	}
}
