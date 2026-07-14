package checkers

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestStandardTopologyContract(t *testing.T) {
	seen := map[Cube]bool{}
	for h, c := range Standard.Coords {
		if int(c.X)+int(c.Y)+int(c.Z) != 0 {
			t.Fatalf("hole %d invalid cube %+v", h, c)
		}
		if seen[c] {
			t.Fatalf("duplicate coordinate %+v", c)
		}
		seen[c] = true
		got, ok := Standard.HoleAt(c)
		if !ok || int(got) != h {
			t.Fatalf("coordinate round trip %d -> %d/%v", h, got, ok)
		}
	}
	if len(seen) != HoleCount {
		t.Fatalf("coordinates = %d", len(seen))
	}
	for camp := 0; camp < CampCount; camp++ {
		campSeen := map[Hole]bool{}
		for _, h := range Standard.Camps[camp] {
			if campSeen[h] || Standard.Camp(h) != camp {
				t.Fatalf("camp %d invalid hole %d", camp, h)
			}
			campSeen[h] = true
		}
		if OppositeCamp(OppositeCamp(camp)) != camp {
			t.Fatalf("camp %d opposite not involutive", camp)
		}
	}
}

func TestNeighborAndJumpTablesAreSymmetric(t *testing.T) {
	for from := Hole(0); int(from) < HoleCount; from++ {
		for d := 0; d < 6; d++ {
			if to := Standard.Neighbors[from][d]; to != NoHole && Standard.Neighbors[to][(d+3)%6] != from {
				t.Fatalf("neighbor asymmetry %d/%d", from, d)
			}
			if to := Standard.Jumps[from][d]; to != NoHole {
				if Standard.Jumps[to][(d+3)%6] != from {
					t.Fatalf("jump asymmetry %d/%d", from, d)
				}
				if Standard.Middles[from][d] == NoHole {
					t.Fatalf("jump %d/%d has no middle", from, d)
				}
			}
		}
	}
}

func TestInitialMatchAndMoves(t *testing.T) {
	m, err := NewMatch()
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Seat]int{}
	for _, p := range m.Board {
		if p != 0 {
			counts[m.Owner[p]]++
		}
	}
	if counts[0] != CampSize || counts[3] != CampSize {
		t.Fatalf("piece counts: %v", counts)
	}
	moves := GenerateMoves(nil, m, m.Active)
	if len(moves) == 0 {
		t.Fatal("initial position has no moves")
	}
	for _, move := range moves {
		assertMoveValid(t, m, move)
	}
}

func TestHopChainsDeduplicateDestinationsAndDoNotRevisit(t *testing.T) {
	m := &MatchState{Ruleset: RulesetVersion, Seats: []Seat{0, 3}, Active: 0, Revision: 1}
	center, _ := Standard.HoleAt(Cube{0, 0, 0})
	m.Board[center] = 1
	m.Owner[1] = 0
	// Occupy alternating spokes to create branching and chained hops.
	for i, c := range []Cube{{1, -1, 0}, {2, -1, -1}, {0, 1, -1}, {-1, 2, -1}, {-1, 0, 1}} {
		h, _ := Standard.HoleAt(c)
		p := PieceID(i + 2)
		m.Board[h] = p
		m.Owner[p] = 3
	}
	moves := GeneratePieceMoves(nil, m, center)
	destinations := map[Hole]bool{}
	for _, move := range moves {
		if destinations[move.To()] {
			t.Fatalf("duplicate destination %d", move.To())
		}
		destinations[move.To()] = true
		visited := map[Hole]bool{move.From: true}
		for i := uint8(0); i < move.Len; i++ {
			if visited[move.Landings[i]] {
				t.Fatalf("revisited landing in %s", Notation(move))
			}
			visited[move.Landings[i]] = true
		}
		assertMoveValid(t, m, move)
	}
}

func TestDestinationCampCannotBeExited(t *testing.T) {
	m := &MatchState{Ruleset: RulesetVersion, Seats: []Seat{0, 3}, Active: 0, Revision: 1}
	h := Standard.Camps[OppositeCamp(0)][0]
	m.Board[h] = 1
	m.Owner[1] = 0
	for _, move := range GeneratePieceMoves(nil, m, h) {
		if Standard.Camp(move.To()) != OppositeCamp(0) {
			t.Fatalf("piece exited destination via %s", Notation(move))
		}
	}
}

func TestApplyUnapplyAndReplayAreDeterministic(t *testing.T) {
	m, _ := NewMatch()
	before := m.Clone()
	move := GenerateMoves(nil, m, m.Active)[0]
	notation := Notation(move)
	u, err := m.Apply(move)
	if err != nil {
		t.Fatal(err)
	}
	if m.Revision != before.Revision+1 || m.Turn != 1 || len(m.History) != 1 {
		t.Fatalf("bad transition: %+v", m)
	}
	m.Unapply(u)
	if !reflect.DeepEqual(m, before) {
		t.Fatal("apply/unapply did not restore exact state")
	}
	parsed, err := ParseNotation(notation)
	if err != nil || !EqualMove(parsed, move) {
		t.Fatalf("notation roundtrip %q: %v", notation, err)
	}
	replayed, err := Replay(before, []string{notation})
	if err != nil {
		t.Fatal(err)
	}
	expected := before.Clone()
	_, _ = expected.Apply(move)
	if !reflect.DeepEqual(replayed, expected) {
		t.Fatal("replay diverged")
	}
}

func TestPropertyLegalSequencesUndoExactly(t *testing.T) {
	m, _ := NewMatch()
	initial := m.Clone()
	undos := make([]Undo, 0, 40)
	for turn := 0; turn < 40 && !m.Outcome.Finished; turn++ {
		moves := GenerateMoves(nil, m, m.Active)
		if len(moves) == 0 {
			break
		}
		move := moves[(turn*17+3)%len(moves)]
		u, err := m.Apply(move)
		if err != nil {
			t.Fatal(err)
		}
		undos = append(undos, u)
	}
	for i := len(undos) - 1; i >= 0; i-- {
		m.Unapply(undos[i])
	}
	if !reflect.DeepEqual(m, initial) {
		t.Fatal("sequence undo did not restore initial state")
	}
}

func TestWinDetection(t *testing.T) {
	m := &MatchState{Ruleset: RulesetVersion, Seats: []Seat{0, 3}, Active: 0, Revision: 1}
	destination := OppositeCamp(0)
	var target, from Hole = NoHole, NoHole
	for _, candidate := range Standard.Camps[destination] {
		for _, n := range Standard.Neighbors[candidate] {
			if n != NoHole && Standard.Camp(n) != destination {
				target, from = candidate, n
				break
			}
		}
		if from != NoHole {
			break
		}
	}
	if from == NoHole {
		t.Fatal("destination camp has no entrance")
	}
	piece := PieceID(1)
	for _, h := range Standard.Camps[destination] {
		if h == target {
			continue
		}
		m.Board[h] = piece
		m.Owner[piece] = 0
		piece++
	}
	m.Board[from] = piece
	m.Owner[piece] = 0
	var winning Move
	for _, mv := range GeneratePieceMoves(nil, m, from) {
		if mv.To() == target {
			winning = mv
			break
		}
	}
	if winning.Len == 0 {
		t.Fatal("no winning move")
	}
	if _, err := m.Apply(winning); err != nil {
		t.Fatal(err)
	}
	if !m.Outcome.Finished || m.Outcome.Winner != 0 {
		t.Fatalf("outcome %+v", m.Outcome)
	}
}

func TestSearchFallbackCancellationAndLegality(t *testing.T) {
	m, _ := NewMatch()
	legal := GenerateMoves(nil, m, m.Active)
	expected := legal[0]
	completed, completedStats, err := Search(context.Background(), m, SearchOptions{MaxDepth: 9})
	if err != nil || completedStats.CompletedDepth != 9 || completedStats.Cancelled || completedStats.Nodes == 0 {
		t.Fatalf("completed scaffold search: %v %+v", err, completedStats)
	}
	found := false
	for _, candidate := range legal {
		if EqualMove(candidate, completed) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("search returned illegal move")
	}
	move, stats, err := Search(context.Background(), m, SearchOptions{Deadline: time.Now().Add(-time.Millisecond), MaxDepth: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !EqualMove(move, expected) || !stats.Cancelled || stats.CompletedDepth != 0 {
		t.Fatalf("fallback/stats: %s %+v", Notation(move), stats)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	move, stats, err = Search(ctx, m, SearchOptions{MaxDepth: 3})
	if err != nil || !EqualMove(move, expected) || !stats.Cancelled {
		t.Fatalf("cancelled search: %v %+v", err, stats)
	}
}

func TestInvalidInputs(t *testing.T) {
	if _, err := NewMatch(0, 0); err == nil {
		t.Fatal("duplicate seat accepted")
	}
	if _, err := ParseNotation("1>2-3"); err == nil {
		t.Fatal("mixed notation accepted")
	}
	m, _ := NewMatch()
	_, err := m.Apply(Move{From: NoHole})
	if !errors.Is(err, ErrWrongTurn) {
		t.Fatalf("error=%v", err)
	}
}

func assertMoveValid(t *testing.T, state *MatchState, move Move) {
	t.Helper()
	if move.Len == 0 || move.To() == NoHole || state.Board[move.To()] != 0 {
		t.Fatalf("bad move %s", Notation(move))
	}
	at := move.From
	for i := uint8(0); i < move.Len; i++ {
		to := move.Landings[i]
		found := false
		for d, candidate := range Standard.Jumps[at] {
			if move.Kind == Hop && candidate == to && Standard.Middles[at][d] != move.From && state.Board[Standard.Middles[at][d]] != 0 {
				found = true
				break
			}
			if move.Kind == Step && Standard.Neighbors[at][d] == to {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("invalid segment %s", Notation(move))
		}
		at = to
	}
}

func BenchmarkGenerateInitialMoves(b *testing.B) {
	m, _ := NewMatch()
	dst := make([]Move, 0, 128)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dst = GenerateMoves(dst[:0], m, m.Active)
	}
}
