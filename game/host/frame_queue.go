package host

// frameRequest is one pending RequestFrame call.
type frameRequest struct {
	handle int
	cb     func(timestampMS float64)
}

// frameQueue holds the callbacks waiting for the next frame. begin moves them
// into the batch that fires now; next pops them in request order, skipping any
// cancelled meanwhile. A request made while a batch fires waits for the next
// frame. The two buffers swap, so steady-state frames allocate nothing.
type frameQueue struct {
	waiting []frameRequest
	firing  []frameRequest
	pos     int
	last    int
}

// add queues cb for the next frame and returns its handle, which is never 0.
func (q *frameQueue) add(cb func(float64)) int {
	q.last++
	if q.last <= 0 {
		q.last = 1
	}
	q.waiting = append(q.waiting, frameRequest{handle: q.last, cb: cb})
	return q.last
}

// cancel drops the request with handle, whether it waits or is in the batch
// that is firing. An unknown handle is ignored.
func (q *frameQueue) cancel(handle int) {
	for i := range q.waiting {
		if q.waiting[i].handle == handle {
			copy(q.waiting[i:], q.waiting[i+1:])
			q.waiting[len(q.waiting)-1] = frameRequest{}
			q.waiting = q.waiting[:len(q.waiting)-1]
			return
		}
	}
	for i := q.pos; i < len(q.firing); i++ {
		if q.firing[i].handle == handle {
			q.firing[i].cb = nil
			return
		}
	}
}

// empty reports whether no request waits for a frame.
func (q *frameQueue) empty() bool { return len(q.waiting) == 0 }

// begin starts a frame: the waiting requests become the firing batch.
func (q *frameQueue) begin() {
	clear(q.firing)
	q.firing, q.waiting = q.waiting, q.firing[:0]
	q.pos = 0
}

// next returns the next callback of the firing batch.
func (q *frameQueue) next() (func(float64), bool) {
	for q.pos < len(q.firing) {
		r := q.firing[q.pos]
		q.firing[q.pos] = frameRequest{}
		q.pos++
		if r.cb != nil {
			return r.cb, true
		}
	}
	return nil, false
}

// clear drops every request.
func (q *frameQueue) clear() {
	clear(q.firing)
	clear(q.waiting)
	q.firing, q.waiting, q.pos = q.firing[:0], q.waiting[:0], 0
}
