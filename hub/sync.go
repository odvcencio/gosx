package hub

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/odvcencio/gosx/crdt"
	crdtsync "github.com/odvcencio/gosx/crdt/sync"
)

type syncedDoc struct {
	name   string
	prefix byte
	doc    *crdt.Doc
}

type peerSyncState struct {
	mu    sync.Mutex
	state map[byte]*crdtsync.State
}

func newPeerSyncState() *peerSyncState {
	return &peerSyncState{
		state: make(map[byte]*crdtsync.State),
	}
}

func (p *peerSyncState) docState(prefix byte) *crdtsync.State {
	p.mu.Lock()
	defer p.mu.Unlock()
	if value, ok := p.state[prefix]; ok {
		return value
	}
	value := crdtsync.NewState()
	p.state[prefix] = value
	return value
}

// SyncDoc registers a CRDT document for binary sync on this hub.
func (h *Hub) SyncDoc(name string, doc *crdt.Doc) {
	if name == "" || doc == nil {
		return
	}

	h.syncMu.Lock()
	prefix, ok := h.syncDocName[name]
	if !ok {
		h.nextSyncDoc++
		prefix = h.nextSyncDoc
		h.syncDocName[name] = prefix
	}
	binding := &syncedDoc{name: name, prefix: prefix, doc: doc}
	h.syncDocs[prefix] = binding
	h.syncMu.Unlock()

	doc.OnChange(func(_ []crdt.Patch) {
		h.broadcastSyncDoc(prefix)
	})
}

func (h *Hub) hasSyncDocs() bool {
	h.syncMu.RLock()
	defer h.syncMu.RUnlock()
	return len(h.syncDocs) > 0
}

func (h *Hub) initClientSync(client *Client) {
	h.syncMu.RLock()
	docs := make([]*syncedDoc, 0, len(h.syncDocs))
	for _, binding := range h.syncDocs {
		docs = append(docs, binding)
	}
	h.syncMu.RUnlock()

	for _, binding := range docs {
		h.queueSyncMessage(client, binding)
	}
}

func (h *Hub) queueSyncMessage(client *Client, binding *syncedDoc) {
	if client == nil || binding == nil || client.syncStates == nil {
		return
	}
	state := client.syncStates.docState(binding.prefix)
	msg, ok := binding.doc.GenerateSyncMessage(state)
	if !ok {
		return
	}
	payload := append([]byte{binding.prefix}, msg...)
	select {
	case client.binarySend <- payload:
	default:
	}
}

func (h *Hub) broadcastSyncDoc(prefix byte) {
	h.syncMu.RLock()
	binding := h.syncDocs[prefix]
	h.syncMu.RUnlock()
	if binding == nil {
		return
	}

	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		h.queueSyncMessage(client, binding)
	}
}

func (h *Hub) handleBinaryMessage(client *Client, data []byte) {
	if client == nil || len(data) < 2 {
		return
	}

	prefix := data[0]
	h.syncMu.RLock()
	binding := h.syncDocs[prefix]
	h.syncMu.RUnlock()
	if binding == nil {
		h.sendCRDTError(client, prefix, fmt.Errorf("unknown crdt doc %d", prefix))
		return
	}

	state := client.syncStates.docState(prefix)
	if err := binding.doc.ReceiveSyncMessage(state, data[1:]); err != nil {
		h.sendCRDTError(client, prefix, err)
		return
	}
	h.broadcastSyncDoc(prefix)
}

func (h *Hub) sendCRDTError(client *Client, prefix byte, err error) {
	if client == nil {
		return
	}
	payload, _ := json.Marshal(Message{
		Event: "__crdt_error",
		Data: mustMarshal(map[string]any{
			"doc":   prefix,
			"error": err.Error(),
		}),
	})
	select {
	case client.send <- payload:
	default:
	}
}
