package checkers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	docsapp "m31labs.dev/gosx/examples/gosx-docs/app"
	checkermaterials "m31labs.dev/gosx/examples/gosx-docs/app/demos/checkers/materials"
	checkpolicy "m31labs.dev/gosx/examples/gosx-docs/app/demos/checkers/policy"
	"m31labs.dev/gosx/hub"
	"m31labs.dev/gosx/hydrate"
	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/scene"
)

const stateEvent = "checkers:state"

var Hub *hub.Hub
var liveGame = newGameSession()

type holeView struct {
	ID    int
	Label string
	Piece PieceID
	Owner int
	X     int
	Y     int
	Z     int
}
type gameSnapshot struct {
	Revision       uint64          `json:"revision"`
	MatchRevision  uint64          `json:"matchRevision"`
	Turn           uint32          `json:"turn"`
	Active         int             `json:"active"`
	Selected       int             `json:"selected"`
	Legal          []int           `json:"legal"`
	LegalHops      []int           `json:"legalHops"`
	Board          []int           `json:"board"`
	Message        string          `json:"message"`
	CanUndo        bool            `json:"canUndo"`
	Winner         int             `json:"winner"`
	Finished       bool            `json:"finished"`
	Thinking       bool            `json:"thinking"`
	Personality    string          `json:"personality"`
	Difficulty     string          `json:"difficulty"`
	PolicyLabel    string          `json:"policyLabel"`
	PolicyFallback bool            `json:"policyFallback"`
	PolicyReason   string          `json:"policyReason"`
	SearchNodes    uint64          `json:"searchNodes"`
	SearchDepth    int             `json:"searchDepth"`
	SearchMS       int64           `json:"searchMS"`
	SceneCommands  []scene.Command `json:"sceneCommands"`
}

type gameSession struct {
	mu          sync.Mutex
	match       *MatchState
	selected    Hole
	undos       []Undo
	revision    uint64
	message     string
	personality checkpolicy.Personality
	difficulty  Difficulty
	thinking    bool
	policy      checkpolicy.Resolution
	searchStats SearchStats
	generation  uint64
	cancel      context.CancelFunc
	cpuEnabled  bool
}

func newGameSession() *gameSession {
	m, _ := NewMatch()
	personality := checkpolicy.JadeCrane
	return &gameSession{match: m, selected: NoHole, revision: 1, message: "Your turn. Select a piece.", personality: personality, difficulty: Club, cpuEnabled: true, policy: checkpolicy.Resolve(context.Background(), nil, checkpolicy.Facts{Personality: personality, Phase: "opening"})}
}

func (g *gameSession) source(h Hole) gameSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.revision++
	if g.thinking {
		g.message = "The CPU is thinking."
		return g.snapshotLocked()
	}
	if int(h) >= HoleCount || g.match.Board[h] == 0 || g.match.Owner[g.match.Board[h]] != g.match.Active {
		g.selected = NoHole
		g.message = "Choose one of the active player's pieces."
		return g.snapshotLocked()
	}
	moves := GeneratePieceMoves(nil, g.match, h)
	if len(moves) == 0 {
		g.selected = NoHole
		g.message = "That piece has no legal move."
		return g.snapshotLocked()
	}
	g.selected = h
	g.message = fmt.Sprintf("Piece at hole %d selected. Choose a highlighted destination.", h)
	return g.snapshotLocked()
}

func (g *gameSession) destination(h Hole) gameSnapshot {
	g.mu.Lock()
	g.revision++
	if g.selected == NoHole {
		g.message = "Select a piece first."
		s := g.snapshotLocked()
		g.mu.Unlock()
		return s
	}
	var chosen Move
	for _, move := range GeneratePieceMoves(nil, g.match, g.selected) {
		if move.To() == h {
			chosen = move
			break
		}
	}
	if chosen.Len == 0 {
		g.message = "That destination is not legal."
		s := g.snapshotLocked()
		g.mu.Unlock()
		return s
	}
	u, err := g.match.Apply(chosen)
	if err != nil {
		g.message = err.Error()
		s := g.snapshotLocked()
		g.mu.Unlock()
		return s
	}
	g.undos = append(g.undos, u)
	g.selected = NoHole
	startCPU := g.cpuEnabled && !g.match.Outcome.Finished && g.match.Active == 3
	if g.match.Outcome.Finished {
		g.message = fmt.Sprintf("Player %d wins.", g.match.Outcome.Winner+1)
	} else if startCPU {
		g.thinking = true
		g.message = "CPU is choosing a move…"
		g.generation++
		g.cancelSearchLocked()
		sourceRevision := g.match.Revision
		generation := g.generation
		s := g.snapshotLocked()
		g.mu.Unlock()
		go g.runCPU(sourceRevision, generation)
		return s
	} else {
		g.message = fmt.Sprintf("Move %s committed. Player %d to move.", Notation(chosen), g.match.Active+1)
	}
	s := g.snapshotLocked()
	g.mu.Unlock()
	return s
}

func (g *gameSession) restart() gameSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.match, _ = NewMatch()
	g.cancelSearchLocked()
	g.generation++
	g.thinking = false
	g.selected = NoHole
	g.undos = nil
	g.revision++
	g.message = "Game restarted. Player 1 to move."
	return g.snapshotLocked()
}
func (g *gameSession) undo() gameSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.revision++
	g.cancelSearchLocked()
	g.generation++
	g.thinking = false
	if len(g.undos) == 0 {
		g.message = "Nothing to undo."
		return g.snapshotLocked()
	}
	last := len(g.undos) - 1
	g.match.Unapply(g.undos[last])
	g.undos = g.undos[:last]
	if g.match.Active == 3 && len(g.undos) > 0 {
		last = len(g.undos) - 1
		g.match.Unapply(g.undos[last])
		g.undos = g.undos[:last]
	}
	g.selected = NoHole
	g.message = fmt.Sprintf("Move undone. Player %d to move.", g.match.Active+1)
	return g.snapshotLocked()
}
func (g *gameSession) snapshot() gameSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked()
}

func (g *gameSession) snapshotLocked() gameSnapshot {
	s := gameSnapshot{Revision: g.revision, MatchRevision: g.match.Revision, Turn: g.match.Turn, Active: int(g.match.Active), Selected: -1, Message: g.message, CanUndo: len(g.undos) > 0, Winner: -1, Finished: g.match.Outcome.Finished, Board: make([]int, HoleCount), Thinking: g.thinking, Personality: string(g.personality), Difficulty: difficultyName(g.difficulty), PolicyLabel: g.policy.Policy.ExplanationLabel, PolicyFallback: g.policy.Fallback, PolicyReason: g.policy.Reason, SearchNodes: g.searchStats.Nodes, SearchDepth: g.searchStats.CompletedDepth, SearchMS: g.searchStats.Elapsed.Milliseconds()}
	if g.selected != NoHole {
		s.Selected = int(g.selected)
		seen := [HoleCount]bool{}
		hopSeen := [HoleCount]bool{}
		for _, m := range GeneratePieceMoves(nil, g.match, g.selected) {
			to := m.To()
			if !seen[to] {
				s.Legal = append(s.Legal, int(to))
				seen[to] = true
			}
			if m.Kind == Hop && !hopSeen[to] {
				s.LegalHops = append(s.LegalHops, int(to))
				hopSeen[to] = true
			}
		}
	}
	for h, p := range g.match.Board {
		if p != 0 {
			s.Board[h] = int(g.match.Owner[p]) + 1
		}
	}
	if g.match.Outcome.Finished {
		s.Winner = int(g.match.Outcome.Winner)
	}
	s.SceneCommands = visualCommands(s.Board)
	return s
}

func decodeHole(data json.RawMessage) (Hole, bool) {
	var payload struct {
		Hole int `json:"hole"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Hole < 0 || payload.Hole >= HoleCount {
		return NoHole, false
	}
	return Hole(payload.Hole), true
}
func broadcast(snapshot gameSnapshot) { Hub.Broadcast(stateEvent, snapshot) }

func (g *gameSession) cancelSearchLocked() {
	if g.cancel != nil {
		g.cancel()
		g.cancel = nil
	}
}
func (g *gameSession) settings(personality, difficulty string) gameSnapshot {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.revision++
	switch checkpolicy.Personality(personality) {
	case checkpolicy.JadeCrane, checkpolicy.IronFox, checkpolicy.CedarTurtle:
		g.personality = checkpolicy.Personality(personality)
	}
	switch difficulty {
	case "friendly":
		g.difficulty = Friendly
	case "club":
		g.difficulty = Club
	case "expert":
		g.difficulty = Expert
	}
	g.message = "CPU settings updated. The active policy is resolved at its next turn."
	return g.snapshotLocked()
}
func difficultyName(d Difficulty) string {
	switch d {
	case Friendly:
		return "friendly"
	case Expert:
		return "expert"
	default:
		return "club"
	}
}

func (g *gameSession) runCPU(sourceRevision, generation uint64) {
	g.mu.Lock()
	if g.match.Revision != sourceRevision || g.generation != generation || !g.thinking {
		g.mu.Unlock()
		return
	}
	position := g.match.Clone()
	personality, difficulty := g.personality, g.difficulty
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.mu.Unlock()
	resolution := checkpolicy.Resolve(ctx, nil, checkpolicy.Facts{Personality: personality, Phase: "midgame"})
	policy := resolution.Policy
	opts := OptionsForDifficulty(difficulty, false, time.Now())
	budget := time.Duration(policy.BudgetMS) * time.Millisecond
	if remaining := time.Until(opts.Deadline); budget > remaining {
		budget = remaining
	}
	opts.Deadline = time.Now().Add(budget)
	opts.Weights = EvalWeights{Progress: int(policy.ProgressWeight * 10), DestinationCamp: int(policy.CampSafety * 20), Mobility: int(policy.Compactness * 4), HopPotential: int(policy.HopWeight * 10), Blocking: int(policy.MobilityDenial * 10), Endgame: DefaultWeights.Endgame}
	move, stats, err := Search(ctx, position, opts)
	cancel()
	g.mu.Lock()
	if g.match.Revision != sourceRevision || g.generation != generation || !g.thinking {
		g.mu.Unlock()
		return
	}
	g.cancel = nil
	g.policy = resolution
	g.searchStats = stats
	g.thinking = false
	g.revision++
	if err != nil {
		g.message = "CPU search stopped: " + err.Error()
		snapshot := g.snapshotLocked()
		g.mu.Unlock()
		broadcast(snapshot)
		return
	}
	undo, applyErr := g.match.Apply(move)
	if applyErr != nil {
		g.message = "CPU produced no applicable move."
	} else {
		g.undos = append(g.undos, undo)
		g.message = fmt.Sprintf("CPU played %s · %s · depth %d · %d nodes.", Notation(move), policy.ExplanationLabel, stats.CompletedDepth, stats.Nodes)
	}
	snapshot := g.snapshotLocked()
	g.mu.Unlock()
	broadcast(snapshot)
}

func init() {
	Hub = hub.New("checkers")
	Hub.On("join", func(ctx *hub.Context) { ctx.Hub.Send(ctx.Client.ID, stateEvent, liveGame.snapshot()) })
	Hub.On("checkers:source", func(ctx *hub.Context) {
		if h, ok := decodeHole(ctx.Data); ok {
			broadcast(liveGame.source(h))
		}
	})
	Hub.On("checkers:destination", func(ctx *hub.Context) {
		if h, ok := decodeHole(ctx.Data); ok {
			broadcast(liveGame.destination(h))
		}
	})
	Hub.On("checkers:restart", func(ctx *hub.Context) { broadcast(liveGame.restart()) })
	Hub.On("checkers:undo", func(ctx *hub.Context) { broadcast(liveGame.undo()) })
	Hub.On("checkers:settings", func(ctx *hub.Context) {
		var p struct {
			Personality string `json:"personality"`
			Difficulty  string `json:"difficulty"`
		}
		if json.Unmarshal(ctx.Data, &p) == nil {
			broadcast(liveGame.settings(p.Personality, p.Difficulty))
		}
	})
	docsapp.RegisterStaticDocsPage("Chinese Checkers", "A playable Hub-backed Chinese Checkers match with pure-Go authoritative rules.", route.FileModuleOptions{Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
		ctx.Runtime().BindHub("checkers", "/demos/checkers/ws", []hydrate.HubBinding{})
		snapshot := liveGame.snapshot()
		material := validatedMaterial(ctx.Query("material"))
		return map[string]any{"scene": ShowcaseSceneWithMaterial(string(material)), "holes": semanticHoles(snapshot), "material": string(material)}, nil
	}})
}

func validatedMaterial(value string) checkermaterials.Family {
	material := checkermaterials.Family(value)
	switch material {
	case checkermaterials.ImperialJade, checkermaterials.CarvedWood, checkermaterials.BrushedSteel:
		return material
	default:
		return checkermaterials.CarvedWood
	}
}

func semanticHoles(snapshot gameSnapshot) []holeView {
	holes := make([]holeView, HoleCount)
	for i := range holes {
		owner := snapshot.Board[i]
		label := fmt.Sprintf("Hole %d, empty", i)
		if owner > 0 {
			label = fmt.Sprintf("Hole %d, player %d piece", i, owner)
		}
		coord := Standard.Coords[i]
		holes[i] = holeView{ID: i, Label: label, Owner: owner, X: int(coord.X), Y: int(coord.Y), Z: int(coord.Z)}
	}
	return holes
}
