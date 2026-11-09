package speech

// Mix is a Sink that renders the timeline to mono float32 PCM. It is the
// reference the browser path is checked against: the same fade shapes, the
// same whole-frame starts. Mix suits tests and offline rendering; clips must
// carry a []float32 in Clip.Handle.
type Mix struct {
	segs  []mixSeg
	lines []mixLine
}

type mixSeg struct {
	line uint64
	seg  Segment
}

type mixLine struct {
	id    uint64
	cutAt int64
	fade  int32
	cut   bool
}

// BeginLine implements Sink.
func (m *Mix) BeginLine(line uint64) { m.lines = append(m.lines, mixLine{id: line}) }

// Schedule implements Sink.
func (m *Mix) Schedule(line uint64, seg Segment) {
	m.segs = append(m.segs, mixSeg{line: line, seg: seg})
}

// CutLine implements Sink.
func (m *Mix) CutLine(line uint64, at int64, fade int32) {
	for i := range m.lines {
		if m.lines[i].id == line {
			m.lines[i].cut, m.lines[i].cutAt, m.lines[i].fade = true, at, fade
		}
	}
}

// Reset forgets every scheduled segment.
func (m *Mix) Reset() { m.segs, m.lines = m.segs[:0], m.lines[:0] }

// Render returns frames [from, from+n) of the mix.
func (m *Mix) Render(from int64, n int) []float32 {
	out := make([]float32, n)
	for i := range m.segs {
		ms := &m.segs[i]
		s := &ms.seg
		data, _ := s.Clip.Handle.([]float32)
		lo, hi := s.Start, s.End()
		if hi <= from || lo >= from+int64(n) {
			continue
		}
		var ln *mixLine
		for j := range m.lines {
			if m.lines[j].id == ms.line {
				ln = &m.lines[j]
			}
		}
		for f := max(lo, from); f < min(hi, from+int64(n)); f++ {
			k := f - lo
			g := 1.0
			if fi := int64(s.FadeIn); fi > 0 && k < fi {
				g = float64(k) / float64(fi)
			}
			if fo := int64(s.FadeOut); fo > 0 && int64(s.Clip.Frames)-k <= fo {
				g = min(g, float64(int64(s.Clip.Frames)-k)/float64(fo))
			}
			if ln != nil && ln.cut && f >= ln.cutAt {
				if f >= ln.cutAt+int64(ln.fade) {
					continue
				}
				g *= 1 - float64(f-ln.cutAt)/float64(ln.fade)
			}
			var v float32 = 1
			if int(k) < len(data) {
				v = data[k]
			}
			out[f-from] += float32(g) * v
		}
	}
	return out
}
