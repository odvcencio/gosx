package checkers

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHubSessionCanSelectCommitUndoAndRestart(t *testing.T) {
	g := newGameSession()
	g.cpuEnabled = false
	initial := g.snapshot()
	moves := GenerateMoves(nil, g.match, g.match.Active)
	if len(moves) == 0 {
		t.Fatal("initial match has no move")
	}
	move := moves[0]
	selected := g.source(move.From)
	if selected.Selected != int(move.From) || len(selected.Legal) == 0 || selected.Revision <= initial.Revision {
		t.Fatalf("selection snapshot: %+v", selected)
	}
	found := false
	for _, h := range selected.Legal {
		if h == int(move.To()) {
			found = true
		}
	}
	if !found {
		t.Fatalf("destination %d absent from %v", move.To(), selected.Legal)
	}
	committed := g.destination(move.To())
	if committed.Selected != -1 || committed.Turn != 1 || !committed.CanUndo || committed.Board[move.From] != 0 || committed.Board[move.To()] == 0 {
		t.Fatalf("commit snapshot: %+v", committed)
	}
	undone := g.undo()
	if undone.Turn != 0 || undone.CanUndo || undone.Board[move.From] == 0 || undone.Board[move.To()] != 0 {
		t.Fatalf("undo snapshot: %+v", undone)
	}
	g.source(move.From)
	g.destination(move.To())
	restarted := g.restart()
	if restarted.Turn != 0 || restarted.CanUndo || restarted.MatchRevision != 1 || !equalInts(restarted.Board, initial.Board) {
		t.Fatalf("restart snapshot: %+v", restarted)
	}
}

func TestHubCPUCompletesGovernedTurnAndUndoRound(t *testing.T) {
	g := newGameSession()
	move := GenerateMoves(nil, g.match, g.match.Active)[0]
	g.source(move.From)
	thinking := g.destination(move.To())
	if !thinking.Thinking || thinking.Active != 3 {
		t.Fatalf("CPU did not start: %+v", thinking)
	}
	final := waitSession(t, g, 2*time.Second, func(s gameSnapshot) bool { return !s.Thinking && s.Turn == 2 })
	if final.Active != 0 || !final.PolicyFallback || final.PolicyLabel == "" || final.SearchDepth < 1 || final.SearchNodes == 0 || !strings.Contains(final.Message, "CPU played") {
		t.Fatalf("CPU result: %+v", final)
	}
	undone := g.undo()
	if undone.Turn != 0 || undone.Active != 0 || undone.CanUndo {
		t.Fatalf("round undo: %+v", undone)
	}
}

func TestRestartCancelsStaleCPUGeneration(t *testing.T) {
	g := newGameSession()
	g.difficulty = Expert
	move := GenerateMoves(nil, g.match, g.match.Active)[0]
	g.source(move.From)
	g.destination(move.To())
	restarted := g.restart()
	if restarted.Thinking || restarted.Turn != 0 || restarted.MatchRevision != 1 {
		t.Fatalf("restart: %+v", restarted)
	}
	time.Sleep(120 * time.Millisecond)
	after := g.snapshot()
	if after.Turn != 0 || after.MatchRevision != 1 || after.Thinking {
		t.Fatalf("stale CPU committed after restart: %+v", after)
	}
}

func TestSettingsSelectBoundedCPUPosture(t *testing.T) {
	g := newGameSession()
	s := g.settings("iron-fox", "friendly")
	if s.Personality != "iron-fox" || s.Difficulty != "friendly" {
		t.Fatalf("settings: %+v", s)
	}
	invalid := g.settings("cheat", "impossible")
	if invalid.Personality != "iron-fox" || invalid.Difficulty != "friendly" {
		t.Fatalf("invalid settings escaped bounds: %+v", invalid)
	}
	grandmaster := g.settings("jade-crane", "grandmaster")
	if grandmaster.Personality != "jade-crane" || grandmaster.Difficulty != "grandmaster" {
		t.Fatalf("grandmaster settings rejected: %+v", grandmaster)
	}
}

func TestSnapshotCarriesAnimatableLastMove(t *testing.T) {
	g := newGameSession()
	g.cpuEnabled = false
	if g.snapshot().LastMove != nil {
		t.Fatal("fresh session should have no last move")
	}
	move := GenerateMoves(nil, g.match, g.match.Active)[0]
	g.source(move.From)
	committed := g.destination(move.To())
	last := committed.LastMove
	if last == nil {
		t.Fatalf("commit produced no last move: %+v", committed)
	}
	if last.ForRevision != committed.MatchRevision || last.Player != 1 || last.From != int(move.From) || last.To != int(move.To()) {
		t.Fatalf("last move identity: %+v", last)
	}
	if len(last.Path) != int(move.Len)+1 || len(last.Holes) != int(move.Len) {
		t.Fatalf("last move path shape: %+v", last)
	}
	start, end := boardHolePositions[move.From], boardHolePositions[move.To()]
	if last.Path[0].X != start.X || last.Path[0].Z != start.Z || last.Path[len(last.Path)-1].X != end.X || last.Path[len(last.Path)-1].Z != end.Z {
		t.Fatalf("last move endpoints: %+v", last.Path)
	}
	if last.Path[0].Y != pieceRestHeight {
		t.Fatalf("path rest height: %+v", last.Path[0])
	}
	if undone := g.undo(); undone.LastMove != nil {
		t.Fatalf("undo should clear last move: %+v", undone.LastMove)
	}
	g.source(move.From)
	g.destination(move.To())
	if restarted := g.restart(); restarted.LastMove != nil {
		t.Fatalf("restart should clear last move: %+v", restarted.LastMove)
	}
}

func TestCPUCommitPublishesLastMoveForItsSeat(t *testing.T) {
	g := newGameSession()
	move := GenerateMoves(nil, g.match, g.match.Active)[0]
	g.source(move.From)
	g.destination(move.To())
	final := waitSession(t, g, 2*time.Second, func(s gameSnapshot) bool { return !s.Thinking && s.Turn == 2 })
	if final.LastMove == nil || final.LastMove.Player != 4 || final.LastMove.ForRevision != final.MatchRevision {
		t.Fatalf("CPU last move: %+v", final.LastMove)
	}
	if len(final.LastMove.Path) < 2 {
		t.Fatalf("CPU path too short: %+v", final.LastMove.Path)
	}
}

func waitSession(t *testing.T, g *gameSession, timeout time.Duration, predicate func(gameSnapshot) bool) gameSnapshot {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := g.snapshot()
		if predicate(s) {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for session")
	return gameSnapshot{}
}

func TestHubSessionRejectsInvalidIntentWithoutMutation(t *testing.T) {
	g := newGameSession()
	before := g.snapshot()
	after := g.destination(0)
	if after.MatchRevision != before.MatchRevision || after.Turn != before.Turn || !strings.Contains(after.Message, "Select a piece") {
		t.Fatalf("invalid destination mutated match: %+v", after)
	}
	after = g.source(NoHole)
	if after.Selected != -1 || after.MatchRevision != before.MatchRevision {
		t.Fatalf("invalid source: %+v", after)
	}
}

func TestHubSnapshotIdentifiesMultiHopDestinations(t *testing.T) {
	g := newGameSession()
	moves := GenerateMoves(nil, g.match, g.match.Active)
	var hop Move
	for _, move := range moves {
		if move.Kind == Hop {
			hop = move
			break
		}
	}
	if hop.Len == 0 {
		t.Fatal("initial fixture has no hop")
	}
	s := g.source(hop.From)
	found := false
	for _, h := range s.LegalHops {
		if h == int(hop.To()) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("hop %s absent from %v", Notation(hop), s.LegalHops)
	}
}

func TestSnapshotWireAndSemanticHoles(t *testing.T) {
	s := newGameSession().snapshot()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, field := range []string{`"revision"`, `"matchRevision"`, `"selected"`, `"legal"`, `"legalHops"`, `"board"`, `"canUndo"`, `"sceneCommands"`} {
		if !strings.Contains(text, field) {
			t.Errorf("snapshot JSON missing %s", field)
		}
	}
	holes := semanticHoles(s)
	if len(holes) != HoleCount {
		t.Fatalf("holes=%d", len(holes))
	}
	occupied := 0
	for _, h := range holes {
		if h.Owner > 0 {
			occupied++
		}
		if h.Label == "" {
			t.Fatalf("hole %d unnamed", h.ID)
		}
	}
	if occupied != 20 {
		t.Fatalf("occupied=%d want 20", occupied)
	}
}

func TestVisualCommandsFollowCommittedBoard(t *testing.T) {
	g := newGameSession()
	g.cpuEnabled = false
	move := GenerateMoves(nil, g.match, g.match.Active)[0]
	before := g.snapshot()
	g.source(move.From)
	after := g.destination(move.To())
	if len(before.SceneCommands) != 1 || len(after.SceneCommands) != 1 {
		t.Fatalf("visual batches before=%d after=%d", len(before.SceneCommands), len(after.SceneCommands))
	}
	b, _ := json.Marshal(before.SceneCommands)
	a, _ := json.Marshal(after.SceneCommands)
	if string(a) == string(b) {
		t.Fatal("committed move did not change renderer command payload")
	}
	// The client move tween patches raw column-major transforms in place, so
	// the wire payload must keep uncompressed per-instance transforms.
	if !strings.Contains(string(a), `"transforms"`) {
		t.Fatal("scene commands lost raw instance transforms; client animation would degrade to teleporting")
	}
}

func TestDecodeHole(t *testing.T) {
	if h, ok := decodeHole([]byte(`{"hole":42}`)); !ok || h != 42 {
		t.Fatalf("decode=%d/%v", h, ok)
	}
	for _, raw := range []string{`{"hole":-1}`, `{"hole":121}`, `nope`} {
		if _, ok := decodeHole([]byte(raw)); ok {
			t.Fatalf("accepted %q", raw)
		}
	}
}
func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
