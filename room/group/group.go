// Package group keeps named persistent groups (a league, a clan, a season) and
// their members in a room/store. A group outlives the rooms its members play
// in and the process that serves them. The package holds no game rules: a group
// is an ID, a name, members, a free-form state blob, and an event log.
package group

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"m31labs.dev/gosx/room/store"
)

// Errors returned by the registry.
var (
	ErrExists    = errors.New("group: already exists")
	ErrNotFound  = errors.New("group: not found")
	ErrBadID     = errors.New("group: id must be 1-64 characters of a-z, 0-9, '-' or '_'")
	ErrFull      = errors.New("group: member limit reached")
	ErrNoMember  = errors.New("group: no such member")
	ErrMemberDup = errors.New("group: member already exists")
)

const (
	keyGroup  = "group/g/"
	keyMember = "group/m/"
	keyState  = "group/s/"
	keyEvents = "group/e/"
)

// Group is a persistent group's record.
type Group struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Created time.Time         `json:"created"`
	Meta    map[string]string `json:"meta,omitempty"`
}

// Member is one seat in a group. Which device holds the seat is the
// identity package's concern.
type Member struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Joined time.Time         `json:"joined"`
	Meta   map[string]string `json:"meta,omitempty"`
}

// Options configures a Registry.
type Options struct {
	// MaxMembers limits members per group. Default 256.
	MaxMembers int
	// Now returns the current time. Default time.Now.
	Now func() time.Time
}

// Registry reads and writes groups in a store. It is safe for concurrent use.
type Registry struct {
	st   *store.Store
	opts Options
	mu   sync.Mutex
}

// New returns a registry over st.
func New(st *store.Store, opts Options) *Registry {
	if opts.MaxMembers <= 0 {
		opts.MaxMembers = 256
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Registry{st: st, opts: opts}
}

// ValidID reports whether id can name a group or member.
func ValidID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// Create makes a group. It fails with ErrExists if id is taken.
func (r *Registry) Create(id, name string, meta map[string]string) (Group, error) {
	if !ValidID(id) {
		return Group{}, ErrBadID
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.st.Get(keyGroup + id); ok {
		return Group{}, ErrExists
	}
	g := Group{ID: id, Name: name, Created: r.opts.Now().UTC(), Meta: meta}
	b, _ := json.Marshal(g)
	if err := r.st.Put(keyGroup+id, b); err != nil {
		return Group{}, err
	}
	return g, nil
}

// Get returns the group with id.
func (r *Registry) Get(id string) (Group, error) {
	b, ok := r.st.Get(keyGroup + id)
	if !ok {
		return Group{}, ErrNotFound
	}
	var g Group
	if err := json.Unmarshal(b, &g); err != nil {
		return Group{}, fmt.Errorf("group: %s: %w", id, err)
	}
	return g, nil
}

// List returns every group, sorted by ID.
func (r *Registry) List() []Group {
	var out []Group
	r.st.Scan(keyGroup, func(_ string, v []byte) bool {
		var g Group
		if json.Unmarshal(v, &g) == nil {
			out = append(out, g)
		}
		return true
	})
	return out
}

// Delete removes a group, its members, state, and event log in one commit.
func (r *Registry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.st.Get(keyGroup + id); !ok {
		return ErrNotFound
	}
	b := &store.Batch{}
	b.Delete(keyGroup + id).Delete(keyState + id).DropLog(keyEvents + id)
	for _, k := range r.st.Keys(keyMember + id + "/") {
		b.Delete(k)
	}
	return r.st.Commit(b)
}

// AddMember adds a member. The member's Joined time is set when zero.
func (r *Registry) AddMember(groupID string, m Member) (Member, error) {
	if !ValidID(m.ID) {
		return Member{}, ErrBadID
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.st.Get(keyGroup + groupID); !ok {
		return Member{}, ErrNotFound
	}
	key := keyMember + groupID + "/" + m.ID
	if _, ok := r.st.Get(key); ok {
		return Member{}, ErrMemberDup
	}
	if len(r.st.Keys(keyMember+groupID+"/")) >= r.opts.MaxMembers {
		return Member{}, ErrFull
	}
	if m.Joined.IsZero() {
		m.Joined = r.opts.Now().UTC()
	}
	b, _ := json.Marshal(m)
	if err := r.st.Put(key, b); err != nil {
		return Member{}, err
	}
	return m, nil
}

// RemoveMember removes a member.
func (r *Registry) RemoveMember(groupID, memberID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := keyMember + groupID + "/" + memberID
	if _, ok := r.st.Get(key); !ok {
		return ErrNoMember
	}
	return r.st.Delete(key)
}

// Member returns one member.
func (r *Registry) Member(groupID, memberID string) (Member, error) {
	b, ok := r.st.Get(keyMember + groupID + "/" + memberID)
	if !ok {
		return Member{}, ErrNoMember
	}
	var m Member
	if err := json.Unmarshal(b, &m); err != nil {
		return Member{}, err
	}
	return m, nil
}

// HasMember reports whether memberID belongs to groupID. It matches the
// identity package's seat validator signature.
func (r *Registry) HasMember(groupID, memberID string) bool {
	_, ok := r.st.Get(keyMember + groupID + "/" + memberID)
	return ok
}

// Members returns the group's members sorted by ID.
func (r *Registry) Members(groupID string) []Member {
	var out []Member
	r.st.Scan(keyMember+groupID+"/", func(_ string, v []byte) bool {
		var m Member
		if json.Unmarshal(v, &m) == nil {
			out = append(out, m)
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SetState stores the group's opaque state blob.
func (r *Registry) SetState(groupID string, state []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.st.Get(keyGroup + groupID); !ok {
		return ErrNotFound
	}
	return r.st.Put(keyState+groupID, state)
}

// State returns the group's state blob, or nil if none was set.
func (r *Registry) State(groupID string) []byte {
	b, _ := r.st.Get(keyState + groupID)
	return b
}

// AppendEvent adds rec to the group's event log and returns its sequence.
// The log keeps the newest entries, bounded by the store's MaxLogEntries.
func (r *Registry) AppendEvent(groupID string, rec []byte) (uint64, error) {
	if _, ok := r.st.Get(keyGroup + groupID); !ok {
		return 0, ErrNotFound
	}
	return r.st.Append(keyEvents+groupID, rec)
}

// Events returns up to limit events with sequence >= from.
func (r *Registry) Events(groupID string, from uint64, limit int) []store.Entry {
	return r.st.LogRead(keyEvents+groupID, from, limit)
}

// Store returns the underlying store, so callers can commit related data in
// the same batch (see EventsKey and StateKey).
func (r *Registry) Store() *store.Store { return r.st }

// EventsKey is the store log name of a group's event log.
func EventsKey(groupID string) string { return keyEvents + groupID }

// StateKey is the store key of a group's state blob.
func StateKey(groupID string) string { return keyState + groupID }
