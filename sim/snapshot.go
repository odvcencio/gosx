package sim

// snapshotEntry holds a single frame's snapshot data.
type snapshotEntry struct {
	frame uint64
	data  []byte
}

// snapshotRing is a fixed-capacity circular buffer of simulation snapshots.
type snapshotRing struct {
	entries []snapshotEntry
	head    int
	size    int
	cap     int
}

// newSnapshotRing creates a ring buffer that holds up to capacity snapshots.
func newSnapshotRing(capacity int) *snapshotRing {
	return &snapshotRing{
		entries: make([]snapshotEntry, capacity),
		cap:     capacity,
	}
}

// Push stores a snapshot for the given frame, copying data to avoid aliasing.
func (r *snapshotRing) Push(frame uint64, data []byte) {
	cp := make([]byte, len(data))
	copy(cp, data)
	r.entries[r.head] = snapshotEntry{frame: frame, data: cp}
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

// Get retrieves the snapshot for a given frame, scanning from newest to oldest.
// Returns nil, false if the frame is not in the buffer.
func (r *snapshotRing) Get(frame uint64) ([]byte, bool) {
	for i := 0; i < r.size; i++ {
		idx := (r.head - 1 - i + r.cap) % r.cap
		if r.entries[idx].frame == frame {
			return r.entries[idx].data, true
		}
	}
	return nil, false
}
