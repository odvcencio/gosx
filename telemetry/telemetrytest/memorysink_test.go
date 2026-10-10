package telemetrytest

import (
	"bytes"
	"context"
	"errors"
	"m31labs.dev/gosx/internal/telemetryerr"
	"m31labs.dev/gosx/internal/telemetryrecord"
	"m31labs.dev/gosx/telemetry/schema"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

func memoryRecord(t *testing.T) schema.Record {
	t.Helper()
	at := time.Unix(100, 0).UTC()
	r, err := telemetryrecord.NewHubSession(schema.Envelope{Stream: "0123456789abcdef0123456789abcdef", Boot: "fedcba9876543210fedcba9876543210", Seq: 1, State: schema.StateOpen, At: at, Revision: 1}, schema.HubSession{ID: "10000000000000000000000000000000", Hub: "room", StartedAt: at, UpdatedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestMemorySinkCountByteCapsAndCopiedRecords(t *testing.T) {
	r := memoryRecord(t)
	s, err := NewMemorySink(8, 0)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() {
			err := s.Write(context.Background(), r)
			if err != nil && !errors.Is(err, telemetryerr.ErrQueueFull) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if len(s.Records()) != 8 {
		t.Fatal("count cap", len(s.Records()))
	}
	rows := s.Records()
	before := rows[0].Bytes()
	rows[0] = schema.Record{}
	raw := s.Records()[0].Bytes()
	raw[0] = '!'
	if !bytes.Equal(before, s.Records()[0].Bytes()) {
		t.Fatal("reader mutated sink")
	}
	size := int64(unsafe.Sizeof(schema.Record{}))
	body := telemetryrecord.RetainedBytes(r) - size
	s, err = NewMemorySink(8, 8*size+2*body)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Write(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Write(context.Background(), r); !errors.Is(err, telemetryerr.ErrQueueFull) || s.used > s.maxBytes {
		t.Fatal("byte cap", err)
	}
}
func TestMemorySinkZeroDefaultsAndNegativeCapacities(t *testing.T) {
	for _, caps := range []struct {
		records int
		bytes   int64
	}{{-1, 0}, {0, -1}} {
		if _, err := NewMemorySink(caps.records, caps.bytes); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
			t.Fatal(err)
		}
	}
	var s MemorySink
	r := memoryRecord(t)
	for range 128 {
		if err := s.Write(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Write(context.Background(), r); !errors.Is(err, telemetryerr.ErrQueueFull) || s.maxBytes != 1<<20 || s.used > s.maxBytes {
		t.Fatal("zero defaults", err)
	}
	tiny, err := NewMemorySink(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := tiny.Write(context.Background(), r); !errors.Is(err, telemetryerr.ErrQueueFull) {
		t.Fatal(err)
	}
}
func TestMemorySinkFailuresContextAndClose(t *testing.T) {
	r := memoryRecord(t)
	for _, op := range []string{"write", "flush", "close"} {
		t.Run(op, func(t *testing.T) {
			s, _ := NewMemorySink(0, 0)
			canary := errors.New("private-sink-error-canary")
			if err := s.FailNext(op, canary); err != nil {
				t.Fatal(err)
			}
			call := func(ctx context.Context) error {
				switch op {
				case "write":
					return s.Write(ctx, r)
				case "flush":
					return s.Flush(ctx)
				default:
					return s.Close(ctx)
				}
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if err := call(cancelled); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if err := call(context.Background()); !errors.Is(err, canary) || strings.Contains(err.Error(), "canary") {
				t.Fatal("injection identity/privacy", err)
			}
			if err := call(context.Background()); err != nil {
				t.Fatal("failure not consumed", err)
			}
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := s.Write(context.Background(), r); !errors.Is(err, telemetryerr.ErrClosed) {
				t.Fatal(err)
			}
		})
	}
	s, _ := NewMemorySink(0, 0)
	if err := s.FailNext("private-operation-canary", nil); !errors.Is(err, telemetryerr.ErrInvalidOptions) || strings.Contains(err.Error(), "canary") {
		t.Fatal(err)
	}
	if err := s.Write(nil, r); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatal(err)
	}
	if err := s.Write(context.Background(), schema.Record{}); !errors.Is(err, telemetryerr.ErrInvalidOptions) {
		t.Fatal("zero record admitted", err)
	}
}

func TestMemorySinkAcceptedWriteAllocations(t *testing.T) {
	s, err := NewMemorySink(128, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	r := memoryRecord(t)
	if got := testing.AllocsPerRun(10, func() {
		if err := s.Write(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}); got != 0 {
		t.Fatal("warm accepted write allocated", got)
	}
}

func BenchmarkMemorySinkAcceptedWrite(b *testing.B) {
	at := time.Unix(100, 0).UTC()
	r, err := telemetryrecord.NewHubSession(schema.Envelope{Stream: "0123456789abcdef0123456789abcdef", Boot: "fedcba9876543210fedcba9876543210", Seq: 1, State: schema.StateOpen, At: at, Revision: 1}, schema.HubSession{ID: "10000000000000000000000000000000", Hub: "room", StartedAt: at, UpdatedAt: at})
	if err != nil {
		b.Fatal(err)
	}
	s, err := NewMemorySink(128, 1<<20)
	if err != nil {
		b.Fatal(err)
	}
	reserved := s.used
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if len(s.records) == 128 {
			b.StopTimer()
			clear(s.records)
			s.records, s.used = s.records[:0], reserved
			b.StartTimer()
		}
		if err := s.Write(context.Background(), r); err != nil {
			b.Fatal(err)
		}
	}
}
