package telemetry

import (
	"context"

	"m31labs.dev/gosx/telemetry/schema"
)

// Sink accepts immutable records synchronously. It must obey its context and
// bound retained data; framework dispatch belongs to the single sink worker.
type Sink interface {
	Write(context.Context, schema.Record) error
	Flush(context.Context) error
	Close(context.Context) error
}

type SinkTransport uint8

const (
	SinkTransportUnknown SinkTransport = iota
	SinkTransportLocal
	SinkTransportNetwork
)

// NamedSink declares a finite trusted sink and its durability promise.
// Memory acceptance does not become local durability by being required.
// Custom sinks must declare their own retained capacity; the framework cannot
// inspect allocations or network calls hidden in application-owned closures.
type NamedSink struct {
	Name             string
	Sink             Sink
	Required         bool
	Persistence      schema.Persistence
	Transport        SinkTransport
	MaxRetainedBytes int64
}
