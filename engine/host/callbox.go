package host

import (
	"bytes"
	"encoding/json"
	"slices"

	"m31labs.dev/gosx/engine"
)

// callEntry is one call found in the call signal's value: its ID and its JSON.
type callEntry struct {
	id  uint64
	raw json.RawMessage
}

// decodeCalls reads the value of a call signal. The value is one call (an
// object) or a mailbox (an array of calls); mailbox says which. It returns the
// calls that have a usable ID, in ascending ID order, with one entry per ID. A
// call whose ID does not decode cannot be answered and is dropped here; a call
// whose other fields do not decode is kept, so that run can answer it with an
// error.
func decodeCalls(data json.RawMessage) (entries []callEntry, mailbox bool) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, false
	}
	if data[0] != '[' {
		if entry, ok := decodeCallEntry(data); ok {
			return []callEntry{entry}, false
		}
		return nil, false
	}
	var raws []json.RawMessage
	if json.Unmarshal(data, &raws) != nil {
		return nil, false
	}
	entries = make([]callEntry, 0, len(raws))
	for _, raw := range raws {
		if entry, ok := decodeCallEntry(raw); ok {
			entries = append(entries, entry)
		}
	}
	slices.SortStableFunc(entries, func(a, b callEntry) int {
		switch {
		case a.id < b.id:
			return -1
		case a.id > b.id:
			return 1
		}
		return 0
	})
	// A mailbox that repeats an ID (a writer that rewrote its list) runs it once.
	return slices.CompactFunc(entries, func(a, b callEntry) bool { return a.id == b.id }), true
}

func decodeCallEntry(raw json.RawMessage) (callEntry, bool) {
	// The ID decodes on its own, so a call with a wrongly typed Name or Args
	// still gets an answer.
	var head struct{ ID uint64 }
	if json.Unmarshal(raw, &head) != nil || head.ID == 0 {
		return callEntry{}, false
	}
	return callEntry{id: head.ID, raw: raw}, true
}

// recentReply is one entry of the engine's reply log.
type recentReply struct {
	id    uint64
	err   string
	value json.RawMessage // nil once it no longer fits the byte budget, and for an error
}

// replyLog keeps the last few replies for Result.Recent. It keeps a reply's
// value while the newest entries' values fit in budget bytes, so that a client
// that missed the reply's own result can still read it, and the result signal
// stays small when replies are large.
type replyLog struct {
	keep   int
	budget int
	items  []recentReply
}

// add records a reply and returns the Recent list to publish with it.
func (l *replyLog) add(id uint64, errText string, value json.RawMessage) []engine.ResultRef {
	entry := recentReply{id: id, err: errText}
	if errText == "" && l.budget > 0 && len(value) > 0 {
		entry.value = value
	}
	l.items = append(l.items, entry)
	if len(l.items) > l.keep {
		l.items = append([]recentReply(nil), l.items[len(l.items)-l.keep:]...)
	}
	// Spend the budget from the newest entry back; a value that does not fit is
	// dropped for good, so the log does not hold memory the page never sees.
	left := l.budget
	for i := len(l.items) - 1; i >= 0; i-- {
		n := len(l.items[i].value)
		switch {
		case n == 0:
		case n <= left:
			left -= n
		default:
			l.items[i].value = nil
		}
	}
	refs := make([]engine.ResultRef, len(l.items))
	for i, item := range l.items {
		refs[i] = engine.ResultRef{ID: item.id, Error: item.err, Value: item.value}
	}
	return refs
}
