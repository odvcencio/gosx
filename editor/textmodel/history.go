package textmodel

type undoGroup struct {
	actor string
	ops   []Operation
}

// History tracks operations for undo/redo with coalescing.
type History struct {
	undo []undoGroup
	redo []undoGroup
}

func NewHistory() *History {
	return &History{}
}

// Push records an operation. Adjacent single-char "user" inserts coalesce.
func (h *History) Push(op Operation) {
	h.redo = removeActorGroups(h.redo, op.Actor)

	if h.shouldCoalesce(op) {
		h.undo[len(h.undo)-1].ops = append(h.undo[len(h.undo)-1].ops, op)
		return
	}
	h.undo = append(h.undo, undoGroup{actor: op.Actor, ops: []Operation{op}})
}

func (h *History) shouldCoalesce(op Operation) bool {
	if len(h.undo) == 0 {
		return false
	}
	if op.Origin != "user" {
		return false
	}
	if op.Kind != OpInsert || len(op.Content) != 1 {
		return false
	}
	last := h.undo[len(h.undo)-1]
	if last.actor != op.Actor {
		return false
	}
	if len(last.ops) == 0 {
		return false
	}
	lastOp := last.ops[len(last.ops)-1]
	if lastOp.Origin != "user" || lastOp.Kind != OpInsert {
		return false
	}
	return true
}

// Undo pops the most recent undo group and returns its first op.
func (h *History) Undo() (Operation, bool) {
	if len(h.undo) == 0 {
		return Operation{}, false
	}
	group := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	h.redo = append(h.redo, group)
	return group.ops[0], true
}

// UndoActor pops only the newest group authored by actor. Edits from other
// collaborators remain in history and are never selected as a side effect.
func (h *History) UndoActor(actor string) (Operation, bool) {
	index := lastActorGroup(h.undo, actor)
	if index < 0 {
		return Operation{}, false
	}
	group := h.undo[index]
	h.undo = append(h.undo[:index], h.undo[index+1:]...)
	h.redo = append(h.redo, group)
	return group.ops[0], true
}

// Redo pops the most recent redo group and returns its first op.
func (h *History) Redo() (Operation, bool) {
	if len(h.redo) == 0 {
		return Operation{}, false
	}
	group := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	h.undo = append(h.undo, group)
	return group.ops[0], true
}

// RedoActor reapplies only the newest undone group authored by actor.
func (h *History) RedoActor(actor string) (Operation, bool) {
	index := lastActorGroup(h.redo, actor)
	if index < 0 {
		return Operation{}, false
	}
	group := h.redo[index]
	h.redo = append(h.redo[:index], h.redo[index+1:]...)
	h.undo = append(h.undo, group)
	return group.ops[0], true
}

func lastActorGroup(groups []undoGroup, actor string) int {
	for i := len(groups) - 1; i >= 0; i-- {
		if groups[i].actor == actor {
			return i
		}
	}
	return -1
}

func removeActorGroups(groups []undoGroup, actor string) []undoGroup {
	kept := groups[:0]
	for _, group := range groups {
		if group.actor != actor {
			kept = append(kept, group)
		}
	}
	return kept
}
