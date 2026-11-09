package teamai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Format is the value of Spec.Format in every teamai data file.
const Format = "gosx.teamai"

// Version is the newest data format version this package reads.
const Version = 1

// Spec is the data a game authors: roles, formations, paths, rules,
// assignments and plans. It is the shape of the JSON data file (see
// docs/team-ai.md) and can also be built in Go. [Compile] validates it and
// returns a [Book].
type Spec struct {
	// Format must be "gosx.teamai".
	Format string `json:"format"`
	// Version is the data format version, 1.
	Version int `json:"version"`
	// Roles names the roles agents can have. Body.Role indexes this list.
	Roles []string `json:"roles"`
	// Bands names the lateral bands a frame origin can be in. Frame.Band
	// indexes this list.
	Bands []string `json:"bands,omitempty"`
	// Landmarks are named points: host-placed, or local to a team's frame.
	Landmarks []LandmarkSpec `json:"landmarks,omitempty"`
	// Formations are sets of alignment slots.
	Formations []FormationSpec `json:"formations,omitempty"`
}

// LandmarkSpec is a named point. A host landmark takes its position from
// World.Landmarks each tick. Any other landmark is At in the agent's team frame,
// signed by Side.
type LandmarkSpec struct {
	Name string `json:"name"`
	Host bool   `json:"host,omitempty"`
	At   Vec2   `json:"at,omitempty"`
	Side Side   `json:"side,omitempty"`
}

// FormationSpec is a named set of slots.
type FormationSpec struct {
	Name  string     `json:"name"`
	Slots []SlotSpec `json:"slots"`
}

// SlotSpec is one alignment slot of a formation.
//
// At is (lateral, depth) in the frame, before signing: the lateral part is
// multiplied by the slot's side sign (see [Frame.Sign]). With From set, the
// slot is placed relative to another slot of the same formation; otherwise
// relative to the frame origin. With Lateral "edge", the lateral part of At is
// the distance inward from the field edge on the slot's side, measured on the
// origin's line, and depth still follows From.
type SlotSpec struct {
	Name string `json:"name"`
	Role string `json:"role"`
	// Alternates are roles that can fill the slot when no agent with Role is
	// free, tried in order.
	Alternates []string `json:"alternates,omitempty"`
	At         Vec2     `json:"at"`
	Side       Side     `json:"side,omitempty"`
	From       string   `json:"from,omitempty"`
	// Lateral is "origin" (the default) or "edge".
	Lateral string `json:"lateral,omitempty"`
	// Bands adds an offset, signed like At, when the frame is in that band.
	Bands []BandOffsetSpec `json:"bands,omitempty"`
	// Clamp keeps the slot inside World.Field.
	Clamp bool `json:"clamp,omitempty"`
}

// BandOffsetSpec is a slot offset that applies in one lateral band.
type BandOffsetSpec struct {
	Band   string `json:"band"`
	Offset Vec2   `json:"offset"`
}

// MarshalJSON writes v as [x, y].
func (v Vec2) MarshalJSON() ([]byte, error) {
	b := make([]byte, 0, 32)
	b = append(b, '[')
	b = strconv.AppendFloat(b, v.X, 'g', -1, 64)
	b = append(b, ',')
	b = strconv.AppendFloat(b, v.Y, 'g', -1, 64)
	return append(b, ']'), nil
}

// UnmarshalJSON reads [x, y].
func (v *Vec2) UnmarshalJSON(b []byte) error {
	var a []float64
	if err := json.Unmarshal(b, &a); err != nil {
		return fmt.Errorf("want a two-number array [x, y]")
	}
	if len(a) != 2 {
		return fmt.Errorf("want a two-number array [x, y], got %d numbers", len(a))
	}
	v.X, v.Y = a[0], a[1]
	return nil
}

// MarshalText writes the data-file name of s.
func (s Side) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText reads "fixed", "strong" or "weak".
func (s *Side) UnmarshalText(b []byte) error {
	switch string(b) {
	case "", "fixed":
		*s = SideFixed
	case "strong":
		*s = SideStrong
	case "weak":
		*s = SideWeak
	default:
		return fmt.Errorf("side %q: want fixed, strong or weak", string(b))
	}
	return nil
}

// Issue is one problem found in a Spec. Path names the offending entry, as in
// formations[1](wedge).slots[2](wing).role.
type Issue struct {
	Path string
	Msg  string
}

func (i Issue) String() string {
	if i.Path == "" {
		return i.Msg
	}
	return i.Path + ": " + i.Msg
}

// SpecError lists every problem Compile or LoadJSON found.
type SpecError struct {
	Issues []Issue
}

// maxReportedIssues bounds the Error text, not the Issues slice.
const maxReportedIssues = 8

func (e *SpecError) Error() string {
	var b strings.Builder
	b.WriteString("teamai: ")
	for i, is := range e.Issues {
		if i == maxReportedIssues {
			fmt.Fprintf(&b, "; and %d more", len(e.Issues)-i)
			break
		}
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(is.String())
	}
	return b.String()
}

// issues collects problems during compilation.
type issues struct {
	list []Issue
}

func (s *issues) addf(path, format string, args ...any) {
	s.list = append(s.list, Issue{Path: path, Msg: fmt.Sprintf(format, args...)})
}

func (s *issues) err() error {
	if len(s.list) == 0 {
		return nil
	}
	return &SpecError{Issues: s.list}
}

// at formats an entry path: table[i](name).
func at(parent, table string, i int, name string) string {
	p := table + "[" + strconv.Itoa(i) + "]"
	if name != "" {
		p += "(" + name + ")"
	}
	if parent == "" {
		return p
	}
	return parent + "." + p
}

func field(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "." + name
}

// maxNameLen bounds every name in a Spec.
const maxNameLen = 64

// validName reports whether s is a usable name: 1 to 64 characters from
// [A-Za-z0-9_-]. Names cannot contain '.', because rules address facts as
// "object.<name>.dist".
func validName(s string) bool {
	if s == "" || len(s) > maxNameLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// nameTable checks a list of names and returns them for lookup.
func nameTable(is *issues, path string, names []string) []string {
	for i, n := range names {
		if !validName(n) {
			is.addf(at("", path, i, ""), "name %q: use 1 to %d characters from A-Z a-z 0-9 _ -", n, maxNameLen)
			continue
		}
		for j := 0; j < i; j++ {
			if names[j] == n {
				is.addf(at("", path, i, n), "duplicate name; first defined at %s[%d]", path, j)
				break
			}
		}
	}
	return names
}

func indexOf(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

func checkVec(is *issues, path string, v Vec2) {
	if !v.finite() {
		is.addf(path, "must be finite numbers")
	}
}
