package store

import "encoding/binary"

// entryOverhead is the bytes charged per retained log record on top of its data.
const entryOverhead = 8

type logState struct {
	first   uint64
	entries [][]byte
}

// state is the in-memory image: what a snapshot holds and a WAL replays onto.
type state struct {
	kv     map[string][]byte
	logs   map[string]*logState
	bytes  int64
	maxLog int
}

func newState(maxLog int) *state {
	return &state{kv: map[string][]byte{}, logs: map[string]*logState{}, maxLog: maxLog}
}

func (s *state) applyAll(ops []op) {
	for _, o := range ops {
		s.apply(o)
	}
}

// apply performs one op. It copies o.val, which may alias a read buffer.
func (s *state) apply(o op) {
	switch o.kind {
	case opPut:
		if old, ok := s.kv[o.key]; ok {
			s.bytes -= int64(len(o.key) + len(old))
		}
		s.kv[o.key] = append([]byte(nil), o.val...)
		s.bytes += int64(len(o.key) + len(o.val))
	case opDel:
		if old, ok := s.kv[o.key]; ok {
			s.bytes -= int64(len(o.key) + len(old))
			delete(s.kv, o.key)
		}
	case opLogHead:
		first, w := decodeUvarint(o.val)
		if w <= 0 || first == 0 {
			return
		}
		l := s.logs[o.key]
		if l == nil {
			l = &logState{}
			s.logs[o.key] = l
		}
		for _, e := range l.entries {
			s.bytes -= int64(len(e)) + entryOverhead
		}
		l.first, l.entries = first, nil
	case opDropLog:
		if l := s.logs[o.key]; l != nil {
			for _, e := range l.entries {
				s.bytes -= int64(len(e)) + entryOverhead
			}
			delete(s.logs, o.key)
		}
	case opAppend:
		l := s.logs[o.key]
		if l == nil {
			l = &logState{first: 1}
			s.logs[o.key] = l
		}
		l.entries = append(l.entries, append([]byte(nil), o.val...))
		s.bytes += int64(len(o.val)) + entryOverhead
		for s.maxLog > 0 && len(l.entries) > s.maxLog {
			s.bytes -= int64(len(l.entries[0])) + entryOverhead
			l.entries[0] = nil
			l.entries = l.entries[1:]
			l.first++
		}
		if len(l.entries) == 0 && cap(l.entries) > 4096 {
			l.entries = nil
		}
	}
}

func encodeUvarint(v uint64) []byte { return binary.AppendUvarint(nil, v) }

func decodeUvarint(b []byte) (uint64, int) { return binary.Uvarint(b) }
