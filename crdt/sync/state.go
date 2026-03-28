package sync

import (
	"encoding/hex"
)

// State tracks per-peer sync progress.
type State struct {
	Initialized   bool
	LastSentHeads [][32]byte
	KnownHashes   map[string]struct{}
	SentHashes    map[string]struct{}
}

func NewState() *State {
	return &State{
		KnownHashes: make(map[string]struct{}),
		SentHashes:  make(map[string]struct{}),
	}
}

func (s *State) HasKnown(hash [32]byte) bool {
	if s == nil {
		return false
	}
	_, ok := s.KnownHashes[hex.EncodeToString(hash[:])]
	return ok
}

func (s *State) HasSent(hash [32]byte) bool {
	if s == nil {
		return false
	}
	_, ok := s.SentHashes[hex.EncodeToString(hash[:])]
	return ok
}

func (s *State) MarkSent(hash [32]byte) {
	if s == nil {
		return
	}
	s.SentHashes[hex.EncodeToString(hash[:])] = struct{}{}
}

func (s *State) MarkKnown(hash [32]byte) {
	if s == nil {
		return
	}
	key := hex.EncodeToString(hash[:])
	s.KnownHashes[key] = struct{}{}
	delete(s.SentHashes, key)
}

func (s *State) NoteHeads(heads [][32]byte) {
	if s == nil {
		return
	}
	s.Initialized = true
	s.LastSentHeads = cloneHeads(heads)
}

func (s *State) ShouldSend(heads [][32]byte, hasChanges bool) bool {
	if s == nil {
		return false
	}
	if !s.Initialized || hasChanges {
		return true
	}
	if len(s.LastSentHeads) != len(heads) {
		return true
	}
	for i := range heads {
		if s.LastSentHeads[i] != heads[i] {
			return true
		}
	}
	return false
}

func cloneHeads(heads [][32]byte) [][32]byte {
	out := make([][32]byte, len(heads))
	copy(out, heads)
	return out
}
