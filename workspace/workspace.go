package workspace

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/odvcencio/gosx/crdt"
	"github.com/odvcencio/gosx/hub"
	"github.com/odvcencio/gosx/vecdb"
)

// Options configures a workspace.
type Options struct {
	Name     string
	Dim      int
	BitWidth int
	Seed     int64
}

// Workspace is a distributed semantic collaboration space.
type Workspace struct {
	name          string
	dim           int
	bitWidth      int
	doc           *crdt.Doc
	idx           *vecdb.Index
	hub           *hub.Hub
	obs           *observer
	agents        map[string]AgentInfo
	clientToAgent map[string]string
	mu            sync.RWMutex
}

// AgentInfo describes a connected agent.
type AgentInfo struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

// New creates a workspace.
func New(opts Options) *Workspace {
	if opts.Dim < 2 {
		opts.Dim = 384
	}
	if opts.BitWidth < 2 {
		opts.BitWidth = 3
	}

	doc := crdt.NewDoc()
	var idx *vecdb.Index
	if opts.Seed != 0 {
		idx = vecdb.NewWithSeed(opts.Dim, opts.BitWidth, opts.Seed)
	} else {
		idx = vecdb.New(opts.Dim, opts.BitWidth)
	}

	h := hub.New(opts.Name)
	h.SyncDoc("workspace", doc)

	ws := &Workspace{
		name:          opts.Name,
		dim:           opts.Dim,
		bitWidth:      opts.BitWidth,
		doc:           doc,
		idx:           idx,
		hub:           h,
		agents:        make(map[string]AgentInfo),
		clientToAgent: make(map[string]string),
	}
	ws.obs = newObserver(doc, idx, opts.Dim)
	ws.registerEvents()
	return ws
}

// ServeHTTP delegates to the underlying Hub for WebSocket upgrades.
func (ws *Workspace) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws.hub.ServeHTTP(w, r)
}

func (ws *Workspace) registerEvents() {
	ws.hub.On("finding", func(ctx *hub.Context) {
		var msg FindingMessage
		if err := json.Unmarshal(ctx.Data, &msg); err != nil {
			return
		}
		ws.HandleWriteFinding(msg)
	})

	ws.hub.On("query", func(ctx *hub.Context) {
		var msg QueryMessage
		if err := json.Unmarshal(ctx.Data, &msg); err != nil {
			return
		}
		results, err := ws.HandleQuery(msg)
		if err != nil {
			return
		}
		ws.hub.Send(ctx.Client.ID, "query_results", results)
	})

	ws.hub.On("join", func(ctx *hub.Context) {
		var msg AgentJoinMessage
		if err := json.Unmarshal(ctx.Data, &msg); err != nil {
			return
		}
		if msg.Name != "" {
			ws.HandleAgentJoin(msg)
			ws.mu.Lock()
			if ws.clientToAgent == nil {
				ws.clientToAgent = make(map[string]string)
			}
			ws.clientToAgent[ctx.Client.ID] = msg.Name
			ws.mu.Unlock()
			ws.hub.Broadcast("agent_joined", msg)
		}
	})

	ws.hub.On("leave", func(ctx *hub.Context) {
		ws.mu.Lock()
		name, ok := ws.clientToAgent[ctx.Client.ID]
		if ok {
			delete(ws.clientToAgent, ctx.Client.ID)
		}
		ws.mu.Unlock()
		if ok {
			ws.HandleAgentLeave(name)
			ws.hub.Broadcast("agent_left", map[string]string{"name": name})
		}
	})
}

func (ws *Workspace) Name() string        { return ws.name }
func (ws *Workspace) Doc() *crdt.Doc      { return ws.doc }
func (ws *Workspace) Index() *vecdb.Index { return ws.idx }
func (ws *Workspace) Hub() *hub.Hub       { return ws.hub }

// WriteVector writes a finding vector to the shared CRDT.
func (ws *Workspace) WriteVector(id string, vec []float32) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if len(vec) != ws.dim {
		return fmt.Errorf("workspace: vector dim %d != workspace dim %d", len(vec), ws.dim)
	}

	ws.doc.Put(crdt.Root, crdt.Prop(id), crdt.VectorValue(vec, ws.dim, ws.bitWidth))
	_, err := ws.doc.Commit(fmt.Sprintf("write %s", id))
	return err
}

// WriteMeta writes metadata for a finding.
func (ws *Workspace) WriteMeta(findingID, key, value string) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	metaProp := crdt.Prop(findingID + ".__meta." + key)
	ws.doc.Put(crdt.Root, metaProp, crdt.StringValue(value))
	_, err := ws.doc.Commit(fmt.Sprintf("meta %s.%s", findingID, key))
	return err
}

// ReadMeta reads metadata for a finding.
func (ws *Workspace) ReadMeta(findingID, key string) (string, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	metaProp := crdt.Prop(findingID + ".__meta." + key)
	val, _, err := ws.doc.Get(crdt.Root, metaProp)
	if err != nil {
		return "", false
	}
	s, ok := val.ToAny().(string)
	return s, ok
}

// Query searches for vectors similar to the query.
func (ws *Workspace) Query(query []float32, k int) []vecdb.SearchResult {
	return ws.idx.Search(query, k)
}

// Save serializes the workspace CRDT to bytes.
func (ws *Workspace) Save() ([]byte, error) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.doc.Save()
}

// Load restores workspace state from persisted CRDT bytes.
func (ws *Workspace) Load(data []byte) error {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	loaded, err := crdt.Load(data)
	if err != nil {
		return fmt.Errorf("workspace: load: %w", err)
	}
	if err := ws.doc.Merge(loaded); err != nil {
		return fmt.Errorf("workspace: merge: %w", err)
	}
	return nil
}
