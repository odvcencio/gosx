package audiohost

// Voice owns a one-use source and the processing nodes dedicated to it. Shared
// mixer buses and effects sends must not be included in nodes. Close releases
// nodes and callbacks immediately even when the context is suspended and the
// browser cannot dispatch an ended event.
type Voice struct {
	source ScheduledSource
	nodes  []Node
	ended  func()
	closed bool
}

// NewVoice installs cleanup before the caller starts its source. onEnded is
// invoked once after cleanup, including after an explicit Close.
func NewVoice(source ScheduledSource, onEnded func(), nodes ...Node) *Voice {
	v := &Voice{source: source, nodes: nodes, ended: onEnded}
	source.OnEnded(v.Close)
	return v
}

// Stop schedules the source end without destroying an envelope or its tail.
func (v *Voice) Stop(at float64) {
	if v != nil && !v.closed {
		v.source.Stop(at)
	}
}

// Close detaches and disconnects the source and each dedicated node once.
// To end playback earlier than its scheduled stop, call Stop before Close.
func (v *Voice) Close() {
	if v == nil || v.closed {
		return
	}
	v.closed = true
	v.source.OnEnded(nil)
	v.source.Disconnect()
	for _, node := range v.nodes {
		node.Disconnect()
	}
	v.nodes = nil
	if fn := v.ended; fn != nil {
		v.ended = nil
		fn()
	}
}
