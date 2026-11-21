package wasm

// Views publishes an engine's typed views to the page's shared signals once
// per frame, and only the ones that changed. Register each view once, change
// the values the read functions return as the game moves, and call Flush at
// the end of every frame: it sends every changed view in one SetSignals call
// and remembers what it sent only when that call succeeds, so a failed send
// is retried on the next Flush.
//
// Unlike View, Views is not paced: it suits an engine that draws its page in
// step with its frames. A view is compared in Go (==, a key or an equality
// function), so nothing is marshalled for a view that did not change.
//
// Views is not safe for concurrent use; call it from the frame loop.
type Views struct {
	set   func(map[string]any) error
	views []published
}

// published is one registered view.
type published interface {
	// pending reports the value to send, if the view changed.
	pending() (any, bool)
	// commit records that the last pending value reached the page.
	commit()
}

// NewViews publishes through the engine context's SetSignals.
func NewViews(ctx Context) *Views { return NewViewsTo(ctx.SetSignals) }

// NewViewsTo publishes through set, which receives a {"$name": value} batch.
// Tests and hosts other than the browser use it.
func NewViewsTo(set func(map[string]any) error) *Views { return &Views{set: set} }

// Flush sends the views that changed since the last successful send and
// returns how many it sent. With nothing changed it calls nothing.
func (vs *Views) Flush() (int, error) {
	var out map[string]any
	var sent []published
	for _, v := range vs.views {
		value, ok := v.pending()
		if !ok {
			continue
		}
		if out == nil {
			out = make(map[string]any, 4)
		}
		out["$"+nameOf(v)] = value
		sent = append(sent, v)
	}
	if out == nil {
		return 0, nil
	}
	if err := vs.set(out); err != nil {
		return 0, err
	}
	for _, v := range sent {
		v.commit()
	}
	return len(sent), nil
}

func nameOf(v published) string { return v.(interface{ signal() string }).signal() }

// Published is one view registered with a Views.
type Published[K, T any] struct {
	name  string
	key   func() K
	same  func(a, b K) bool
	value func(K) T
	// staleOnly views are sent only after MarkStale.
	staleOnly bool
	quiet     func(K) bool
	has       bool
	stale     bool
	sent      K
	next      K
}

func (p *Published[K, T]) signal() string { return p.name }

// MarkStale sends the view on the next Flush whether or not its key changed:
// for a view whose change the key does not show.
func (p *Published[K, T]) MarkStale() { p.stale = true }

// Quiet keeps the view unsent while quiet reports true and the view has never
// been sent: a page that never turns a layer on never receives it. Once sent,
// the view is sent on every change, quiet or not.
func (p *Published[K, T]) Quiet(quiet func(K) bool) *Published[K, T] {
	p.quiet = quiet
	return p
}

func (p *Published[K, T]) pending() (any, bool) {
	k := p.key()
	switch {
	case p.staleOnly && !p.stale:
		return nil, false
	case !p.has && p.quiet != nil && p.quiet(k):
		return nil, false
	case p.stale || !p.has || !p.same(k, p.sent):
		p.next = k
		return p.value(k), true
	}
	return nil, false
}

func (p *Published[K, T]) commit() { p.sent, p.has, p.stale = p.next, true, false }

func register[K, T any](vs *Views, p *Published[K, T]) *Published[K, T] {
	vs.views = append(vs.views, p)
	return p
}

func equal[K comparable](a, b K) bool { return a == b }

// Value publishes read's value as signal name whenever it differs (==) from
// the value last sent. The first Flush sends it.
func Value[T comparable](vs *Views, name string, read func() T) *Published[T, T] {
	return register(vs, &Published[T, T]{name: name, key: read, same: equal[T], value: func(v T) T { return v }})
}

// Equal is Value for a view that == cannot compare, such as one holding a
// slice: same reports whether two values show the same thing.
func Equal[T any](vs *Views, name string, read func() T, same func(a, b T) bool) *Published[T, T] {
	return register(vs, &Published[T, T]{name: name, key: read, same: same, value: func(v T) T { return v }})
}

// Keyed publishes value(key) as signal name whenever key differs from the key
// last sent: for a view too large to compare, keyed by a serial, or one whose
// text depends on something outside it (the device in hand, say).
func Keyed[K comparable, T any](vs *Views, name string, key func() K, value func(K) T) *Published[K, T] {
	return register(vs, &Published[K, T]{name: name, key: key, same: equal[K], value: value})
}

// Stale publishes read's value as signal name only after MarkStale: for a view
// that holds lists, which the engine marks when it changes them. The first
// Flush does not send it unless it was marked.
func Stale[T any](vs *Views, name string, read func() T) *Published[struct{}, T] {
	return register(vs, &Published[struct{}, T]{name: name, key: func() struct{} { return struct{}{} }, same: equal[struct{}],
		value: func(struct{}) T { return read() }, staleOnly: true})
}
