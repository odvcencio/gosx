package hub

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"m31labs.dev/gosx/crdt"
	crdtsync "m31labs.dev/gosx/crdt/sync"
)

type syncedDoc struct {
	name   string
	prefix byte
	doc    *crdt.Doc
}

// BinaryAuthorizer decides whether an inbound binary CRDT sync frame
// (a client -> server ReceiveSyncMessage) from client for the SyncDoc
// registered as docName should be applied. Return false to drop the frame
// instead of merging it into the document.
//
// This gate covers INBOUND application only. It has no effect on
// broadcastSyncDoc — server -> client sync fan-out (including the initial
// bootstrap frame queued by initClientSync) is never gated, so read-only
// clients keep receiving live convergent state; only their own writes can be
// refused.
//
// If no authorizer is installed (the default), every joined client may push
// inbound sync for every registered SyncDoc, matching the hub's behavior
// before this hook existed. Install one with Hub.SetBinaryAuthorizer for any
// SyncDoc whose server-side application must be restricted per client (e.g.
// a per-connection read/write permission resolved at WebSocket upgrade
// time — see m31labs.dev/kiln/collab.Session's CanWrite wiring).
type BinaryAuthorizer func(client *Client, docName string) bool

// BinaryChangeAuthorizer validates the concrete CRDT changes carried by an
// inbound sync frame before the document merges them. It is intended for
// actor binding, capability checks, and per-change audit policy that cannot be
// enforced by the document-level BinaryAuthorizer alone. Returning an error
// rejects the complete frame without mutating the document or peer sync state.
// Frames that carry only sync heads/need metadata are passed with no changes.
type BinaryChangeAuthorizer func(client *Client, docName string, changes []crdt.Change) error

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

// SetBinaryAuthorizer installs fn as the hub's inbound binary sync gate (see
// BinaryAuthorizer). Passing nil removes any previously installed authorizer,
// reverting to allow-all for inbound sync. Safe to call at any time,
// including before any SyncDoc registration or client connection.
func (h *Hub) SetBinaryAuthorizer(fn BinaryAuthorizer) {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	h.binaryAuthorizer = fn
}

// SetBinaryChangeAuthorizer installs the pre-merge change-level sync gate.
// Passing nil restores the default behavior after any document-level gate.
func (h *Hub) SetBinaryChangeAuthorizer(fn BinaryChangeAuthorizer) {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	h.binaryChangeAuthorizer = fn
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
	client.tryBinarySend(payload)
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
	authorizer := h.binaryAuthorizer
	changeAuthorizer := h.binaryChangeAuthorizer
	h.syncMu.RUnlock()
	if binding == nil {
		h.sendCRDTError(client, prefix, fmt.Errorf("unknown crdt doc %d", prefix))
		return
	}
	if authorizer != nil && !authorizer(client, binding.name) {
		log.Printf("[hub/%s] dropped unauthorized inbound sync for doc %q from client %s", h.name, binding.name, client.ID)
		h.sendCRDTError(client, prefix, fmt.Errorf("not authorized to sync doc %q", binding.name))
		return
	}
	if changeAuthorizer != nil {
		changes, err := decodeSyncChanges(data[1:])
		if err != nil {
			h.sendCRDTError(client, prefix, err)
			return
		}
		if err := changeAuthorizer(client, binding.name, changes); err != nil {
			log.Printf("[hub/%s] dropped unauthorized CRDT changes for doc %q from client %s: %v", h.name, binding.name, client.ID, err)
			h.sendCRDTError(client, prefix, err)
			return
		}
	}

	state := client.syncStates.docState(prefix)
	if err := binding.doc.ReceiveSyncMessage(state, data[1:]); err != nil {
		h.sendCRDTError(client, prefix, err)
		return
	}
	h.broadcastSyncDoc(prefix)
}

func decodeSyncChanges(data []byte) ([]crdt.Change, error) {
	message, err := crdtsync.DecodeMessage(data)
	if err != nil {
		return nil, fmt.Errorf("decode sync frame for authorization: %w", err)
	}
	changes := make([]crdt.Change, 0, len(message.Changes))
	for _, chunk := range message.Changes {
		change, err := crdt.DecodeChangeChunk(chunk)
		if err != nil {
			return nil, fmt.Errorf("decode sync change for authorization: %w", err)
		}
		changes = append(changes, change)
	}
	return changes, nil
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
	client.trySend(payload)
}
