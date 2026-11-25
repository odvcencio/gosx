package actions

// DefaultQueueLimit is how many commands a Queue holds when its Limit is 0.
const DefaultQueueLimit = 4

// Queue holds the commands of a fixed-step simulation whose step takes one
// command at a time. The page draws about twice as many frames as the
// simulation steps, and a step's input is read once (Ticks), so two presses
// can arrive before one step: a pump fake and a throw away a moment apart.
// The second must not overwrite the first. Push keeps them in the order they
// came; each step Takes the first, and the others wait for the steps after it.
//
// Pushing a command that already waits does nothing, so pressing the same
// thing twice before a step takes it is one press. The zero value of T is
// "no command" and is never queued. The zero Queue is ready to use. It is not
// safe for concurrent use.
type Queue[T comparable] struct {
	// Limit is how many commands wait at most, the one the next step takes
	// included; a push beyond it is dropped. Zero selects DefaultQueueLimit.
	Limit int
	items []T
}

func (q *Queue[T]) limit() int {
	if q.Limit > 0 {
		return q.Limit
	}
	return DefaultQueueLimit
}

// Push adds v at the end. It reports whether v was added: false for the zero
// value, for a command that already waits and when the queue is full.
func (q *Queue[T]) Push(v T) bool {
	var zero T
	if v == zero || len(q.items) >= q.limit() {
		return false
	}
	for _, w := range q.items {
		if w == v {
			return false
		}
	}
	q.items = append(q.items, v)
	return true
}

// Take removes and returns the first command, for this step. ok is false,
// and v the zero value, when nothing waits.
func (q *Queue[T]) Take() (v T, ok bool) {
	if len(q.items) == 0 {
		return v, false
	}
	v = q.items[0]
	q.items = append(q.items[:0], q.items[1:]...)
	return v, true
}

// Next is the command the next Take returns, without removing it.
func (q *Queue[T]) Next() (v T, ok bool) {
	if len(q.items) == 0 {
		return v, false
	}
	return q.items[0], true
}

// Len is how many commands wait.
func (q *Queue[T]) Len() int { return len(q.items) }

// Clear drops every waiting command: at a snap, a whistle or a dialog, so a
// press made before it does not act after it.
func (q *Queue[T]) Clear() { q.items = q.items[:0] }
