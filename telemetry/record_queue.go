package telemetry

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"m31labs.dev/gosx/internal/telemetryrecord"
	"m31labs.dev/gosx/telemetry/schema"
)

// One bounded FIFO feeds the existing worker. Its backing headers are charged
// before any payload is admitted; removing a record clears the retained slot.
type recordQueue struct {
	mu                    sync.Mutex
	slots                 []schema.Record
	head, count, inFlight int
	used                  atomic.Int64
	maxBytes              int64
}

func newRecordQueue(maxRecords int, maxBytes int64) *recordQueue {
	header := int64(unsafe.Sizeof(schema.Record{}))
	count := maxRecords
	if int64(count) > maxBytes/(2*header) {
		count = int(maxBytes / (2 * header))
	}
	q := &recordQueue{slots: make([]schema.Record, count), maxBytes: maxBytes}
	q.used.Store(int64(count) * header)
	return q
}
func queueRecordCharge(r schema.Record) int64 {
	return telemetryrecord.RetainedBytes(r) - int64(unsafe.Sizeof(schema.Record{}))
}
func (q *recordQueue) enqueue(r schema.Record) error {
	if q == nil || !telemetryrecord.Valid(r) {
		return ErrInvalidOptions
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	charge := queueRecordCharge(r)
	if q.count+q.inFlight == len(q.slots) || charge > q.maxBytes-q.used.Load() {
		return ErrQueueFull
	}
	q.slots[(q.head+q.count)%len(q.slots)] = r
	q.count++
	q.used.Add(charge)
	return nil
}
func (q *recordQueue) take() (schema.Record, bool) {
	if q == nil {
		return schema.Record{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.count == 0 {
		return schema.Record{}, false
	}
	r := q.slots[q.head]
	q.slots[q.head] = schema.Record{}
	q.head = (q.head + 1) % len(q.slots)
	q.count--
	q.inFlight++
	return r, true
}
func (q *recordQueue) release(r schema.Record) {
	q.mu.Lock()
	q.inFlight--
	q.used.Add(-queueRecordCharge(r))
	q.mu.Unlock()
}
func (t *Telemetry) drainActivityEvents() {
	if t.activities == nil {
		return
	}
	for {
		record, ok := t.activities.events.take()
		if !ok {
			return
		}
		t.activities.events.release(record)
	}
}
