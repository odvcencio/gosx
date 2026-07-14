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

// BinaryReadAuthorizer decides whether a client may receive a registered
// document during bootstrap and subsequent server-to-client broadcasts. A nil
// authorizer preserves the historical allow-all behavior.
type BinaryReadAuthorizer func(client *Client, docName string) bool

// BinaryChangeAuthorizer validates the concrete CRDT changes carried by an
// inbound sync frame before the document merges them. It is intended for
// actor binding, capability checks, and per-change audit policy that cannot be
// enforced by the document-level BinaryAuthorizer alone. Returning an error
// rejects the complete frame without mutating the document or peer sync state.
// Frames that carry only sync heads/need metadata are passed with no changes.
type BinaryChangeAuthorizer func(client *Client, docName string, changes []crdt.Change) error

// BinaryMessageHandler may consume an application-defined binary frame before
// the CRDT sync-prefix dispatcher sees it. It must return true only for frames
// it recognizes. Connection metadata remains immutable and available through
// client.Metadata, so application protocols can bind frames to authenticated
// actors without carrying identity in the payload.
type BinaryMessageHandler func(client *Client, data []byte) bool

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

func (p *peerSyncState) delete(prefix byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.state, prefix)
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

func (h *Hub) SetBinaryReadAuthorizer(fn BinaryReadAuthorizer) {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	h.binaryReadAuthorizer = fn
}

// SetBinaryChangeAuthorizer installs the pre-merge change-level sync gate.
// Passing nil restores the default behavior after any document-level gate.
func (h *Hub) SetBinaryChangeAuthorizer(fn BinaryChangeAuthorizer) {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	h.binaryChangeAuthorizer = fn
}

func (h *Hub) SetBinaryMessageHandler(fn BinaryMessageHandler) {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	h.binaryMessageHandler = fn
}

// SyncDoc registers a CRDT document for binary sync on this hub.
func (h *Hub) SyncDoc(name string, doc *crdt.Doc) {
	if name == "" || doc == nil {
		return
	}

	h.syncMu.Lock()
	prefix, ok := h.syncDocName[name]
	if !ok {
		prefix, ok = h.nextFreeSyncPrefixLocked()
		if !ok {
			h.syncMu.Unlock()
			return
		}
		h.syncDocName[name] = prefix
	}
	binding := &syncedDoc{name: name, prefix: prefix, doc: doc}
	h.syncDocs[prefix] = binding
	h.syncMu.Unlock()

	doc.OnChange(func(_ []crdt.Patch) {
		h.broadcastSyncDoc(prefix)
	})
}

func (h *Hub) nextFreeSyncPrefixLocked() (byte, bool) {
	for offset := 0; offset < 255; offset++ {
		candidate := byte((int(h.nextSyncDoc)+offset)%255 + 1)
		if _, used := h.syncDocs[candidate]; used {
			continue
		}
		h.nextSyncDoc = candidate
		return candidate, true
	}
	return 0, false
}

// UnsyncDoc removes a document from binary synchronization and releases its
// wire prefix for reuse. Existing clients also forget the prefix's peer state,
// so a later document cannot inherit stale heads or Bloom filters.
func (h *Hub) UnsyncDoc(name string) bool {
	if name == "" {
		return false
	}
	h.syncMu.Lock()
	prefix, ok := h.syncDocName[name]
	if ok {
		delete(h.syncDocName, name)
		delete(h.syncDocs, prefix)
	}
	h.syncMu.Unlock()
	if !ok {
		return false
	}
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	for _, client := range clients {
		if client.syncStates != nil {
			client.syncStates.delete(prefix)
		}
	}
	return true
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
	h.syncMu.RLock()
	authorizer := h.binaryReadAuthorizer
	h.syncMu.RUnlock()
	if authorizer != nil && !authorizer(client, binding.name) {
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
	h.syncMu.RLock()
	applicationHandler := h.binaryMessageHandler
	h.syncMu.RUnlock()
	if applicationHandler != nil && applicationHandler(client, data) {
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
