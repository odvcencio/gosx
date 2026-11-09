package teamai

// maxMarks bounds the distinct mark names in a Book: one bit each in an
// agent's state.
const maxMarks = 32

// PathSpec is a named sequence of segments.
type PathSpec struct {
	Name     string        `json:"name"`
	Segments []SegmentSpec `json:"segments"`
}

// SegmentSpec is one segment of a path. Points are (lateral, depth) in the
// agent's frame at path start; the lateral part is signed by the agent's slot
// side.
//
//	line    To (from the path origin), By (from the segment start), or
//	        Landmark plus Offset; Track follows a moving landmark
//	curve   By and Control, both from the segment start (a quadratic Bezier)
//	hold    stand still for Duration seconds
//	decide  pick the next segment with Branches (see DecisionSpec)
type SegmentSpec struct {
	Label    string  `json:"label,omitempty"`
	Kind     string  `json:"kind"`
	To       *Vec2   `json:"to,omitempty"`
	By       *Vec2   `json:"by,omitempty"`
	Control  *Vec2   `json:"control,omitempty"`
	Landmark string  `json:"landmark,omitempty"`
	Offset   Vec2    `json:"offset,omitempty"`
	Track    bool    `json:"track,omitempty"`
	Speed    float64 `json:"speed,omitempty"`
	// EndSpeed is the speed fraction at the end of a line or curve; the
	// speed changes linearly with distance. Unset means Speed.
	EndSpeed *float64 `json:"end_speed,omitempty"`
	// Duration is the hold time, or the time to run a line or curve, which
	// replaces Speed.
	Duration float64 `json:"duration,omitempty"`
	// NotBefore delays the segment until this many seconds after the path
	// started; the agent holds until then.
	NotBefore float64 `json:"not_before,omitempty"`
	// Mark is set, and reported as an event, when the segment completes.
	Mark string `json:"mark,omitempty"`
	// Next is the label to continue at. Empty means the following segment;
	// the path ends after its last segment.
	Next string `json:"next,omitempty"`
	DecisionSpec
}

// markTable collects mark names in order of first use.
type markTable struct {
	names []string
}

func (m *markTable) id(is *issues, path, name string) int16 {
	if name == "" {
		return -1
	}
	if !validName(name) {
		is.addf(path, "mark %q: use 1 to %d characters from A-Z a-z 0-9 _ -", name, maxNameLen)
		return -1
	}
	if i := indexOf(m.names, name); i >= 0 {
		return int16(i)
	}
	if len(m.names) == maxMarks {
		is.addf(path, "more than %d distinct marks", maxMarks)
		return -1
	}
	m.names = append(m.names, name)
	return int16(len(m.names) - 1)
}

func compilePath(is *issues, path string, ps *PathSpec, b *Book, marks *markTable, rb *ruleBinder) Path {
	p := Path{name: ps.Name}
	if len(ps.Segments) == 0 {
		is.addf(field(path, "segments"), "a path needs at least one segment")
		return p
	}
	if len(ps.Segments) > maxSegments {
		is.addf(field(path, "segments"), "%d segments; at most %d", len(ps.Segments), maxSegments)
		return p
	}
	labels := func(name string) int {
		for i := range ps.Segments {
			if ps.Segments[i].Label == name {
				return i
			}
		}
		return -1
	}
	p.segs = make([]segment, len(ps.Segments))
	for i := range ps.Segments {
		ss := &ps.Segments[i]
		sp := at(path, "segments", i, ss.Label)
		s := &p.segs[i]
		s.label = ss.Label
		s.landmark = -1
		s.mark = marks.id(is, field(sp, "mark"), ss.Mark)
		next := int16(i + 1)
		if i == len(ps.Segments)-1 {
			next = -1
		}
		s.next = next
		if ss.Label != "" {
			if !validName(ss.Label) {
				is.addf(field(sp, "label"), "label %q: use 1 to %d characters from A-Z a-z 0-9 _ -", ss.Label, maxNameLen)
			} else if j := labels(ss.Label); j != i {
				is.addf(field(sp, "label"), "duplicate label; first used at segments[%d]", j)
			}
		}
		if ss.Next != "" {
			if j := labels(ss.Next); j >= 0 {
				s.next = int16(j)
			} else {
				is.addf(field(sp, "next"), "unknown label %q", ss.Next)
			}
		}
		checkSeconds(is, field(sp, "duration"), ss.Duration)
		checkSeconds(is, field(sp, "not_before"), ss.NotBefore)
		s.duration = ss.Duration
		s.notBefore = ss.NotBefore
		s.speed = ss.Speed
		if s.speed == 0 {
			s.speed = 1
		}
		s.endSpeed = s.speed
		if ss.EndSpeed != nil {
			s.endSpeed = *ss.EndSpeed
		}
		checkFraction(is, field(sp, "speed"), ss.Speed)
		if ss.EndSpeed != nil {
			checkFraction(is, field(sp, "end_speed"), *ss.EndSpeed)
		}
		hasDecision := len(ss.Branches) > 0 || ss.Chooser != "" || ss.Window != 0 || ss.Mode != "" || ss.Otherwise != ""
		geometry := ss.To != nil || ss.By != nil || ss.Control != nil || ss.Landmark != "" || !ss.Offset.IsZero() || ss.Track

		switch ss.Kind {
		case "line":
			s.kind = SegmentLine
			n := 0
			if ss.To != nil {
				n++
				s.to = *ss.To
				checkVec(is, field(sp, "to"), *ss.To)
			}
			if ss.By != nil {
				n++
				s.to = *ss.By
				s.relative = true
				checkVec(is, field(sp, "by"), *ss.By)
			}
			if ss.Landmark != "" {
				n++
				id, ok := b.Landmark(ss.Landmark)
				if !ok {
					is.addf(field(sp, "landmark"), "unknown landmark %q", ss.Landmark)
				}
				s.landmark = id
				s.to = ss.Offset
				s.track = ss.Track
				checkVec(is, field(sp, "offset"), ss.Offset)
			} else if !ss.Offset.IsZero() || ss.Track {
				is.addf(sp, "offset and track apply to a landmark line only")
			}
			if n != 1 {
				is.addf(sp, "a line needs exactly one of to, by or landmark")
			}
			if ss.Control != nil {
				is.addf(field(sp, "control"), "control applies to a curve only")
			}
			if hasDecision {
				is.addf(sp, "branches, window, mode, chooser and otherwise apply to a decide segment only")
			}
		case "curve":
			s.kind = SegmentCurve
			if ss.By == nil || ss.Control == nil {
				is.addf(sp, "a curve needs by and control, both from the segment start")
				break
			}
			if ss.To != nil || ss.Landmark != "" || !ss.Offset.IsZero() || ss.Track {
				is.addf(sp, "a curve takes only by and control")
			}
			checkVec(is, field(sp, "by"), *ss.By)
			checkVec(is, field(sp, "control"), *ss.Control)
			s.to = *ss.By
			s.control = *ss.Control
			s.poly, s.lens = bezierPoly(*ss.Control, *ss.By)
			if hasDecision {
				is.addf(sp, "branches, window, mode, chooser and otherwise apply to a decide segment only")
			}
		case "hold":
			s.kind = SegmentHold
			if geometry {
				is.addf(sp, "a hold takes no points; it stands where the previous segment ended")
			}
			if ss.Duration <= 0 {
				is.addf(field(sp, "duration"), "a hold needs a duration in seconds")
			}
			if hasDecision {
				is.addf(sp, "branches, window, mode, chooser and otherwise apply to a decide segment only")
			}
		case "decide":
			s.kind = SegmentDecide
			if geometry {
				is.addf(sp, "a decide segment takes no points")
			}
			if ss.Mark != "" || ss.Duration != 0 {
				is.addf(sp, "a decide segment takes no mark or duration")
			}
			s.decision = compileDecision(is, sp, &ss.DecisionSpec, labels, next, rb)
			if ss.Next != "" {
				is.addf(field(sp, "next"), "a decide segment continues through its branches or otherwise, not next")
			}
		case "":
			is.addf(field(sp, "kind"), "missing kind: want line, curve, hold or decide")
		default:
			is.addf(field(sp, "kind"), "kind %q: want line, curve, hold or decide", ss.Kind)
		}
	}
	return p
}

func checkSeconds(is *issues, path string, v float64) {
	if !finite(v) || v < 0 || v > 86400 {
		is.addf(path, "%v: want a finite number of seconds from 0 to 86400", v)
	}
}

// checkFraction checks a speed fraction of MaxSpeed.
func checkFraction(is *issues, path string, v float64) {
	if !finite(v) || v < 0 || v > 1 {
		is.addf(path, "%v: want a fraction of the agent's top speed from 0 to 1", v)
	}
}
