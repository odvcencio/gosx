package teamai

import "strconv"

// Table limits. They keep compiled indices in int16 and per-agent state small.
const (
	maxTable = 4096 // entries per table (roles, formations, paths, ...)
	maxSlots = 64   // slots per formation
)

// LandmarkID indexes Book landmarks and World.Landmarks.
type LandmarkID int16

// Book is a compiled, validated Spec. Its tables are read-only after Compile.
// A Book can be shared by executors on different goroutines when the
// registered predicates and choosers it holds are safe for concurrent use;
// the built-in data rules are.
type Book struct {
	roles        []string
	bands        []string
	objects      []string
	signals      []string
	teamSignals  []string
	agentSignals []string
	landmarks    []landmark
	formations   []Formation
	slotNames    []string // every slot name used by a move step
	paths        []Path
	assignments  []Assignment
	plans        []Plan
	marks        []string
	// preds and choosers are indexed by Decision branch rules, step untils
	// and interrupts.
	predNames []string
	preds     []Predicate
	chNames   []string
	choosers  []Chooser
}

type landmark struct {
	name string
	host bool
	at   Vec2
	side Side
}

// Compile validates spec and returns its Book. reg supplies the predicates and
// choosers written in Go; it can be nil. On failure the error is a *SpecError
// that lists every problem with the path of the offending entry.
func Compile(spec *Spec, reg *Registry) (*Book, error) {
	var is issues
	switch {
	case spec.Format != Format:
		is.addf("format", "format %q: want %q", spec.Format, Format)
	case spec.Version < 1:
		is.addf("version", "version %d: want 1", spec.Version)
	case spec.Version > Version:
		is.addf("version", "version %d is newer than this build reads (%d)", spec.Version, Version)
	}
	if err := is.err(); err != nil {
		return nil, err
	}
	for _, t := range []struct {
		name string
		n    int
	}{
		{"roles", len(spec.Roles)}, {"bands", len(spec.Bands)}, {"objects", len(spec.Objects)},
		{"signals", len(spec.Signals)}, {"team_signals", len(spec.TeamSignals)},
		{"agent_signals", len(spec.AgentSignals)}, {"landmarks", len(spec.Landmarks)},
		{"formations", len(spec.Formations)}, {"paths", len(spec.Paths)}, {"rules", len(spec.Rules)},
		{"assignments", len(spec.Assignments)}, {"plans", len(spec.Plans)},
	} {
		if t.n > maxTable {
			is.addf(t.name, "%d entries; at most %d", t.n, maxTable)
		}
	}
	if err := is.err(); err != nil {
		return nil, err
	}
	b := &Book{}
	b.roles = nameTable(&is, "roles", spec.Roles)
	b.bands = nameTable(&is, "bands", spec.Bands)
	b.objects = nameTable(&is, "objects", spec.Objects)
	b.signals = nameTable(&is, "signals", spec.Signals)
	b.teamSignals = nameTable(&is, "team_signals", spec.TeamSignals)
	b.agentSignals = nameTable(&is, "agent_signals", spec.AgentSignals)

	b.landmarks = make([]landmark, len(spec.Landmarks))
	for i := range spec.Landmarks {
		ls := &spec.Landmarks[i]
		p := at("", "landmarks", i, ls.Name)
		checkEntryName(&is, p, ls.Name, i, func(j int) string { return spec.Landmarks[j].Name })
		checkVec(&is, field(p, "at"), ls.At)
		if ls.Host && !ls.At.IsZero() {
			is.addf(field(p, "at"), "a host landmark takes its position from World.Landmarks; remove at")
		}
		b.landmarks[i] = landmark{name: ls.Name, host: ls.Host, at: ls.At, side: ls.Side}
	}

	b.formations = make([]Formation, len(spec.Formations))
	for i := range spec.Formations {
		fs := &spec.Formations[i]
		p := at("", "formations", i, fs.Name)
		checkEntryName(&is, p, fs.Name, i, func(j int) string { return spec.Formations[j].Name })
		b.formations[i] = compileFormation(&is, p, fs, b.roles, b.bands)
	}

	var marks markTable
	var rb ruleBinder
	b.paths = make([]Path, len(spec.Paths))
	for i := range spec.Paths {
		ps := &spec.Paths[i]
		p := at("", "paths", i, ps.Name)
		checkEntryName(&is, p, ps.Name, i, func(j int) string { return spec.Paths[j].Name })
		b.paths[i] = compilePath(&is, p, ps, b, &marks, &rb)
	}

	// Assignment names are needed by plans, and steps name slots of any
	// formation, so collect both before compiling.
	b.assignments = make([]Assignment, len(spec.Assignments))
	for i := range spec.Assignments {
		b.assignments[i].name = spec.Assignments[i].Name
	}
	for i := range spec.Assignments {
		as := &spec.Assignments[i]
		p := at("", "assignments", i, as.Name)
		checkEntryName(&is, p, as.Name, i, func(j int) string { return spec.Assignments[j].Name })
		b.assignments[i] = compileAssignment(&is, p, as, b, &marks, &rb)
	}
	for i := range b.formations {
		f := &b.formations[i]
		f.nameSlot = make([]int16, len(b.slotNames))
		for k, n := range b.slotNames {
			f.nameSlot[k] = int16(f.SlotIndex(n))
		}
	}

	b.plans = make([]Plan, len(spec.Plans))
	for i := range spec.Plans {
		ps := &spec.Plans[i]
		p := at("", "plans", i, ps.Name)
		checkEntryName(&is, p, ps.Name, i, func(j int) string { return spec.Plans[j].Name })
		b.plans[i] = compilePlan(&is, p, ps, b)
	}
	b.marks = marks.names

	// Rules last: facts can name marks, objects and signals.
	compileRules(&is, b, spec.Rules, reg, &rb)
	b.predNames = rb.preds
	b.chNames = rb.choosers
	if err := is.err(); err != nil {
		return nil, err
	}
	return b, nil
}

// slotName returns the index of a slot name used by a move step, adding it
// when some formation has it, or -1 when none does.
func (b *Book) slotName(name string) int {
	if i := indexOf(b.slotNames, name); i >= 0 {
		return i
	}
	for i := range b.formations {
		if b.formations[i].SlotIndex(name) >= 0 {
			b.slotNames = append(b.slotNames, name)
			return len(b.slotNames) - 1
		}
	}
	return -1
}

// checkEntryName checks the name of entry i and that no earlier entry has it.
func checkEntryName(is *issues, path, name string, i int, nameAt func(int) string) {
	if !validName(name) {
		is.addf(field(path, "name"), "name %q: use 1 to %d characters from A-Z a-z 0-9 _ -", name, maxNameLen)
		return
	}
	for j := 0; j < i; j++ {
		if nameAt(j) == name {
			is.addf(field(path, "name"), "duplicate name; first defined at index %d", j)
			return
		}
	}
}

func itoa(i int) string { return strconv.Itoa(i) }

// NumRoles returns the number of roles.
func (b *Book) NumRoles() int { return len(b.roles) }

// Role returns the ID of the named role.
func (b *Book) Role(name string) (RoleID, bool) {
	i := indexOf(b.roles, name)
	return RoleID(i), i >= 0
}

// RoleName returns the name of role r, or "" when r is out of range.
func (b *Book) RoleName(r RoleID) string {
	if r < 0 || int(r) >= len(b.roles) {
		return ""
	}
	return b.roles[r]
}

// Band returns the index of the named band, for Frame.Band.
func (b *Book) Band(name string) (int, bool) {
	i := indexOf(b.bands, name)
	return i, i >= 0
}

// Object returns the index of the named object in World.Objects.
func (b *Book) Object(name string) (int, bool) {
	i := indexOf(b.objects, name)
	return i, i >= 0
}

// NumObjects returns the length World.Objects must have.
func (b *Book) NumObjects() int { return len(b.objects) }

// Signal returns the index of the named signal in World.Signals.
func (b *Book) Signal(name string) (int, bool) {
	i := indexOf(b.signals, name)
	return i, i >= 0
}

// NumSignals returns the length World.Signals must have.
func (b *Book) NumSignals() int { return len(b.signals) }

// TeamSignal returns the index of the named team signal.
func (b *Book) TeamSignal(name string) (int, bool) {
	i := indexOf(b.teamSignals, name)
	return i, i >= 0
}

// NumTeamSignals returns the number of team signals per team.
func (b *Book) NumTeamSignals() int { return len(b.teamSignals) }

// AgentSignal returns the index of the named agent signal.
func (b *Book) AgentSignal(name string) (int, bool) {
	i := indexOf(b.agentSignals, name)
	return i, i >= 0
}

// NumAgentSignals returns the number of agent signals per agent.
func (b *Book) NumAgentSignals() int { return len(b.agentSignals) }

// NumLandmarks returns the number of landmarks: the length World.Landmarks
// must have.
func (b *Book) NumLandmarks() int { return len(b.landmarks) }

// Landmark returns the ID of the named landmark.
func (b *Book) Landmark(name string) (LandmarkID, bool) {
	for i := range b.landmarks {
		if b.landmarks[i].name == name {
			return LandmarkID(i), true
		}
	}
	return -1, false
}

// LandmarkName returns the name of landmark id.
func (b *Book) LandmarkName(id LandmarkID) string {
	if id < 0 || int(id) >= len(b.landmarks) {
		return ""
	}
	return b.landmarks[id].name
}

// LandmarkPos returns the world position of landmark id for a team in frame
// fr: hosted[id] (World.Landmarks) for a host landmark, otherwise the authored
// point in fr.
func (b *Book) LandmarkPos(id LandmarkID, fr *Frame, hosted []Vec2) Vec2 {
	l := &b.landmarks[id]
	if l.host {
		if int(id) < len(hosted) {
			return hosted[id]
		}
		return Vec2{}
	}
	return fr.ToWorld(Vec2{X: float64(l.at.X * fr.Sign(l.side)), Y: l.at.Y})
}

// NumFormations returns the number of formations.
func (b *Book) NumFormations() int { return len(b.formations) }

// FormationID returns the ID of the named formation.
func (b *Book) FormationID(name string) (FormationID, bool) {
	for i := range b.formations {
		if b.formations[i].name == name {
			return FormationID(i), true
		}
	}
	return NoFormation, false
}

// Formation returns formation id, or nil when id is out of range.
func (b *Book) Formation(id FormationID) *Formation {
	if id < 0 || int(id) >= len(b.formations) {
		return nil
	}
	return &b.formations[id]
}

// NumPaths returns the number of paths.
func (b *Book) NumPaths() int { return len(b.paths) }

// PathID returns the ID of the named path.
func (b *Book) PathID(name string) (PathID, bool) {
	for i := range b.paths {
		if b.paths[i].name == name {
			return PathID(i), true
		}
	}
	return NoPath, false
}

// Path returns path id, or nil when id is out of range.
func (b *Book) Path(id PathID) *Path {
	if id < 0 || int(id) >= len(b.paths) {
		return nil
	}
	return &b.paths[id]
}

// NumAssignments returns the number of assignments.
func (b *Book) NumAssignments() int { return len(b.assignments) }

// AssignmentID returns the ID of the named assignment.
func (b *Book) AssignmentID(name string) (AssignmentID, bool) {
	for i := range b.assignments {
		if b.assignments[i].name == name {
			return AssignmentID(i), true
		}
	}
	return NoAssignment, false
}

// Assignment returns assignment id, or nil when id is out of range.
func (b *Book) Assignment(id AssignmentID) *Assignment {
	if id < 0 || int(id) >= len(b.assignments) {
		return nil
	}
	return &b.assignments[id]
}

// NumPlans returns the number of plans.
func (b *Book) NumPlans() int { return len(b.plans) }

// PlanID returns the ID of the named plan.
func (b *Book) PlanID(name string) (PlanID, bool) {
	for i := range b.plans {
		if b.plans[i].name == name {
			return PlanID(i), true
		}
	}
	return -1, false
}

// Plan returns plan id, or nil when id is out of range.
func (b *Book) Plan(id PlanID) *Plan {
	if id < 0 || int(id) >= len(b.plans) {
		return nil
	}
	return &b.plans[id]
}

// NumMarks returns the number of distinct mark names.
func (b *Book) NumMarks() int { return len(b.marks) }

// Mark returns the index of the named mark, as reported in EventMark events.
func (b *Book) Mark(name string) (int, bool) {
	i := indexOf(b.marks, name)
	return i, i >= 0
}

// MarkName returns the name of mark i.
func (b *Book) MarkName(i int) string {
	if i < 0 || i >= len(b.marks) {
		return ""
	}
	return b.marks[i]
}
