package telemetry

import (
	"context"
	"sync"

	"m31labs.dev/gosx/telemetry/schema"
)

// Receipt identifies one admitted revision. A caller's wait never cancels
// shared work; memory acceptance does not promise crash durability.
type Receipt struct {
	state       *receiptState
	persistence schema.Persistence
}
type receiptState struct {
	mu        sync.Mutex
	done      chan struct{}
	err       error
	completed bool
}

func newMemoryReceipt() Receipt {
	return Receipt{state: &receiptState{done: make(chan struct{})}, persistence: schema.PersistenceMemory}
}
func (r Receipt) Persistence() schema.Persistence { return r.persistence }
func (r Receipt) Wait(ctx context.Context) error {
	if ctx == nil {
		return invalid("context", "required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.state == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.state.done:
		if err := ctx.Err(); err != nil {
			return err
		}
		r.state.mu.Lock()
		defer r.state.mu.Unlock()
		return r.state.err
	}
}
func (r Receipt) complete(err error) {
	if r.state == nil {
		return
	}
	s := r.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.completed {
		s.err = err
		s.completed = true
		close(s.done)
	}
}
func (r Receipt) ready() bool {
	if r.state == nil {
		return true
	}
	select {
	case <-r.state.done:
		return true
	default:
		return false
	}
}
