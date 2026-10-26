// Package dgram holds the fixed-slot datagram queue that netplay network
// transports use between their I/O goroutines and the game loop.
package dgram

import "sync"

// MaxSize is the largest datagram a Queue holds. It equals netplay.MaxPacketSize.
const MaxSize = 1200

// Queue is a bounded FIFO of datagrams. Push and Pop are safe for concurrent
// use and do not allocate. A full Queue drops new datagrams, as a lossy link
// would.
type Queue struct {
	mu      sync.Mutex
	slots   [][MaxSize]byte
	lens    []int
	head, n int
	dropped uint64
}

// NewQueue returns a Queue that holds up to slots datagrams.
func NewQueue(slots int) *Queue {
	if slots < 1 {
		slots = 1
	}
	return &Queue{slots: make([][MaxSize]byte, slots), lens: make([]int, slots)}
}

// Push copies p into the queue. It returns false, and counts a drop, when p
// is longer than MaxSize or the queue is full.
func (q *Queue) Push(p []byte) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(p) > MaxSize || q.n == len(q.slots) {
		q.dropped++
		return false
	}
	i := (q.head + q.n) % len(q.slots)
	q.lens[i] = copy(q.slots[i][:], p)
	q.n++
	return true
}

// Pop copies the oldest datagram into buf, truncating it to len(buf), and
// returns the copied length. ok is false when the queue is empty.
func (q *Queue) Pop(buf []byte) (n int, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.n == 0 {
		return 0, false
	}
	n = copy(buf, q.slots[q.head][:q.lens[q.head]])
	q.head = (q.head + 1) % len(q.slots)
	q.n--
	return n, true
}

// Len returns the number of queued datagrams.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.n
}

// Dropped returns how many datagrams Push has refused.
func (q *Queue) Dropped() uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped
}
