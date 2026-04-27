package sync

import (
	"bytes"
	"encoding/hex"
	"sort"
)

// State tracks per-peer sync progress.
type State struct {
	Initialized    bool
	LastSentHeads  [][32]byte
	KnownHashes    map[string]struct{}
	SentHashes     map[string]struct{}
	NeedHashes     map[string]struct{}
	PeerNeedHashes map[string]struct{}
}

func NewState() *State {
	return &State{
		KnownHashes:    make(map[string]struct{}),
		SentHashes:     make(map[string]struct{}),
		NeedHashes:     make(map[string]struct{}),
		PeerNeedHashes: make(map[string]struct{}),
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
	key := hex.EncodeToString(hash[:])
	s.SentHashes[key] = struct{}{}
	delete(s.PeerNeedHashes, key)
}

func (s *State) MarkKnown(hash [32]byte) {
	if s == nil {
		return
	}
	key := hex.EncodeToString(hash[:])
	s.KnownHashes[key] = struct{}{}
	delete(s.SentHashes, key)
	delete(s.NeedHashes, key)
	delete(s.PeerNeedHashes, key)
}

func (s *State) MarkNeed(hash [32]byte) {
	if s == nil {
		return
	}
	key := hex.EncodeToString(hash[:])
	s.NeedHashes[key] = struct{}{}
}

func (s *State) MarkPeerNeed(hash [32]byte) {
	if s == nil {
		return
	}
	key := hex.EncodeToString(hash[:])
	delete(s.KnownHashes, key)
	delete(s.SentHashes, key)
	s.PeerNeedHashes[key] = struct{}{}
}

func (s *State) HasPeerNeed(hash [32]byte) bool {
	if s == nil {
		return false
	}
	_, ok := s.PeerNeedHashes[hex.EncodeToString(hash[:])]
	return ok
}

func (s *State) Needed() [][32]byte {
	if s == nil || len(s.NeedHashes) == 0 {
		return nil
	}
	return hashMapValues(s.NeedHashes)
}

func (s *State) PeerNeeded() [][32]byte {
	if s == nil || len(s.PeerNeedHashes) == 0 {
		return nil
	}
	return hashMapValues(s.PeerNeedHashes)
}

func (s *State) NoteHeads(heads [][32]byte) {
	if s == nil {
		return
	}
	s.Initialized = true
	s.LastSentHeads = normalizeHeads(heads)
}

func (s *State) ShouldSend(heads [][32]byte, hasChanges bool) bool {
	if s == nil {
		return false
	}
	if !s.Initialized || hasChanges || len(s.NeedHashes) > 0 {
		return true
	}
	current := normalizeHeads(heads)
	if len(s.LastSentHeads) != len(current) {
		return true
	}
	for i := range current {
		if s.LastSentHeads[i] != current[i] {
			return true
		}
	}
	return false
}

func hashMapValues(values map[string]struct{}) [][32]byte {
	out := make([][32]byte, 0, len(values))
	for value := range values {
		raw, err := hex.DecodeString(value)
		if err != nil || len(raw) != 32 {
			continue
		}
		var hash [32]byte
		copy(hash[:], raw)
		out = append(out, hash)
	}
	return normalizeHeads(out)
}

func normalizeHeads(heads [][32]byte) [][32]byte {
	out := make([][32]byte, len(heads))
	copy(out, heads)
	sort.Slice(out, func(i, j int) bool {
		return bytes.Compare(out[i][:], out[j][:]) < 0
	})
	return out
}
