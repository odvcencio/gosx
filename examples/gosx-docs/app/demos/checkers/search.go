package checkers

import (
	"context"
	"errors"
	"time"
)

type Difficulty uint8

const (
	Friendly Difficulty = iota
	Club
	Expert
)

type EvalWeights struct {
	Progress, DestinationCamp, Mobility, HopPotential, Blocking, Endgame int
}

var DefaultWeights = EvalWeights{Progress: 12, DestinationCamp: 28, Mobility: 2, HopPotential: 4, Blocking: 1, Endgame: 100000}

type SearchOptions struct {
	Deadline     time.Time
	MaxDepth     int
	TableEntries int
	Weights      EvalWeights
}

func OptionsForDifficulty(level Difficulty, mobile bool, now time.Time) SearchOptions {
	budget, depth := 35*time.Millisecond, 3
	switch level {
	case Club:
		budget, depth = 120*time.Millisecond, 5
	case Expert:
		budget, depth = 350*time.Millisecond, 8
		if mobile {
			budget = 180 * time.Millisecond
		}
	}
	return SearchOptions{Deadline: now.Add(budget), MaxDepth: depth, TableEntries: 1 << 15, Weights: DefaultWeights}
}

type SearchStats struct {
	Nodes, Cutoffs, CacheHits uint64
	CompletedDepth            int
	Elapsed                   time.Duration
	Cancelled                 bool
}

var ErrNoLegalMove = errors.New("checkers: no legal move")
var ErrSearchPlayers = errors.New("checkers: alpha-beta search requires two seats")

const (
	negativeInfinity = -1 << 29
	positiveInfinity = 1 << 29
	maxSearchDepth   = 12
)

type ttBound uint8

const (
	ttExact ttBound = iota
	ttLower
	ttUpper
)

type ttEntry struct {
	key              uint64
	value            int
	depth            int8
	bound            ttBound
	bestFrom, bestTo Hole
}

type searcher struct {
	ctx       context.Context
	deadline  time.Time
	root      Seat
	weights   EvalWeights
	table     []ttEntry
	stats     SearchStats
	cancelled bool
	moves     [maxSearchDepth + 1][]Move
}

// Search performs deterministic iterative-deepening alpha-beta for a
// two-player position. It establishes a legal fallback before checking the
// deadline and publishes only fully completed depths.
func Search(ctx context.Context, state *MatchState, opts SearchOptions) (Move, SearchStats, error) {
	started := time.Now()
	if state == nil || len(state.Seats) != 2 {
		return Move{}, SearchStats{Elapsed: time.Since(started)}, ErrSearchPlayers
	}
	if state.Outcome.Finished {
		return Move{}, SearchStats{Elapsed: time.Since(started)}, ErrGameOver
	}
	rootMoves := GenerateMoves(nil, state, state.Active)
	if len(rootMoves) == 0 {
		return Move{}, SearchStats{Elapsed: time.Since(started)}, ErrNoLegalMove
	}
	best := rootMoves[0]
	depthLimit := opts.MaxDepth
	if depthLimit <= 0 {
		depthLimit = 1
	}
	if depthLimit > maxSearchDepth {
		depthLimit = maxSearchDepth
	}
	weights := opts.Weights
	if weights == (EvalWeights{}) {
		weights = DefaultWeights
	}
	weights = weights.bounded()
	tableSize := opts.TableEntries
	if tableSize <= 0 {
		tableSize = 1 << 14
	}
	tableSize = nextPowerOfTwo(tableSize)
	if tableSize > 1<<20 {
		tableSize = 1 << 20
	}
	s := &searcher{ctx: ctx, deadline: opts.Deadline, root: state.Active, weights: weights, table: make([]ttEntry, tableSize)}
	for ply := range s.moves {
		s.moves[ply] = make([]Move, 0, 192)
	}
	work := state.Clone()
	work.History = nil
	for depth := 1; depth <= depthLimit; depth++ {
		s.cancelled = false
		value, candidate := s.rootSearch(work, depth, best)
		_ = value
		if s.cancelled {
			s.stats.Cancelled = true
			break
		}
		best = candidate
		s.stats.CompletedDepth = depth
	}
	s.stats.Elapsed = time.Since(started)
	return best, s.stats, nil
}

func (s *searcher) rootSearch(state *MatchState, depth int, previous Move) (int, Move) {
	moves := GenerateMoves(s.moves[0][:0], state, state.Active)
	orderMoves(moves, state, state.Active, previous.From, previous.To())
	best, bestValue := moves[0], negativeInfinity
	alpha := negativeInfinity
	for _, move := range moves {
		if s.shouldStop() {
			return bestValue, best
		}
		u := applySearch(state, move)
		s.stats.Nodes++
		value := s.alphaBeta(state, depth-1, alpha, positiveInfinity, 1)
		unapplySearch(state, u)
		if s.cancelled {
			return bestValue, best
		}
		if value > bestValue {
			best, bestValue = move, value
		}
		if value > alpha {
			alpha = value
		}
	}
	return bestValue, best
}

func (s *searcher) alphaBeta(state *MatchState, depth, alpha, beta, ply int) int {
	if s.shouldStop() {
		return 0
	}
	if state.Outcome.Finished || depth == 0 {
		return s.evaluate(state)
	}
	key := hashState(state)
	originalAlpha, originalBeta := alpha, beta
	if entry := s.probe(key); entry != nil && int(entry.depth) >= depth {
		s.stats.CacheHits++
		switch entry.bound {
		case ttExact:
			return entry.value
		case ttLower:
			if entry.value > alpha {
				alpha = entry.value
			}
		case ttUpper:
			if entry.value < beta {
				beta = entry.value
			}
		}
		if alpha >= beta {
			return entry.value
		}
	}
	moves := GenerateMoves(s.moves[ply][:0], state, state.Active)
	if len(moves) == 0 {
		return s.evaluate(state)
	}
	hintFrom, hintTo := NoHole, NoHole
	if entry := s.probe(key); entry != nil {
		hintFrom, hintTo = entry.bestFrom, entry.bestTo
	}
	orderMoves(moves, state, state.Active, hintFrom, hintTo)
	maximizing := state.Active == s.root
	bestValue, best := positiveInfinity, moves[0]
	if maximizing {
		bestValue = negativeInfinity
	}
	for _, move := range moves {
		u := applySearch(state, move)
		s.stats.Nodes++
		value := s.alphaBeta(state, depth-1, alpha, beta, ply+1)
		unapplySearch(state, u)
		if s.cancelled {
			return 0
		}
		if maximizing {
			if value > bestValue {
				bestValue, best = value, move
			}
			if value > alpha {
				alpha = value
			}
		} else {
			if value < bestValue {
				bestValue, best = value, move
			}
			if value < beta {
				beta = value
			}
		}
		if alpha >= beta {
			s.stats.Cutoffs++
			break
		}
	}
	bound := ttExact
	if bestValue <= originalAlpha {
		bound = ttUpper
	} else if bestValue >= originalBeta {
		bound = ttLower
	}
	s.store(key, depth, bestValue, bound, best)
	return bestValue
}

func (s *searcher) shouldStop() bool {
	if s.cancelled {
		return true
	}
	if s.stats.Nodes&63 != 0 {
		return false
	}
	select {
	case <-s.ctx.Done():
		s.cancelled = true
		return true
	default:
	}
	if !s.deadline.IsZero() && !time.Now().Before(s.deadline) {
		s.cancelled = true
		return true
	}
	return false
}

func (s *searcher) evaluate(state *MatchState) int {
	if state.Outcome.Finished {
		if state.Outcome.Winner == s.root {
			return s.weights.Endgame
		}
		return -s.weights.Endgame
	}
	return evaluateSeat(state, s.root, s.weights) - evaluateSeat(state, state.nextSeat(s.root), s.weights)
}

func evaluateSeat(state *MatchState, seat Seat, w EvalWeights) int {
	dest := OppositeCamp(int(seat))
	progress, camp, mobility, hops, blocking := 0, 0, 0, 0, 0
	for h, p := range state.Board {
		if p == 0 || state.Owner[p] != seat {
			continue
		}
		hole := Hole(h)
		minDist := 99
		for _, target := range Standard.Camps[dest] {
			if d := cubeDistance(Standard.Coords[hole], Standard.Coords[target]); d < minDist {
				minDist = d
			}
		}
		progress -= minDist
		if Standard.Camp(hole) == dest {
			camp++
		}
		for d, n := range Standard.Neighbors[hole] {
			if n != NoHole && state.Board[n] == 0 {
				mobility++
			}
			j := Standard.Jumps[hole][d]
			if n != NoHole && j != NoHole && state.Board[n] != 0 && state.Board[j] == 0 {
				hops++
			}
		}
	}
	opponent := state.nextSeat(seat)
	for h, p := range state.Board {
		if p == 0 || state.Owner[p] != opponent {
			continue
		}
		for _, n := range Standard.Neighbors[h] {
			if n != NoHole && state.Board[n] != 0 && state.Owner[state.Board[n]] == seat {
				blocking++
			}
		}
	}
	return progress*w.Progress + camp*w.DestinationCamp + mobility*w.Mobility + hops*w.HopPotential + blocking*w.Blocking
}

type searchUndo struct {
	move    Move
	piece   PieceID
	active  Seat
	outcome Outcome
}

func applySearch(state *MatchState, move Move) searchUndo {
	u := searchUndo{move: move, piece: state.Board[move.From], active: state.Active, outcome: state.Outcome}
	state.Board[move.From] = 0
	state.Board[move.To()] = u.piece
	if state.hasWon(state.Active) {
		state.Outcome = Outcome{Winner: state.Active, Finished: true}
	} else {
		state.Active = state.nextSeat(state.Active)
	}
	return u
}
func unapplySearch(state *MatchState, u searchUndo) {
	state.Board[u.move.To()] = 0
	state.Board[u.move.From] = u.piece
	state.Active = u.active
	state.Outcome = u.outcome
}

func orderMoves(moves []Move, state *MatchState, seat Seat, hintFrom, hintTo Hole) {
	for i := 1; i < len(moves); i++ {
		m := moves[i]
		score := moveOrderScore(m, state, seat, hintFrom, hintTo)
		j := i
		for j > 0 && score > moveOrderScore(moves[j-1], state, seat, hintFrom, hintTo) {
			moves[j] = moves[j-1]
			j--
		}
		moves[j] = m
	}
}
func moveOrderScore(m Move, state *MatchState, seat Seat, hintFrom, hintTo Hole) int {
	if hintFrom != NoHole && m.From == hintFrom && m.To() == hintTo {
		return 1 << 20
	}
	score := int(m.Len) * 16
	if Standard.Camp(m.To()) == OppositeCamp(int(seat)) {
		score += 512
	}
	from, to := Standard.Coords[m.From], Standard.Coords[m.To()]
	target := Standard.Coords[Standard.Camps[OppositeCamp(int(seat))][CampSize-1]]
	score += (cubeDistance(from, target) - cubeDistance(to, target)) * 32
	return score
}

func hashState(state *MatchState) uint64 {
	h := uint64(1469598103934665603)
	for i, p := range state.Board {
		if p != 0 {
			h ^= uint64(p) + uint64(i+1)*0x9e3779b97f4a7c15
			h *= 1099511628211
		}
	}
	h ^= uint64(state.Active) + 0x517cc1b727220a95
	return h
}
func (s *searcher) probe(key uint64) *ttEntry {
	e := &s.table[key&uint64(len(s.table)-1)]
	if e.key == key {
		return e
	}
	return nil
}
func (s *searcher) store(key uint64, depth, value int, bound ttBound, best Move) {
	slot := &s.table[key&uint64(len(s.table)-1)]
	if slot.key == 0 || int(slot.depth) <= depth {
		*slot = ttEntry{key: key, value: value, depth: int8(depth), bound: bound, bestFrom: best.From, bestTo: best.To()}
	}
}
func (w EvalWeights) bounded() EvalWeights {
	w.Progress = clamp(w.Progress, -1000, 1000)
	w.DestinationCamp = clamp(w.DestinationCamp, -1000, 1000)
	w.Mobility = clamp(w.Mobility, -1000, 1000)
	w.HopPotential = clamp(w.HopPotential, -1000, 1000)
	w.Blocking = clamp(w.Blocking, -1000, 1000)
	w.Endgame = clamp(w.Endgame, 10000, 1000000)
	return w
}
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
func nextPowerOfTwo(v int) int {
	n := 1
	for n < v {
		n <<= 1
	}
	return n
}
func cubeDistance(a, b Cube) int {
	return (abs(int(a.X-b.X)) + abs(int(a.Y-b.Y)) + abs(int(a.Z-b.Z))) / 2
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
