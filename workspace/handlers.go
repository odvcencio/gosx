package workspace

// FindingMessage is sent by agents to write a discovery to the workspace.
type FindingMessage struct {
	ID     string    `json:"id"`
	Vector []float32 `json:"vector"`
	Text   string    `json:"text,omitempty"`
	Agent  string    `json:"agent"`
}

// QueryMessage is sent by agents to search the semantic space.
type QueryMessage struct {
	Vector []float32 `json:"vector"`
	K      int       `json:"k"`
}

// QueryResult is returned to agents from a semantic query.
type QueryResult struct {
	ID    string  `json:"id"`
	Score float32 `json:"score"`
	Text  string  `json:"text,omitempty"`
}

// AgentJoinMessage is sent when an agent connects to the workspace.
type AgentJoinMessage struct {
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
}

// HandleWriteFinding processes a finding message from an agent.
func (ws *Workspace) HandleWriteFinding(msg FindingMessage) error {
	if err := ws.WriteVector(msg.ID, msg.Vector); err != nil {
		return err
	}
	if msg.Text != "" {
		ws.WriteMeta(msg.ID, "text", msg.Text)
	}
	if msg.Agent != "" {
		ws.WriteMeta(msg.ID, "agent", msg.Agent)
	}
	return nil
}

// HandleQuery processes a query message and returns results with metadata.
func (ws *Workspace) HandleQuery(msg QueryMessage) ([]QueryResult, error) {
	k := msg.K
	if k <= 0 {
		k = 5
	}
	results := ws.idx.Search(msg.Vector, k)
	out := make([]QueryResult, len(results))
	for i, r := range results {
		out[i] = QueryResult{
			ID:    r.ID,
			Score: r.Score,
		}
		if text, ok := ws.ReadMeta(r.ID, "text"); ok {
			out[i].Text = text
		}
	}
	return out, nil
}

// HandleAgentJoin registers an agent in the workspace.
func (ws *Workspace) HandleAgentJoin(msg AgentJoinMessage) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.agents[msg.Name] = AgentInfo{Name: msg.Name, Role: msg.Role}
}

// HandleAgentLeave removes an agent from the workspace.
func (ws *Workspace) HandleAgentLeave(name string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.agents, name)
}

// Agents returns a snapshot of all connected agents.
func (ws *Workspace) Agents() []AgentInfo {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	out := make([]AgentInfo, 0, len(ws.agents))
	for _, a := range ws.agents {
		out = append(out, a)
	}
	return out
}
