package teamai

import "strconv"

// Table limits. They keep compiled indices in int16 and per-agent state small.
const (
	maxTable = 4096 // entries per table (roles, formations, paths, ...)
	maxSlots = 64   // slots per formation
)

// LandmarkID indexes Book landmarks and World.Landmarks.
type LandmarkID int16

// Book is a compiled, validated Spec. It is read-only after Compile and safe to
// share between executors and goroutines.
type Book struct {
	roles      []string
	bands      []string
	landmarks  []landmark
	formations []Formation
}

type landmark struct {
	name string
	host bool
	at   Vec2
	side Side
}

// Compile validates spec and returns its Book. On failure the error is a
// *SpecError that lists every problem with the path of the offending entry.
func Compile(spec *Spec) (*Book, error) {
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
	b := &Book{}
	checkTable(&is, "roles", len(spec.Roles))
	checkTable(&is, "bands", len(spec.Bands))
	checkTable(&is, "landmarks", len(spec.Landmarks))
	checkTable(&is, "formations", len(spec.Formations))
	if err := is.err(); err != nil {
		return nil, err
	}
	b.roles = nameTable(&is, "roles", spec.Roles)
	b.bands = nameTable(&is, "bands", spec.Bands)

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
	if err := is.err(); err != nil {
		return nil, err
	}
	return b, nil
}

func checkTable(is *issues, name string, n int) {
	if n > maxTable {
		is.addf(name, "%d entries; at most %d", n, maxTable)
	}
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
// fr: World.Landmarks[id] for a host landmark, otherwise the authored point in
// fr. hosted is World.Landmarks.
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
