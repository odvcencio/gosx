package telemetrytest

import (
	"context"
	"sync"
	"unsafe"

	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/internal/telemetryrecord"
	"m31labs.dev/gosx/telemetry/schema"
)

// MemorySink is a synchronous bounded live-only sink. Its zero value uses
// 128 records and 1 MiB, including retained record payloads and metadata.
// Acceptance never promises local durability. Records remain readable after
// Close. Copied reader views belong to the caller's own memory budget.
type MemorySink struct {
	mu                  sync.Mutex
	maxRecords          int
	maxBytes            int64
	used                int64
	records             []schema.Record
	failures            [3]error
	initialized, closed bool
}

func NewMemorySink(maxRecords int, maxBytes int64) (*MemorySink, error) {
	if maxRecords < 0 || maxBytes < 0 {
		return nil, &telemetryerr.ConfigError{Field: "memory_sink", Code: "capacity"}
	}
	s := &MemorySink{maxRecords: maxRecords, maxBytes: maxBytes}
	s.initialize()
	return s, nil
}
func (s *MemorySink) initialize() {
	if s.initialized {
		return
	}
	if s.maxRecords == 0 {
		s.maxRecords = 128
	}
	if s.maxBytes == 0 {
		s.maxBytes = 1 << 20
	}
	// Capacity is finite before publication, including the backing header arena.
	slots := s.maxRecords
	size := int64(unsafe.Sizeof(schema.Record{}))
	if int64(slots) > s.maxBytes/size {
		slots = int(s.maxBytes / size)
	}
	s.records = make([]schema.Record, 0, slots)
	s.used = int64(slots) * size
	s.initialized = true
}
func memoryContext(ctx context.Context) error {
	if ctx == nil {
		return &telemetryerr.ConfigError{Field: "sink.context", Code: "required"}
	}
	return ctx.Err()
}
func (s *MemorySink) Write(ctx context.Context, r schema.Record) error {
	if err := memoryContext(ctx); err != nil {
		return err
	}
	if s == nil || !telemetryrecord.Valid(r) {
		return telemetryerr.ErrInvalidOptions
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialize()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return telemetryerr.ErrClosed
	}
	if err := s.failure(0); err != nil {
		return err
	}
	charge := telemetryrecord.RetainedBytes(r) - int64(unsafe.Sizeof(schema.Record{}))
	if len(s.records) == cap(s.records) || charge > s.maxBytes-s.used {
		return telemetryerr.ErrQueueFull
	}
	s.records = append(s.records, r)
	s.used += charge
	return nil
}
func (s *MemorySink) Flush(ctx context.Context) error {
	if err := memoryContext(ctx); err != nil {
		return err
	}
	if s == nil {
		return telemetryerr.ErrInvalidOptions
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialize()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return telemetryerr.ErrClosed
	}
	return s.failure(1)
}
func (s *MemorySink) Close(ctx context.Context) error {
	if err := memoryContext(ctx); err != nil {
		return err
	}
	if s == nil {
		return telemetryerr.ErrInvalidOptions
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initialize()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return nil
	}
	if err := s.failure(2); err != nil {
		return err
	}
	s.closed = true
	return nil
}
func (s *MemorySink) Records() []schema.Record {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]schema.Record(nil), s.records...)
}

// FailNext accepts write/flush/close and replaces that operation's single
// pending failure. A cancelled operation does not consume the injection.
func (s *MemorySink) FailNext(operation string, err error) error {
	slot := -1
	switch operation {
	case "write":
		slot = 0
	case "flush":
		slot = 1
	case "close":
		slot = 2
	}
	if slot < 0 || s == nil {
		return &telemetryerr.ConfigError{Field: "sink.operation", Code: "unsupported"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[slot] = err
	return nil
}

type memoryFailure struct{ cause error }

func (e *memoryFailure) Error() string { return "telemetry: memory sink operation failed" }
func (e *memoryFailure) Unwrap() error { return e.cause }
func (s *MemorySink) failure(slot int) error {
	err := s.failures[slot]
	s.failures[slot] = nil
	if err == nil {
		return nil
	}
	return &memoryFailure{cause: err}
}
