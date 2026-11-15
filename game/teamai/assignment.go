package teamai

import "strings"

// AssignmentID indexes Book assignments. NoAssignment is -1.
type AssignmentID int16

// NoAssignment marks an agent without an assignment.
const NoAssignment AssignmentID = -1

// PlanID indexes Book plans.
type PlanID int16

// StepKind is what an assignment step does.
type StepKind uint8

const (
	// StepSlot moves to the agent's formation slot.
	StepSlot StepKind = iota + 1
	// StepMove moves to another slot, a landmark, or an offset.
	StepMove
	// StepPath follows a path.
	StepPath
	// StepHold stands still.
	StepHold
	// StepMirror holds an offset from a moving target.
	StepMirror
	// StepGuard guards a zone around a landmark.
	StepGuard
	// StepPursue runs down a target, leading it.
	StepPursue
	// StepCutoff takes the cutoff angle on a target against the field edge.
	StepCutoff
	// StepEngage pursues a target, then stays in contact.
	StepEngage
	// StepDecide picks the next step by rule.
	StepDecide
)

var stepKinds = []struct {
	name string
	kind StepKind
}{
	{"slot", StepSlot}, {"move", StepMove}, {"path", StepPath}, {"hold", StepHold},
	{"mirror", StepMirror}, {"guard", StepGuard}, {"pursue", StepPursue},
	{"cutoff", StepCutoff}, {"engage", StepEngage}, {"decide", StepDecide},
}

func (k StepKind) String() string {
	for _, s := range stepKinds {
		if s.kind == k {
			return s.name
		}
	}
	return "unknown"
}

// AssignmentSpec is a small state machine of steps an agent runs, with
// interrupts that jump to a step when a rule starts to hold.
type AssignmentSpec struct {
	Name       string          `json:"name"`
	Steps      []StepSpec      `json:"steps"`
	Interrupts []InterruptSpec `json:"interrupts,omitempty"`
}

// StepSpec is one step of an assignment. Do selects the kind:
//
//	slot     move to the agent's formation slot (plus Offset)
//	move     move to Slot (another slot of the formation), to Landmark, or by
//	         Offset from where the step started
//	path     follow Path, starting where the agent stands
//	hold     stand still
//	mirror   hold Offset from Target, matching its velocity
//	guard    guard the zone of Radius around Landmark (plus Offset); when
//	         Target is inside it, stand Gap units from Target toward Protect
//	pursue   run down Target, leading it; with Radius, end on contact
//	cutoff   take the cutoff angle on Target against the field edge, arriving
//	         Margin seconds early; with Radius, end on contact
//	engage   pursue Target until within Radius, then stay in contact
//	decide   pick the next step with Branches (see DecisionSpec)
//
// A step ends when Until holds, when Duration seconds pass, or (when Until is
// empty) at its natural end: arrival for slot and move, the end of the path,
// contact for pursue and cutoff with a Radius. Mirror, guard and engage have
// no natural end. Offsets are (lateral, depth) in the team frame, lateral
// signed by the agent's slot side.
type StepSpec struct {
	Label    string  `json:"label,omitempty"`
	Do       string  `json:"do"`
	Path     string  `json:"path,omitempty"`
	Slot     string  `json:"slot,omitempty"`
	Landmark string  `json:"landmark,omitempty"`
	Protect  string  `json:"protect,omitempty"`
	Target   string  `json:"target,omitempty"`
	Offset   Vec2    `json:"offset,omitempty"`
	Radius   float64 `json:"radius,omitempty"`
	Gap      float64 `json:"gap,omitempty"`
	Margin   float64 `json:"margin,omitempty"`
	// Speed is a fraction of the agent's top speed. Zero means 1.
	Speed    float64 `json:"speed,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	Until    string  `json:"until,omitempty"`
	// Latch picks the target once when the step starts instead of every
	// tick.
	Latch bool `json:"latch,omitempty"`
	// Mark is set, and reported, when the step ends.
	Mark string `json:"mark,omitempty"`
	// Next is the label to continue at. Empty means the following step; the
	// assignment ends after its last step.
	Next string `json:"next,omitempty"`
	DecisionSpec
}

// InterruptSpec jumps to the step labelled Goto when the rule When starts to
// hold. It fires once each time the rule goes from false to true, after the
// agent's Reaction ticks.
type InterruptSpec struct {
	When string `json:"when"`
	Goto string `json:"goto"`
}

// PlanSpec puts a team in a formation and gives every slot an assignment.
type PlanSpec struct {
	Name      string `json:"name"`
	Formation string `json:"formation"`
	// Assign gives slots assignments, by slot name or by role.
	Assign []PlanAssignSpec `json:"assign"`
	// Default is the assignment of slots that Assign does not cover. Empty
	// leaves them without one.
	Default string `json:"default,omitempty"`
	// Extra is the assignment of team agents left without a slot. Empty
	// leaves them without one.
	Extra string `json:"extra,omitempty"`
}

// PlanAssignSpec gives one slot, or every slot with a role, an assignment.
type PlanAssignSpec struct {
	Slot       string `json:"slot,omitempty"`
	Role       string `json:"role,omitempty"`
	Assignment string `json:"assignment"`
}

// Target selectors.
const (
	selNone uint8 = iota
	selNearestOpp
	selNearestMate
	selOppRole
	selMateRole
	selObject
	selLandmark
	selAssigned
)

type selector struct {
	kind  uint8
	index int16
}

func parseSelector(b *Book, s string) (selector, bool) {
	switch s {
	case "":
		return selector{}, true
	case "nearest_opponent":
		return selector{kind: selNearestOpp}, true
	case "nearest_teammate":
		return selector{kind: selNearestMate}, true
	case "assigned":
		return selector{kind: selAssigned}, true
	}
	head, name, ok := strings.Cut(s, ":")
	if !ok {
		return selector{}, false
	}
	switch head {
	case "opponent", "teammate":
		r, found := b.Role(name)
		if !found {
			return selector{}, false
		}
		k := selOppRole
		if head == "teammate" {
			k = selMateRole
		}
		return selector{kind: k, index: int16(r)}, true
	case "object":
		i := indexOf(b.objects, name)
		return selector{kind: selObject, index: int16(i)}, i >= 0
	case "landmark":
		id, found := b.Landmark(name)
		return selector{kind: selLandmark, index: int16(id)}, found
	}
	return selector{}, false
}

// Assignment is a compiled AssignmentSpec.
type Assignment struct {
	name       string
	steps      []step
	interrupts []interrupt
}

type step struct {
	kind     StepKind
	label    string
	path     PathID
	slot     int16 // slot name, resolved per formation at run time: index into slotNames
	landmark LandmarkID
	protect  LandmarkID
	target   selector
	offset   Vec2
	radius   float64
	gap      float64
	margin   float64
	speed    float64
	duration float64
	until    int16 // predicate, or -1
	latch    bool
	mark     int16
	next     int16
	decision Decision
}

type interrupt struct {
	rule   int16
	target int16
}

// Name returns the assignment's name.
func (a *Assignment) Name() string { return a.name }

// NumSteps returns the number of steps.
func (a *Assignment) NumSteps() int { return len(a.steps) }

// StepKind returns the kind of step i.
func (a *Assignment) StepKind(i int) StepKind { return a.steps[i].kind }

// StepLabel returns the label of step i, or "".
func (a *Assignment) StepLabel(i int) string { return a.steps[i].label }

// Plan is a compiled PlanSpec.
type Plan struct {
	name      string
	formation FormationID
	slotAssn  []AssignmentID // per slot
	extra     AssignmentID
}

// Name returns the plan's name.
func (p *Plan) Name() string { return p.name }

// Formation returns the plan's formation.
func (p *Plan) Formation() FormationID { return p.formation }

// SlotAssignment returns the assignment of slot i.
func (p *Plan) SlotAssignment(i int) AssignmentID { return p.slotAssn[i] }

// stepFields lists the optional fields each step kind takes.
var stepFields = map[StepKind]string{
	StepSlot:   "offset speed",
	StepMove:   "slot landmark offset speed",
	StepPath:   "path speed",
	StepHold:   "",
	StepMirror: "target offset speed latch",
	StepGuard:  "landmark protect target offset radius gap speed latch",
	StepPursue: "target radius speed latch",
	StepCutoff: "target radius margin speed latch",
	StepEngage: "target radius speed latch",
	StepDecide: "decision",
}

// maxInterrupts bounds the interrupts of one assignment: one bit each in an
// agent's state.
const maxInterrupts = 16

func compileAssignment(is *issues, path string, as *AssignmentSpec, b *Book, marks *markTable, rb *ruleBinder) Assignment {
	a := Assignment{name: as.Name}
	if len(as.Steps) == 0 {
		is.addf(field(path, "steps"), "an assignment needs at least one step")
		return a
	}
	if len(as.Steps) > maxSegments {
		is.addf(field(path, "steps"), "%d steps; at most %d", len(as.Steps), maxSegments)
		return a
	}
	if len(as.Interrupts) > maxInterrupts {
		is.addf(field(path, "interrupts"), "%d interrupts; at most %d", len(as.Interrupts), maxInterrupts)
		return a
	}
	labels := func(name string) int {
		for i := range as.Steps {
			if as.Steps[i].Label == name {
				return i
			}
		}
		return -1
	}
	a.steps = make([]step, len(as.Steps))
	for i := range as.Steps {
		ss := &as.Steps[i]
		sp := at(path, "steps", i, ss.Label)
		s := &a.steps[i]
		s.label = ss.Label
		s.path = NoPath
		s.slot = -1
		s.landmark = -1
		s.protect = -1
		s.until = -1
		s.offset = ss.Offset
		s.radius = ss.Radius
		s.gap = ss.Gap
		s.margin = ss.Margin
		s.duration = ss.Duration
		s.latch = ss.Latch
		s.speed = ss.Speed
		if s.speed == 0 {
			s.speed = 1
		}
		s.mark = marks.id(is, field(sp, "mark"), ss.Mark)
		next := int16(i + 1)
		if i == len(as.Steps)-1 {
			next = -1
		}
		s.next = next
		if ss.Label != "" {
			if !validName(ss.Label) {
				is.addf(field(sp, "label"), "label %q: use 1 to %d characters from A-Z a-z 0-9 _ -", ss.Label, maxNameLen)
			} else if j := labels(ss.Label); j != i {
				is.addf(field(sp, "label"), "duplicate label; first used at steps[%d]", j)
			}
		}
		if ss.Next != "" {
			if j := labels(ss.Next); j >= 0 {
				s.next = int16(j)
			} else {
				is.addf(field(sp, "next"), "unknown label %q", ss.Next)
			}
		}
		checkVec(is, field(sp, "offset"), ss.Offset)
		checkFraction(is, field(sp, "speed"), ss.Speed)
		checkSeconds(is, field(sp, "duration"), ss.Duration)
		checkSeconds(is, field(sp, "margin"), ss.Margin)
		checkDistance(is, field(sp, "radius"), ss.Radius)
		checkDistance(is, field(sp, "gap"), ss.Gap)
		if ss.Until != "" {
			s.until = rb.predicate(is, field(sp, "until"), ss.Until)
		}
		if ss.Path != "" {
			id, ok := b.PathID(ss.Path)
			if !ok {
				is.addf(field(sp, "path"), "unknown path %q", ss.Path)
			}
			s.path = id
		}
		if ss.Slot != "" {
			s.slot = int16(b.slotName(ss.Slot))
			if s.slot < 0 {
				is.addf(field(sp, "slot"), "no formation has a slot %q", ss.Slot)
			}
		}
		if ss.Landmark != "" {
			id, ok := b.Landmark(ss.Landmark)
			if !ok {
				is.addf(field(sp, "landmark"), "unknown landmark %q", ss.Landmark)
			}
			s.landmark = id
		}
		if ss.Protect != "" {
			id, ok := b.Landmark(ss.Protect)
			if !ok {
				is.addf(field(sp, "protect"), "unknown landmark %q", ss.Protect)
			}
			s.protect = id
		}
		if sel, ok := parseSelector(b, ss.Target); ok {
			s.target = sel
		} else {
			is.addf(field(sp, "target"), "target %q: want nearest_opponent, nearest_teammate, assigned, opponent:<role>, teammate:<role>, object:<name> or landmark:<name>", ss.Target)
		}
		hasDecision := len(ss.Branches) > 0 || ss.Chooser != "" || ss.Window != 0 || ss.Mode != "" || ss.Otherwise != ""

		kind := StepKind(0)
		for _, k := range stepKinds {
			if k.name == ss.Do {
				kind = k.kind
			}
		}
		s.kind = kind
		used := []struct {
			name string
			set  bool
		}{
			{"path", ss.Path != ""}, {"slot", ss.Slot != ""}, {"landmark", ss.Landmark != ""},
			{"protect", ss.Protect != ""}, {"target", ss.Target != ""}, {"offset", !ss.Offset.IsZero()},
			{"radius", ss.Radius != 0}, {"gap", ss.Gap != 0}, {"margin", ss.Margin != 0},
			{"speed", ss.Speed != 0}, {"latch", ss.Latch}, {"decision", hasDecision},
		}
		if kind == 0 {
			if ss.Do == "" {
				is.addf(field(sp, "do"), "missing do: want slot, move, path, hold, mirror, guard, pursue, cutoff, engage or decide")
			} else {
				is.addf(field(sp, "do"), "do %q: want slot, move, path, hold, mirror, guard, pursue, cutoff, engage or decide", ss.Do)
			}
			continue
		}
		for _, u := range used {
			if !u.set || strings.Contains(" "+stepFields[kind]+" ", " "+u.name+" ") {
				continue
			}
			if u.name == "decision" {
				is.addf(sp, "branches, window, mode, chooser and otherwise apply to a decide step only")
			} else {
				is.addf(field(sp, u.name), "a %s step does not take %s", ss.Do, u.name)
			}
		}
		switch kind {
		case StepPath:
			if ss.Path == "" {
				is.addf(sp, "a path step needs path")
			}
		case StepMove:
			if ss.Slot != "" && ss.Landmark != "" {
				is.addf(sp, "a move step takes slot or landmark, not both")
			}
		case StepHold:
			if ss.Duration <= 0 && ss.Until == "" {
				is.addf(sp, "a hold step needs duration or until")
			}
		case StepMirror, StepPursue, StepCutoff, StepEngage:
			if ss.Target == "" {
				is.addf(sp, "a %s step needs target", ss.Do)
			}
			if kind == StepEngage && ss.Radius <= 0 {
				is.addf(field(sp, "radius"), "an engage step needs a contact radius")
			}
		case StepGuard:
			if ss.Landmark == "" || ss.Radius <= 0 {
				is.addf(sp, "a guard step needs landmark and radius")
			}
		case StepDecide:
			if ss.Mark != "" || ss.Duration != 0 || ss.Until != "" {
				is.addf(sp, "a decide step takes no mark, duration or until")
			}
			if ss.Next != "" {
				is.addf(field(sp, "next"), "a decide step continues through its branches or otherwise, not next")
			}
			s.decision = compileDecision(is, sp, &ss.DecisionSpec, labels, next, rb)
		}
	}
	a.interrupts = make([]interrupt, len(as.Interrupts))
	for i := range as.Interrupts {
		in := &as.Interrupts[i]
		ip := at(path, "interrupts", i, in.When)
		a.interrupts[i].target = int16(labels(in.Goto))
		if a.interrupts[i].target < 0 {
			is.addf(field(ip, "goto"), "unknown label %q", in.Goto)
		}
		if in.When == "" {
			is.addf(field(ip, "when"), "an interrupt needs a rule")
			a.interrupts[i].rule = -1
			continue
		}
		a.interrupts[i].rule = rb.predicate(is, field(ip, "when"), in.When)
	}
	return a
}

func checkDistance(is *issues, path string, v float64) {
	if !finite(v) || v < 0 {
		is.addf(path, "%v: want a finite distance, 0 or more", v)
	}
}

func compilePlan(is *issues, path string, ps *PlanSpec, b *Book) Plan {
	p := Plan{name: ps.Name, formation: NoFormation, extra: NoAssignment}
	fid, ok := b.FormationID(ps.Formation)
	if !ok {
		is.addf(field(path, "formation"), "unknown formation %q", ps.Formation)
		return p
	}
	p.formation = fid
	f := b.Formation(fid)
	def := NoAssignment
	if ps.Default != "" {
		if def, ok = b.AssignmentID(ps.Default); !ok {
			is.addf(field(path, "default"), "unknown assignment %q", ps.Default)
		}
	}
	if ps.Extra != "" {
		if p.extra, ok = b.AssignmentID(ps.Extra); !ok {
			is.addf(field(path, "extra"), "unknown assignment %q", ps.Extra)
		}
	}
	p.slotAssn = make([]AssignmentID, f.NumSlots())
	for i := range p.slotAssn {
		p.slotAssn[i] = def
	}
	set := make([]bool, f.NumSlots())
	for i := range ps.Assign {
		pa := &ps.Assign[i]
		ap := at(path, "assign", i, pa.Slot+pa.Role)
		aid, ok := b.AssignmentID(pa.Assignment)
		if !ok {
			is.addf(field(ap, "assignment"), "unknown assignment %q", pa.Assignment)
		}
		switch {
		case (pa.Slot == "") == (pa.Role == ""):
			is.addf(ap, "give slot or role, not both or neither")
		case pa.Slot != "":
			j := f.SlotIndex(pa.Slot)
			if j < 0 {
				is.addf(field(ap, "slot"), "formation %q has no slot %q", f.name, pa.Slot)
				continue
			}
			if set[j] {
				is.addf(field(ap, "slot"), "slot %q is assigned twice", pa.Slot)
			}
			set[j] = true
			p.slotAssn[j] = aid
		default:
			r, found := b.Role(pa.Role)
			if !found {
				is.addf(field(ap, "role"), "unknown role %q", pa.Role)
				continue
			}
			n := 0
			for j := range f.slots {
				if f.slots[j].role == r && !set[j] {
					set[j] = true
					p.slotAssn[j] = aid
					n++
				}
			}
			if n == 0 {
				is.addf(field(ap, "role"), "formation %q has no unassigned slot with role %q", f.name, pa.Role)
			}
		}
	}
	return p
}
