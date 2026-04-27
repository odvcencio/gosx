package game

// Scratch is reusable frame-local storage for hot paths that need a temporary
// slice without paying per-frame garbage-collector cost. Keep one Scratch per
// system or runtime and call Reset at the start or end of each frame.
type Scratch[T any] struct {
	values []T
}

// NewScratch creates reusable scratch storage with the requested capacity.
func NewScratch[T any](capacity int) *Scratch[T] {
	if capacity < 0 {
		capacity = 0
	}
	return &Scratch[T]{values: make([]T, 0, capacity)}
}

// Reset keeps the backing array and clears the logical length.
func (s *Scratch[T]) Reset() {
	if s == nil {
		return
	}
	s.values = s.values[:0]
}

// Slice returns a scratch slice of length n. Existing contents are discarded.
func (s *Scratch[T]) Slice(n int) []T {
	if s == nil {
		return nil
	}
	if n < 0 {
		n = 0
	}
	if cap(s.values) < n {
		s.values = make([]T, n)
		return s.values
	}
	s.values = s.values[:n]
	var zero T
	for i := range s.values {
		s.values[i] = zero
	}
	return s.values
}

// Append appends values into scratch storage and returns the current contents.
func (s *Scratch[T]) Append(values ...T) []T {
	if s == nil {
		return nil
	}
	s.values = append(s.values, values...)
	return s.values
}

// Values returns the current scratch contents.
func (s *Scratch[T]) Values() []T {
	if s == nil {
		return nil
	}
	return s.values
}
