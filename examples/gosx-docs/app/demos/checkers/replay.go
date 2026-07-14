package checkers

import (
	"fmt"
	"strconv"
	"strings"
)

func Notation(m Move) string {
	sep := "-"
	if m.Kind == Hop {
		sep = ">"
	}
	parts := make([]string, 0, int(m.Len)+1)
	parts = append(parts, strconv.Itoa(int(m.From)))
	for i := uint8(0); i < m.Len; i++ {
		parts = append(parts, strconv.Itoa(int(m.Landings[i])))
	}
	return strings.Join(parts, sep)
}

func ParseNotation(s string) (Move, error) {
	kind, sep := Step, "-"
	if strings.Contains(s, ">") {
		kind, sep = Hop, ">"
	}
	parts := strings.Split(s, sep)
	if len(parts) < 2 || len(parts)-1 > MaxLandings {
		return Move{}, ErrIllegalMove
	}
	vals := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n >= HoleCount {
			return Move{}, fmt.Errorf("%w: notation", ErrIllegalMove)
		}
		vals[i] = n
	}
	if kind == Step && len(vals) != 2 {
		return Move{}, ErrIllegalMove
	}
	m := Move{From: Hole(vals[0]), Len: uint8(len(vals) - 1), Kind: kind}
	for i := 1; i < len(vals); i++ {
		m.Landings[i-1] = Hole(vals[i])
	}
	return m, nil
}

func Replay(initial *MatchState, notation []string) (*MatchState, error) {
	m := initial.Clone()
	m.History = nil
	for i, text := range notation {
		move, err := ParseNotation(text)
		if err != nil {
			return nil, fmt.Errorf("replay move %d: %w", i, err)
		}
		if _, err = m.Apply(move); err != nil {
			return nil, fmt.Errorf("replay move %d: %w", i, err)
		}
	}
	return m, nil
}
