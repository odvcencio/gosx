package hub

import "context"

// Close stops upgrade admission, closes accepted connections, and waits for
// reservations and both pumps to finish. Concurrent callers share completion
// and wait only until their own context expires. An expired call leaves the
// owner and observers in place until pump cleanup finishes.
func (h *Hub) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	done := h.beginClose()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SignalShutdown stops upgrades and starts the existing single socket-close
// owner without waiting for network I/O or pump completion. Applications may
// register the Hub through server.App.UseShutdownSource before Build.
func (h *Hub) SignalShutdown(context.Context) {
	if h != nil {
		h.beginClose()
	}
}

// Drain starts closing if necessary, then waits for the same owner used by
// SignalShutdown and Close. It also satisfies the server's structural shutdown
// contract, without importing server or transferring socket ownership.
func (h *Hub) Drain(ctx context.Context) error { return h.Close(ctx) }

func (h *Hub) beginClose() <-chan struct{} {
	h.mu.Lock()
	if h.closeDone == nil {
		h.closeDone = make(chan struct{})
	}
	done := h.closeDone
	first := !h.closing
	h.closing = true
	if first {
		h.closingConnections = true
	}
	h.mu.Unlock()
	if first {
		// One owner closes sockets even if a caller's deadline expires. An
		// arbitrary network Close can block; it must not block every caller.
		go h.closeConnections()
	}
	return done
}

func (h *Hub) closeConnections() {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		c.setDisconnectReason("hub_closed", "")
		if c.conn != nil {
			_ = c.conn.Close()
		}
	}
	h.mu.Lock()
	h.closingConnections = false
	finish := h.finishCloseLocked()
	h.mu.Unlock()
	if finish {
		h.finishClose()
	}
}

func (h *Hub) finishCloseLocked() bool {
	if !h.closing || h.closingConnections || h.pendingClients != 0 || h.pumps != 0 || h.finalizing {
		return false
	}
	h.finalizing = true
	return true
}

func (h *Hub) finishClose() {
	h.mu.Lock()
	list := h.observers.Swap(nil)
	h.mu.Unlock()
	// Remove the list before callbacks: even a reentrant Broadcast from a
	// Closed callback must not admit another dispatch to these subscribers.
	if list != nil {
		for _, slot := range list.slots {
			h.visitObserver(slot, func(o Observer) { o.Closed(h) })
			slot.active.Store(false)
		}
	}
	h.mu.Lock()
	close(h.closeDone)
	h.mu.Unlock()
}

func (h *Hub) pumpFinished() {
	h.mu.Lock()
	h.pumps--
	finish := h.finishCloseLocked()
	h.mu.Unlock()
	if finish {
		h.finishClose()
	}
}
