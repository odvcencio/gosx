package teamai

// maxBranches bounds the branches of one decision.
const maxBranches = 16

// Decision is a point where a path or an assignment picks what comes next.
// Each branch names a rule and a target. In "first" mode the first branch
// whose rule holds is taken; in "weighted" mode one of the branches whose rule
// holds is drawn with probability proportional to its weight, from a seeded
// hash, so the draw is the same on every platform. A Chooser, when set,
// replaces both. The agent first tests a decision Body.Reaction ticks after
// reaching it, then every tick for Window seconds; when no branch is taken by
// then, the decision moves on to its default. A decision with no window is
// tested once.
type Decision struct {
	branches  []branch
	window    float64
	weighted  bool
	chooser   int16 // index into the Book's choosers, or -1
	otherwise int16 // target when no branch fires; -1 ends
	// key identifies the decision in weighted draws: its position in the
	// Spec, so the draw does not depend on memory addresses.
	key uint32
}

type branch struct {
	label  string // target label, for Choosers and events
	when   string // rule name; "" always holds
	rule   int16  // index into the Book's predicates, or -1 for always
	weight float64
	target int16
}

// NumBranches returns the number of branches.
func (d *Decision) NumBranches() int { return len(d.branches) }

// BranchLabel returns the label branch i jumps to.
func (d *Decision) BranchLabel(i int) string { return d.branches[i].label }

// BranchWeight returns the weight of branch i.
func (d *Decision) BranchWeight(i int) float64 { return d.branches[i].weight }

// BranchRule returns the rule name of branch i, or "" when the branch always
// holds.
func (d *Decision) BranchRule(i int) string { return d.branches[i].when }

// Window returns the seconds the decision waits for a branch.
func (d *Decision) Window() float64 { return d.window }

// BranchSpec is one branch of a decision.
type BranchSpec struct {
	// When names a rule. Empty means the branch always holds.
	When string `json:"when,omitempty"`
	// Goto is the label of the segment or step to jump to.
	Goto string `json:"goto"`
	// Weight is the relative chance in weighted mode. Zero means 1.
	Weight float64 `json:"weight,omitempty"`
}

// DecisionSpec is the decision part of a decide segment or step.
type DecisionSpec struct {
	Branches []BranchSpec `json:"branches,omitempty"`
	// Window is how long, in seconds, the decision keeps testing its
	// branches before it takes Otherwise. Zero tests once.
	Window float64 `json:"window,omitempty"`
	// Mode is "first" (the default) or "weighted".
	Mode string `json:"mode,omitempty"`
	// Chooser names a registered Chooser that picks the branch instead.
	Chooser string `json:"chooser,omitempty"`
	// Otherwise is the label to take when no branch fires. Empty means the
	// next entry.
	Otherwise string `json:"otherwise,omitempty"`
}

// compileDecision checks ds. labels maps a label to its index; next is the
// index that follows the decision (-1 at the end).
func compileDecision(is *issues, path string, ds *DecisionSpec, labels func(string) int, next int16, rb *ruleBinder) Decision {
	rb.decisions++
	d := Decision{window: ds.Window, chooser: -1, otherwise: next, key: rb.decisions}
	if !finite(ds.Window) || ds.Window < 0 {
		is.addf(field(path, "window"), "window %v: want a finite number of seconds, 0 or more", ds.Window)
		d.window = 0
	}
	switch ds.Mode {
	case "", "first":
	case "weighted":
		d.weighted = true
	default:
		is.addf(field(path, "mode"), "mode %q: want first or weighted", ds.Mode)
	}
	if len(ds.Branches) == 0 && ds.Chooser == "" {
		is.addf(field(path, "branches"), "a decision needs branches")
	}
	if len(ds.Branches) > maxBranches {
		is.addf(field(path, "branches"), "%d branches; at most %d", len(ds.Branches), maxBranches)
		return d
	}
	if ds.Otherwise != "" {
		if j := labels(ds.Otherwise); j >= 0 {
			d.otherwise = int16(j)
		} else {
			is.addf(field(path, "otherwise"), "unknown label %q", ds.Otherwise)
		}
	}
	if ds.Chooser != "" {
		d.chooser = rb.chooser(is, field(path, "chooser"), ds.Chooser)
	}
	d.branches = make([]branch, len(ds.Branches))
	for i := range ds.Branches {
		bs := &ds.Branches[i]
		bp := at(path, "branches", i, bs.Goto)
		br := &d.branches[i]
		br.label = bs.Goto
		br.when = bs.When
		br.weight = bs.Weight
		br.rule = -1
		if br.weight == 0 {
			br.weight = 1
		}
		if !finite(bs.Weight) || bs.Weight < 0 {
			is.addf(field(bp, "weight"), "weight %v: want a finite number, 0 or more", bs.Weight)
			br.weight = 1
		}
		j := labels(bs.Goto)
		if j < 0 {
			is.addf(field(bp, "goto"), "unknown label %q", bs.Goto)
		}
		br.target = int16(j)
		if bs.When != "" {
			br.rule = rb.predicate(is, field(bp, "when"), bs.When)
		}
	}
	return d
}
