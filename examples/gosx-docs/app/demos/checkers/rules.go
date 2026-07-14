package checkers

import "errors"

const MaxLandings = HoleCount - 1

type PieceID uint8
type Seat uint8
type MoveKind uint8

const (
	Step MoveKind = iota + 1
	Hop
)

type Move struct {
	From     Hole
	Landings [MaxLandings]Hole
	Len      uint8
	Kind     MoveKind
}

func (m Move) To() Hole {
	if m.Len == 0 {
		return NoHole
	}
	return m.Landings[m.Len-1]
}

var (
	ErrIllegalMove = errors.New("checkers: illegal move")
	ErrWrongTurn   = errors.New("checkers: piece does not belong to active seat")
	ErrGameOver    = errors.New("checkers: game is over")
)

// GeneratePieceMoves appends deterministic, destination-deduplicated moves.
// dst may be reused by callers to avoid hot-path allocations.
func GeneratePieceMoves(dst []Move, state *MatchState, from Hole) []Move {
	if state == nil || int(from) >= HoleCount || state.Board[from] == 0 {
		return dst
	}
	piece := state.Board[from]
	seat := state.Owner[piece]
	for _, to := range Standard.Neighbors[from] {
		if to != NoHole && state.Board[to] == 0 && campMoveAllowed(seat, from, to) {
			m := Move{From: from, Len: 1, Kind: Step}
			m.Landings[0] = to
			dst = append(dst, m)
		}
	}
	var visited [HoleCount]bool
	var emitted [HoleCount]bool
	var path [MaxLandings]Hole
	visited[from] = true
	var walk func(Hole, uint8)
	walk = func(at Hole, depth uint8) {
		for d, to := range Standard.Jumps[at] {
			mid := Standard.Middles[at][d]
			if to == NoHole || mid == NoHole || mid == from || state.Board[mid] == 0 || state.Board[to] != 0 || visited[to] || !campMoveAllowed(seat, at, to) {
				continue
			}
			visited[to] = true
			path[depth] = to
			if !emitted[to] {
				m := Move{From: from, Len: depth + 1, Kind: Hop}
				copy(m.Landings[:], path[:depth+1])
				dst = append(dst, m)
				emitted[to] = true
			}
			walk(to, depth+1)
			visited[to] = false
		}
	}
	walk(from, 0)
	return dst
}

func GenerateMoves(dst []Move, state *MatchState, seat Seat) []Move {
	for h, p := range state.Board {
		if p != 0 && state.Owner[p] == seat {
			dst = GeneratePieceMoves(dst, state, Hole(h))
		}
	}
	return dst
}

// Once a piece enters its destination camp it may move within it but not leave.
func campMoveAllowed(seat Seat, from, to Hole) bool {
	dest := OppositeCamp(int(seat))
	return Standard.Camp(from) != dest || Standard.Camp(to) == dest
}

func EqualMove(a, b Move) bool {
	if a.From != b.From || a.Len != b.Len || a.Kind != b.Kind {
		return false
	}
	for i := uint8(0); i < a.Len; i++ {
		if a.Landings[i] != b.Landings[i] {
			return false
		}
	}
	return true
}
