package checkers

import "fmt"

const RulesetVersion = "standard-1"

type Outcome struct {
	Winner   Seat
	Finished bool
}

type MatchState struct {
	Ruleset  string
	Seats    []Seat
	Active   Seat
	Board    [HoleCount]PieceID
	Owner    [CampCount*CampSize + 1]Seat
	Turn     uint32
	Revision uint64
	Outcome  Outcome
	History  []Move
}

type Undo struct {
	Move       Move
	Piece      PieceID
	Active     Seat
	Turn       uint32
	Revision   uint64
	Outcome    Outcome
	HistoryLen int
}

func NewMatch(seats ...Seat) (*MatchState, error) {
	if len(seats) == 0 {
		seats = []Seat{0, 3}
	}
	seen := [CampCount]bool{}
	m := &MatchState{Ruleset: RulesetVersion, Seats: append([]Seat(nil), seats...), Active: seats[0], Revision: 1}
	for _, seat := range seats {
		if int(seat) >= CampCount || seen[seat] {
			return nil, fmt.Errorf("checkers: invalid/duplicate seat %d", seat)
		}
		seen[seat] = true
		for i, h := range Standard.Camps[seat] {
			id := PieceID(int(seat)*CampSize + i + 1)
			m.Board[h] = id
			m.Owner[id] = seat
		}
	}
	return m, nil
}

func (m *MatchState) Clone() *MatchState {
	c := *m
	c.Seats = append([]Seat(nil), m.Seats...)
	c.History = append([]Move(nil), m.History...)
	return &c
}

func (m *MatchState) Apply(move Move) (Undo, error) {
	if m.Outcome.Finished {
		return Undo{}, ErrGameOver
	}
	if int(move.From) >= HoleCount || m.Board[move.From] == 0 || m.Owner[m.Board[move.From]] != m.Active {
		return Undo{}, ErrWrongTurn
	}
	legal := GeneratePieceMoves(nil, m, move.From)
	ok := false
	for _, candidate := range legal {
		if EqualMove(candidate, move) {
			ok = true
			break
		}
	}
	if !ok {
		return Undo{}, ErrIllegalMove
	}
	piece := m.Board[move.From]
	u := Undo{Move: move, Piece: piece, Active: m.Active, Turn: m.Turn, Revision: m.Revision, Outcome: m.Outcome, HistoryLen: len(m.History)}
	m.Board[move.From], m.Board[move.To()] = 0, piece
	m.History = append(m.History, move)
	m.Turn++
	m.Revision++
	if m.hasWon(m.Active) {
		m.Outcome = Outcome{Winner: m.Active, Finished: true}
	} else {
		m.Active = m.nextSeat(m.Active)
	}
	return u, nil
}

func (m *MatchState) Unapply(u Undo) {
	m.Board[u.Move.To()] = 0
	m.Board[u.Move.From] = u.Piece
	m.Active, m.Turn, m.Revision, m.Outcome = u.Active, u.Turn, u.Revision, u.Outcome
	if u.HistoryLen <= len(m.History) {
		m.History = m.History[:u.HistoryLen]
		if u.HistoryLen == 0 {
			m.History = nil
		}
	}
}

func (m *MatchState) hasWon(seat Seat) bool {
	for _, h := range Standard.Camps[OppositeCamp(int(seat))] {
		p := m.Board[h]
		if p == 0 || m.Owner[p] != seat {
			return false
		}
	}
	return true
}

func (m *MatchState) nextSeat(seat Seat) Seat {
	for i, s := range m.Seats {
		if s == seat {
			return m.Seats[(i+1)%len(m.Seats)]
		}
	}
	return m.Seats[0]
}
