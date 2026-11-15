package teamai

// FormationID indexes Book formations. NoFormation is -1.
type FormationID int16

// NoFormation marks a team with no formation.
const NoFormation FormationID = -1

// Formation is a compiled set of alignment slots. Get one from
// [Book.Formation].
type Formation struct {
	name  string
	slots []slot
	// order lists slots so that every From reference comes first.
	order []int16
	// nameSlot maps a Book slot-name index (move steps) to a slot here, or
	// -1.
	nameSlot []int16
}

type slot struct {
	name  string
	role  RoleID
	alt   []RoleID
	at    Vec2
	side  Side
	from  int16
	edge  bool
	bands []Vec2 // indexed by band; zero offset when the band has none
	clamp bool
}

// SlotInfo describes one slot of a formation.
type SlotInfo struct {
	Name       string
	Role       RoleID
	Alternates []RoleID
	Side       Side
}

// Name returns the formation's name.
func (f *Formation) Name() string { return f.name }

// NumSlots returns the number of slots.
func (f *Formation) NumSlots() int { return len(f.slots) }

// Slot describes slot i.
func (f *Formation) Slot(i int) SlotInfo {
	s := &f.slots[i]
	return SlotInfo{Name: s.name, Role: s.role, Alternates: s.alt, Side: s.side}
}

// SlotIndex returns the index of the named slot, or -1.
func (f *Formation) SlotIndex(name string) int {
	for i := range f.slots {
		if f.slots[i].name == name {
			return i
		}
	}
	return -1
}

// SlotSign returns the lateral sign (+1 or -1) of slot i in frame fr. Paths and
// local landmarks an agent runs from that slot use the same sign, so a route
// authored for the right side mirrors with its slot.
func (f *Formation) SlotSign(fr *Frame, i int) float64 {
	return fr.Sign(f.slots[i].side)
}

// Resolve writes the world position of every slot in frame fr to dst and
// returns dst[:NumSlots()]. field bounds "edge" slots and clamped slots; a zero
// field puts edge slots at their offset from the origin. Resolve allocates only
// when dst is shorter than NumSlots().
func (f *Formation) Resolve(fr *Frame, field Rect, dst []Vec2) []Vec2 {
	n := len(f.slots)
	if cap(dst) < n {
		dst = make([]Vec2, n)
	}
	dst = dst[:n]
	fw := fr.Fwd()
	right := fr.Right()
	for _, i := range f.order {
		s := &f.slots[i]
		sign := fr.Sign(s.side)
		off := s.at
		if fr.Band >= 0 && fr.Band < len(s.bands) {
			off = off.Add(s.bands[fr.Band])
		}
		base := fr.Origin
		if s.from >= 0 {
			base = dst[s.from]
		}
		var p Vec2
		if s.edge {
			lat := off.X
			if edge := field.Exit(fr.Origin, right.Scale(sign)); finite(edge) {
				lat = edge - off.X
			}
			depth := base.Sub(fr.Origin).Dot(fw) + off.Y
			p = fr.Origin.AddScaled(right, float64(sign*lat)).AddScaled(fw, depth)
		} else {
			p = base.AddScaled(right, float64(sign*off.X)).AddScaled(fw, off.Y)
		}
		if s.clamp {
			p = field.Clamp(p)
		}
		dst[i] = p
	}
	return dst
}

// AssignByRole gives each slot of f an agent of team: dst[slot] is the index
// into agents, or -1 when no agent can fill it. It reports how many slots stay
// empty.
//
// Slots are filled in order. The first pass gives each slot the lowest-index
// free active agent whose Role is the slot's role; the second pass fills the
// empty slots from their alternate roles, in order. The result depends only on
// the arguments. used is scratch with at least len(agents) entries; it is
// cleared first. dst must have at least NumSlots() entries.
func AssignByRole(dst []int, f *Formation, agents []Body, team int, used []bool) int {
	return assignSlots(dst, f, agents, team, used, nil)
}

// AssignNearest is AssignByRole, except that each slot takes the free eligible
// agent nearest to its position in slots (from Resolve) instead of the lowest
// index. Ties go to the lower index. Use it to re-form a squad on the move.
func AssignNearest(dst []int, f *Formation, agents []Body, team int, used []bool, slots []Vec2) int {
	return assignSlots(dst, f, agents, team, used, slots)
}

func assignSlots(dst []int, f *Formation, agents []Body, team int, used []bool, pos []Vec2) int {
	n := len(f.slots)
	used = used[:len(agents)]
	for i := range used {
		used[i] = false
	}
	for i := 0; i < n; i++ {
		dst[i] = -1
	}
	for i := 0; i < n; i++ {
		dst[i] = pickAgent(agents, team, f.slots[i].role, used, pos, i)
	}
	empty := 0
	for i := 0; i < n; i++ {
		if dst[i] >= 0 {
			continue
		}
		for _, r := range f.slots[i].alt {
			if dst[i] = pickAgent(agents, team, r, used, pos, i); dst[i] >= 0 {
				break
			}
		}
		if dst[i] < 0 {
			empty++
		}
	}
	return empty
}

func pickAgent(agents []Body, team int, role RoleID, used []bool, pos []Vec2, slotIndex int) int {
	best := -1
	bestD := 0.0
	for j := range agents {
		a := &agents[j]
		if used[j] || a.Team != team || a.Role != role || !a.Active() {
			continue
		}
		if pos == nil {
			best = j
			break
		}
		d := a.Pos.Sub(pos[slotIndex]).Len2()
		if best < 0 || d < bestD {
			best, bestD = j, d
		}
	}
	if best >= 0 {
		used[best] = true
	}
	return best
}

// compileFormation checks one FormationSpec.
func compileFormation(is *issues, path string, fs *FormationSpec, roles, bands []string) Formation {
	f := Formation{name: fs.Name, slots: make([]slot, len(fs.Slots))}
	if len(fs.Slots) == 0 {
		is.addf(field(path, "slots"), "a formation needs at least one slot")
	}
	if len(fs.Slots) > maxSlots {
		is.addf(field(path, "slots"), "%d slots; at most %d", len(fs.Slots), maxSlots)
		return f
	}
	for i := range fs.Slots {
		ss := &fs.Slots[i]
		sp := at(path, "slots", i, ss.Name)
		s := &f.slots[i]
		s.name = ss.Name
		s.at = ss.At
		s.side = ss.Side
		s.clamp = ss.Clamp
		s.from = -1
		if !validName(ss.Name) {
			is.addf(field(sp, "name"), "name %q: use 1 to %d characters from A-Z a-z 0-9 _ -", ss.Name, maxNameLen)
		}
		for j := 0; j < i; j++ {
			if fs.Slots[j].Name == ss.Name {
				is.addf(field(sp, "name"), "duplicate slot name; first defined at slots[%d]", j)
				break
			}
		}
		s.role = RoleID(indexOf(roles, ss.Role))
		if s.role < 0 {
			is.addf(field(sp, "role"), "unknown role %q", ss.Role)
		}
		for k, a := range ss.Alternates {
			r := RoleID(indexOf(roles, a))
			if r < 0 {
				is.addf(field(sp, "alternates["+itoa(k)+"]"), "unknown role %q", a)
				continue
			}
			s.alt = append(s.alt, r)
		}
		checkVec(is, field(sp, "at"), ss.At)
		switch ss.Lateral {
		case "", "origin":
		case "edge":
			s.edge = true
		default:
			is.addf(field(sp, "lateral"), "lateral %q: want origin or edge", ss.Lateral)
		}
		if ss.Side > SideWeak || ss.Side < SideFixed {
			is.addf(field(sp, "side"), "side %d: want fixed, strong or weak", ss.Side)
		}
		if ss.From != "" {
			j := -1
			for k := range fs.Slots {
				if fs.Slots[k].Name == ss.From {
					j = k
					break
				}
			}
			switch {
			case j < 0:
				is.addf(field(sp, "from"), "unknown slot %q", ss.From)
			case j == i:
				is.addf(field(sp, "from"), "a slot cannot be placed from itself")
			default:
				s.from = int16(j)
			}
		}
		if len(ss.Bands) > 0 {
			s.bands = make([]Vec2, len(bands))
		}
		for k := range ss.Bands {
			bo := &ss.Bands[k]
			bp := at(sp, "bands", k, bo.Band)
			b := indexOf(bands, bo.Band)
			if b < 0 {
				is.addf(field(bp, "band"), "unknown band %q", bo.Band)
				continue
			}
			checkVec(is, field(bp, "offset"), bo.Offset)
			s.bands[b] = bo.Offset
		}
	}
	f.order = slotOrder(is, path, &f)
	return f
}

// slotOrder sorts slots so each From comes before its dependents, and reports
// cycles.
func slotOrder(is *issues, path string, f *Formation) []int16 {
	n := len(f.slots)
	order := make([]int16, 0, n)
	state := make([]uint8, n) // 0 new, 1 visiting, 2 done
	var visit func(i int) bool
	visit = func(i int) bool {
		switch state[i] {
		case 1:
			return false
		case 2:
			return true
		}
		state[i] = 1
		if from := f.slots[i].from; from >= 0 && !visit(int(from)) {
			return false
		}
		state[i] = 2
		order = append(order, int16(i))
		return true
	}
	for i := 0; i < n; i++ {
		if !visit(i) {
			is.addf(at(path, "slots", i, f.slots[i].name)+".from", "slots placed from each other form a cycle")
			for j := range state {
				if state[j] == 1 {
					state[j] = 2
					f.slots[j].from = -1
					order = append(order, int16(j))
				}
			}
		}
	}
	return order
}
