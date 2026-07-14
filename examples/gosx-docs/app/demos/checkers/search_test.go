package checkers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDifficultyBudgetsAndBounds(t *testing.T) {
	now := time.Unix(100, 0)
	cases := []struct {
		level  Difficulty
		mobile bool
		budget time.Duration
		depth  int
	}{
		{Friendly, false, 35 * time.Millisecond, 3}, {Club, false, 120 * time.Millisecond, 5},
		{Expert, false, 350 * time.Millisecond, 8}, {Expert, true, 180 * time.Millisecond, 8},
		{Grandmaster, false, 1000 * time.Millisecond, 11}, {Grandmaster, true, 500 * time.Millisecond, 11},
	}
	for _, tc := range cases {
		got := OptionsForDifficulty(tc.level, tc.mobile, now)
		if got.Deadline.Sub(now) != tc.budget || got.MaxDepth != tc.depth || got.TableEntries <= 0 {
			t.Fatalf("options %+v", got)
		}
	}
	if OptionsForDifficulty(Grandmaster, false, now).TableEntries <= OptionsForDifficulty(Club, false, now).TableEntries {
		t.Fatal("grandmaster should search with a larger transposition table")
	}
	scales := map[Difficulty]float64{Friendly: 0.5, Club: 1, Expert: 3, Grandmaster: 8}
	for level, want := range scales {
		if got := BudgetScale(level); got != want {
			t.Fatalf("BudgetScale(%d)=%v want %v", level, got, want)
		}
	}
	w := (EvalWeights{Progress: 99999, DestinationCamp: -99999, Endgame: 1}).bounded()
	if w.Progress != 1000 || w.DestinationCamp != -1000 || w.Endgame != 10000 {
		t.Fatalf("unbounded weights %+v", w)
	}
}

func TestAlphaBetaBestMoveCuratedProgress(t *testing.T) {
	m := singlePiecePosition(t, Cube{0, 0, 0})
	moves := GenerateMoves(nil, m, m.Active)
	if len(moves) < 2 {
		t.Fatal("fixture needs choices")
	}
	weights := EvalWeights{Progress: 100, DestinationCamp: 1, Endgame: 100000}
	move, stats, err := Search(context.Background(), m, SearchOptions{MaxDepth: 1, Weights: weights, TableEntries: 256})
	if err != nil {
		t.Fatal(err)
	}
	bestDistance := 99
	for _, candidate := range moves {
		d := distanceToCamp(candidate.To(), OppositeCamp(0))
		if d < bestDistance {
			bestDistance = d
		}
	}
	if got := distanceToCamp(move.To(), OppositeCamp(0)); got != bestDistance {
		t.Fatalf("move %s distance=%d want=%d", Notation(move), got, bestDistance)
	}
	if stats.CompletedDepth != 1 || stats.Nodes != uint64(len(moves)) {
		t.Fatalf("stats %+v moves=%d", stats, len(moves))
	}
}

func TestPersonalityWeightsAreExternalAndDeterministic(t *testing.T) {
	m, _ := NewMatch()
	profiles := []EvalWeights{
		{Progress: 20, DestinationCamp: 40, Mobility: 1, HopPotential: 8, Blocking: 0, Endgame: 100000},
		{Progress: 5, DestinationCamp: 10, Mobility: 2, HopPotential: 1, Blocking: 30, Endgame: 100000},
	}
	for i, w := range profiles {
		a, _, err := Search(context.Background(), m, SearchOptions{MaxDepth: 3, Weights: w, TableEntries: 1024})
		if err != nil {
			t.Fatal(err)
		}
		b, _, _ := Search(context.Background(), m, SearchOptions{MaxDepth: 3, Weights: w, TableEntries: 1024})
		if !EqualMove(a, b) {
			t.Fatalf("profile %d nondeterministic: %s / %s", i, Notation(a), Notation(b))
		}
		assertLegalSearchMove(t, m, a)
	}
}

func TestAlphaBetaUsesCutoffsAndTranspositionTable(t *testing.T) {
	m, _ := NewMatch()
	move, stats, err := Search(context.Background(), m, SearchOptions{MaxDepth: 6, TableEntries: 1 << 14})
	if err != nil {
		t.Fatal(err)
	}
	assertLegalSearchMove(t, m, move)
	if stats.CompletedDepth != 6 || stats.Nodes == 0 || stats.Cutoffs == 0 || stats.CacheHits == 0 {
		t.Fatalf("expected search instrumentation, got %+v", stats)
	}
}

func TestSearchDeadlineAndCancellationNeverReturnIllegal(t *testing.T) {
	m, _ := NewMatch()
	legalFallback := GenerateMoves(nil, m, m.Active)[0]
	move, stats, err := Search(context.Background(), m, SearchOptions{MaxDepth: 12, Deadline: time.Now()})
	if err != nil || !EqualMove(move, legalFallback) || !stats.Cancelled {
		t.Fatalf("expired: %v %s %+v", err, Notation(move), stats)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	move, stats, err = Search(ctx, m, SearchOptions{MaxDepth: 12})
	if err != nil || !EqualMove(move, legalFallback) || !stats.Cancelled {
		t.Fatalf("cancel: %v %s %+v", err, Notation(move), stats)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Millisecond)
	defer cancel()
	move, stats, err = Search(ctx, m, SearchOptions{MaxDepth: 12, TableEntries: 4096})
	if err != nil {
		t.Fatal(err)
	}
	assertLegalSearchMove(t, m, move)
	if !stats.Cancelled {
		t.Fatalf("short search unexpectedly completed %+v", stats)
	}
}

func TestSearchRejectsNonTwoPlayerPosition(t *testing.T) {
	m, _ := NewMatch(0, 2, 4)
	_, _, err := Search(context.Background(), m, SearchOptions{})
	if !errors.Is(err, ErrSearchPlayers) {
		t.Fatalf("err=%v", err)
	}
}

func TestSearchRejectsFinishedMatch(t *testing.T) {
	m, _ := NewMatch()
	m.Outcome = Outcome{Winner: 0, Finished: true}
	_, _, err := Search(context.Background(), m, SearchOptions{})
	if !errors.Is(err, ErrGameOver) {
		t.Fatalf("err=%v", err)
	}
}

func TestLegalMoveGenerationAllocationGate(t *testing.T) {
	m, _ := NewMatch()
	dst := make([]Move, 0, 128)
	allocs := testing.AllocsPerRun(1000, func() { dst = GenerateMoves(dst[:0], m, m.Active) })
	if allocs != 0 {
		t.Fatalf("legal hot path allocations=%g", allocs)
	}
}

func BenchmarkAlphaBetaClub(b *testing.B) {
	m, _ := NewMatch()
	opts := SearchOptions{MaxDepth: 5, TableEntries: 1 << 14}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _, _ = Search(context.Background(), m, opts)
	}
}

func singlePiecePosition(t *testing.T, c Cube) *MatchState {
	t.Helper()
	h, ok := Standard.HoleAt(c)
	if !ok {
		t.Fatal("fixture coordinate missing")
	}
	m := &MatchState{Ruleset: RulesetVersion, Seats: []Seat{0, 3}, Active: 0, Revision: 1}
	m.Board[h] = 1
	m.Owner[1] = 0
	return m
}
func distanceToCamp(h Hole, camp int) int {
	best := 99
	for _, target := range Standard.Camps[camp] {
		if d := cubeDistance(Standard.Coords[h], Standard.Coords[target]); d < best {
			best = d
		}
	}
	return best
}
func assertLegalSearchMove(t *testing.T, m *MatchState, move Move) {
	t.Helper()
	for _, candidate := range GenerateMoves(nil, m, m.Active) {
		if EqualMove(candidate, move) {
			return
		}
	}
	t.Fatalf("illegal search result %s", Notation(move))
}
