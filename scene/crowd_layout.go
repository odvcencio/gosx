package scene

import (
	"fmt"

	"m31labs.dev/gosx/det"
)

// CrowdSeat is one occupied seat of a Layout.
type CrowdSeat struct {
	// Index is the fan's position in Layout.Seats, which is also its instance
	// order in the browser.
	Index int
	// Seat numbers every seat of the crowd, filled or not, so changing Fill
	// never moves the other fans.
	Seat int
	// Section is the crowd-wide section index; Stand and Row locate the seat.
	Section int
	Stand   int
	Row     int
	// Position is the seat in the crowd frame. Y is the row's floor.
	Position Vector3
	// Yaw is the facing in radians: the fan looks along (sin Yaw, 0, cos Yaw).
	Yaw float64
	// Wave is the seat's azimuth around the crowd origin in turns, in [0, 1).
	// Travelling waves move along it.
	Wave float64
	// Seed is the fan's variation seed. The fields below derive from it.
	Seed uint32
	// Color and Accent are the fan's shirt and accent palette slots in its
	// section.
	Color  int
	Accent int
	// Prop is what the fan holds.
	Prop CrowdProp
	// Variant picks the body height and skin tone.
	Variant uint8
	// Phase offsets the fan's free-running animation, out of 65536 cycles.
	Phase uint16
	// Eagerness orders how readily the fan joins an action: a section at
	// excitement e has every fan with Eagerness < 64 + 192*e joining in.
	Eagerness uint8
}

// CrowdSectionLayout summarizes one section of a Layout.
type CrowdSectionLayout struct {
	ID    string
	Index int
	Stand int
	// First and Count are the section's range in Layout.Seats.
	First, Count int
	// Center is the mean seat position in the crowd frame, and Radius the
	// largest distance from it to a seat.
	Center Vector3
	Radius float64
}

// CrowdLayout is the expanded crowd: every occupied seat in instance order,
// grouped by section.
type CrowdLayout struct {
	Seats    []CrowdSeat
	Sections []CrowdSectionLayout
}

// Layout expands the crowd's seats. It reports the same errors as IR.
func (c Crowd) Layout() (CrowdLayout, error) {
	ir, err := c.IR()
	if err != nil {
		return CrowdLayout{}, err
	}
	return ir.Layout()
}

// Layout expands the seats of a normalized crowd.
func (ir CrowdIR) Layout() (CrowdLayout, error) {
	out := CrowdLayout{Seats: make([]CrowdSeat, 0, ir.Seats), Sections: make([]CrowdSectionLayout, len(ir.Sections))}
	for i, section := range ir.Sections {
		out.Sections[i] = CrowdSectionLayout{ID: section.ID, Index: i, Stand: section.Stand, First: -1}
	}
	err := layoutCrowdIR(ir, func(seat CrowdSeat) bool {
		if len(out.Seats) >= CrowdMaxSeats {
			return false
		}
		out.Seats = append(out.Seats, seat)
		return true
	})
	if err != nil {
		return CrowdLayout{}, err
	}
	for i := range out.Seats {
		seat := &out.Seats[i]
		section := &out.Sections[seat.Section]
		if section.First < 0 {
			section.First = i
		}
		section.Count++
		section.Center.X += seat.Position.X
		section.Center.Y += seat.Position.Y
		section.Center.Z += seat.Position.Z
	}
	for i := range out.Sections {
		section := &out.Sections[i]
		if section.First < 0 {
			section.First = len(out.Seats)
			continue
		}
		n := float64(section.Count)
		section.Center = Vector3{X: section.Center.X / n, Y: section.Center.Y / n, Z: section.Center.Z / n}
		for _, seat := range out.Seats[section.First : section.First+section.Count] {
			dx, dy, dz := seat.Position.X-section.Center.X, seat.Position.Y-section.Center.Y, seat.Position.Z-section.Center.Z
			section.Radius = det.Max(section.Radius, det.Sqrt(crowdMul(dx, dx)+crowdMul(dy, dy)+crowdMul(dz, dz)))
		}
	}
	return out, nil
}

// crowdMul rounds a product before it feeds a sum, so no compiler fuses the
// two into one multiply-add and every platform (and the browser twin in
// crowd-layout.ts) computes the same seats.
func crowdMul(a, b float64) float64 { return float64(a * b) }

// crowdHash is the lowbias32 integer hash. crowd-layout.ts and the crowd
// shaders use the same function.
func crowdHash(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

// crowdRoll returns the fan's 24-bit roll for one purpose.
func crowdRoll(seed, purpose uint32) uint32 {
	return crowdHash(seed^crowdHash(purpose)) >> 8
}

const (
	crowdRollFill uint32 = iota + 1
	crowdRollColor
	crowdRollAccent
	crowdRollProp
	crowdRollVariant
	crowdRollPhase
	crowdRollEager
	crowdRollYaw
	crowdRollJitterX
	crowdRollJitterZ
)

// crowdPiece is a straight run or a circular turn of a stand's front edge.
// Base positions are front-row arc length from the start of the path.
type crowdPiece struct {
	corner bool
	base   float64
	length float64
	// straight: start, unit direction, outward normal
	px, pz, tx, tz, nx, nz float64
	// corner: centre, front-row radius, angle at the piece start, +1 or -1
	cx, cz, radius, angle, sign float64
}

func (p crowdPiece) at(s, d float64) (x, z, nx, nz float64) {
	if !p.corner {
		return p.px + crowdMul(p.tx, s) + crowdMul(p.nx, d), p.pz + crowdMul(p.tz, s) + crowdMul(p.nz, d), p.nx, p.nz
	}
	theta := p.angle + crowdMul(p.sign, s/p.radius)
	c, sn := det.Cos(theta), det.Sin(theta)
	r := p.radius + d
	return p.cx + crowdMul(r, c), p.cz + crowdMul(r, sn), c, sn
}

func (p crowdPiece) factor(d float64) float64 {
	if !p.corner {
		return 1
	}
	return (p.radius + d) / p.radius
}

// split returns the part of p from base offset a to b (offsets within p).
func (p crowdPiece) split(a, b float64) crowdPiece {
	q := p
	q.length = b - a
	if p.corner {
		q.angle = p.angle + crowdMul(p.sign, a/p.radius)
	} else {
		q.px = p.px + crowdMul(p.tx, a)
		q.pz = p.pz + crowdMul(p.tz, a)
	}
	return q
}

// crowdPath returns the stand's front edge as pieces with ascending base
// positions, its length, and whether it closes on itself.
func crowdPath(s CrowdStandIR) ([]crowdPiece, float64, bool) {
	switch s.Shape {
	case string(CrowdShapeLine):
		dx, dz := s.X1-s.X0, s.Z1-s.Z0
		length := det.Sqrt(crowdMul(dx, dx) + crowdMul(dz, dz))
		tx, tz := dx/length, dz/length
		nx, nz := -tz, tx
		mx, mz := s.X0+dx/2-s.FacingX, s.Z0+dz/2-s.FacingZ
		if crowdMul(nx, mx)+crowdMul(nz, mz) < 0 {
			nx, nz = -nx, -nz
		}
		return []crowdPiece{{px: s.X0, pz: s.Z0, tx: tx, tz: tz, nx: nx, nz: nz, length: length}}, length, false
	case string(CrowdShapeArc):
		sweep := s.EndAngle - s.StartAngle
		sign := 1.0
		if sweep < 0 {
			sign = -1
		}
		length := crowdMul(det.Abs(sweep), s.Radius)
		return []crowdPiece{{corner: true, cx: s.CenterX, cz: s.CenterZ, radius: s.Radius, angle: s.StartAngle, sign: sign, length: length}}, length, false
	}
	loop, total := crowdBowlLoop(s)
	if s.LoopStart == s.LoopEnd {
		return loop, total, true
	}
	from := crowdMul(s.LoopStart, total)
	to := crowdMul(s.LoopEnd, total)
	if to <= from {
		to += total
	}
	var out []crowdPiece
	base := 0.0
	for lap := 0; lap < 2; lap++ {
		offset := crowdMul(float64(lap), total)
		for _, piece := range loop {
			start := piece.base + offset
			end := start + piece.length
			a, b := det.Max(start, from), det.Min(end, to)
			if b-a <= 1e-9 {
				continue
			}
			cut := piece.split(a-start, b-start)
			cut.base = base
			base += cut.length
			out = append(out, cut)
		}
	}
	return out, base, false
}

// crowdBowlLoop builds the rounded rectangle, starting at the middle of the
// +X side and running toward +Z (counter-clockwise seen from above with X
// right and Z down the page).
func crowdBowlLoop(s CrowdStandIR) ([]crowdPiece, float64) {
	a, b, c := s.HalfWidth, s.HalfDepth, s.Corner
	ax, bz := a-c, b-c
	half := det.Pi / 2
	raw := []crowdPiece{
		{px: s.CenterX + a, pz: s.CenterZ, tx: 0, tz: 1, nx: 1, nz: 0, length: bz},
		{corner: true, cx: s.CenterX + ax, cz: s.CenterZ + bz, radius: c, angle: 0, sign: 1, length: crowdMul(half, c)},
		{px: s.CenterX + ax, pz: s.CenterZ + b, tx: -1, tz: 0, nx: 0, nz: 1, length: 2 * ax},
		{corner: true, cx: s.CenterX - ax, cz: s.CenterZ + bz, radius: c, angle: half, sign: 1, length: crowdMul(half, c)},
		{px: s.CenterX - a, pz: s.CenterZ + bz, tx: 0, tz: -1, nx: -1, nz: 0, length: 2 * bz},
		{corner: true, cx: s.CenterX - ax, cz: s.CenterZ - bz, radius: c, angle: det.Pi, sign: 1, length: crowdMul(half, c)},
		{px: s.CenterX - ax, pz: s.CenterZ - b, tx: 1, tz: 0, nx: 0, nz: -1, length: 2 * ax},
		{corner: true, cx: s.CenterX + ax, cz: s.CenterZ - bz, radius: c, angle: crowdMul(3, half), sign: 1, length: crowdMul(half, c)},
		{px: s.CenterX + a, pz: s.CenterZ - bz, tx: 0, tz: 1, nx: 1, nz: 0, length: bz},
	}
	out := make([]crowdPiece, 0, len(raw))
	base := 0.0
	for _, piece := range raw {
		if piece.length <= 1e-9 {
			continue
		}
		piece.base = base
		base += piece.length
		out = append(out, piece)
	}
	return out, base
}

// layoutCrowdIR expands every occupied seat in instance order: stand by
// stand, section by section, row by row along the front edge. emit returns
// false to stop with ErrCrowdTooLarge.
func layoutCrowdIR(ir CrowdIR, emit func(CrowdSeat) bool) error {
	l := crowdLayouter{ir: ir, emit: emit}
	for si, stand := range ir.Stands {
		if stand.FirstSection < 0 || stand.FirstSection+stand.SectionCount > len(ir.Sections) {
			return fmt.Errorf("scene: crowd stand %q has an invalid section range", stand.ID)
		}
		var ok bool
		if stand.Shape == string(CrowdShapeFloor) {
			ok = l.floor(si, stand)
		} else {
			ok = l.stand(si, stand)
		}
		if !ok {
			return ErrCrowdTooLarge
		}
	}
	return nil
}

type crowdLayouter struct {
	ir    CrowdIR
	emit  func(CrowdSeat) bool
	seat  int
	index int
}

// spans returns the cumulative section boundaries of a stand as fractions.
func (l *crowdLayouter) spans(stand CrowdStandIR) []float64 {
	total := 0.0
	for k := 0; k < stand.SectionCount; k++ {
		total += l.ir.Sections[stand.FirstSection+k].Span
	}
	out := make([]float64, stand.SectionCount+1)
	running := 0.0
	for k := 0; k < stand.SectionCount; k++ {
		running += l.ir.Sections[stand.FirstSection+k].Span
		out[k+1] = running / total
	}
	out[stand.SectionCount] = 1
	return out
}

func (l *crowdLayouter) stand(si int, stand CrowdStandIR) bool {
	pieces, length, closed := crowdPath(stand)
	bounds := l.spans(stand)
	half := stand.AisleWidth / 2
	width := stand.SeatWidth
	for k := 0; k < stand.SectionCount; k++ {
		section := stand.FirstSection + k
		u0, u1 := crowdMul(bounds[k], length), crowdMul(bounds[k+1], length)
		left, right := half, half
		if !closed && k == 0 {
			left = 0
		}
		if !closed && k == stand.SectionCount-1 {
			right = 0
		}
		for row := 0; row < stand.Rows; row++ {
			d := crowdMul(float64(row)+0.5, stand.RowDepth)
			y := stand.FrontHeight + crowdMul(float64(row), stand.RowRise)
			rowLength := 0.0
			for _, piece := range pieces {
				a, b := det.Max(piece.base, u0), det.Min(piece.base+piece.length, u1)
				if b > a {
					rowLength += crowdMul(b-a, piece.factor(d))
				}
			}
			usable := rowLength - left - right
			n := int(det.Floor(usable/width + 1e-9))
			if n < 1 {
				continue
			}
			margin := left + (usable-crowdMul(float64(n), width))/2
			for j := 0; j < n; j++ {
				q := margin + crowdMul(float64(j)+0.5, width)
				x, z, nx, nz := crowdPathPoint(pieces, u0, u1, q, d)
				yaw := det.Atan2(-nx, -nz)
				if !l.place(si, section, row, x, y, z, yaw) {
					return false
				}
			}
		}
	}
	return true
}

// crowdPathPoint walks the pieces of the section [u0, u1] to the point q
// metres along the row at offset d.
func crowdPathPoint(pieces []crowdPiece, u0, u1, q, d float64) (x, z, nx, nz float64) {
	var last crowdPiece
	lastOffset := 0.0
	for _, piece := range pieces {
		a, b := det.Max(piece.base, u0), det.Min(piece.base+piece.length, u1)
		if b <= a {
			continue
		}
		f := piece.factor(d)
		run := crowdMul(b-a, f)
		last, lastOffset = piece, b-piece.base
		if q <= run {
			return piece.at(a-piece.base+q/f, d)
		}
		q -= run
	}
	return last.at(lastOffset, d)
}

func (l *crowdLayouter) floor(si int, stand CrowdStandIR) bool {
	g := 1 / det.Sqrt(stand.Density)
	nx := int(det.Floor(2*stand.HalfWidth/g + 1e-9))
	nz := int(det.Floor(2*stand.HalfDepth/g + 1e-9))
	if nx < 1 || nz < 1 {
		return true
	}
	x0 := stand.CenterX - stand.HalfWidth + (2*stand.HalfWidth-crowdMul(float64(nx), g))/2
	z0 := stand.CenterZ - stand.HalfDepth + (2*stand.HalfDepth-crowdMul(float64(nz), g))/2
	bounds := l.spans(stand)
	for k := 0; k < stand.SectionCount; k++ {
		section := stand.FirstSection + k
		c0 := int(det.Floor(crowdMul(bounds[k], float64(nx)) + 0.5))
		c1 := int(det.Floor(crowdMul(bounds[k+1], float64(nx)) + 0.5))
		for row := 0; row < nz; row++ {
			for col := c0; col < c1; col++ {
				seed := l.nextSeed()
				jx := crowdMul(float64(crowdRoll(seed, crowdRollJitterX))/crowdThresholdOne-0.5, 0.6)
				jz := crowdMul(float64(crowdRoll(seed, crowdRollJitterZ))/crowdThresholdOne-0.5, 0.6)
				x := x0 + crowdMul(float64(col)+0.5+jx, g)
				z := z0 + crowdMul(float64(row)+0.5+jz, g)
				yaw := det.Atan2(stand.FacingX-x, stand.FacingZ-z)
				if !l.placeSeeded(seed, si, section, row, x, stand.FrontHeight, z, yaw) {
					return false
				}
			}
		}
	}
	return true
}

func (l *crowdLayouter) nextSeed() uint32 {
	seed := crowdHash(l.ir.Seed + uint32(l.seat+1)*0x9e3779b9)
	l.seat++
	return seed
}

func (l *crowdLayouter) place(si, section, row int, x, y, z, yaw float64) bool {
	return l.placeSeeded(l.nextSeed(), si, section, row, x, y, z, yaw)
}

func (l *crowdLayouter) placeSeeded(seed uint32, si, section, row int, x, y, z, yaw float64) bool {
	sec := l.ir.Sections[section]
	if crowdRoll(seed, crowdRollFill) >= sec.Fill {
		return true
	}
	yaw += crowdMul(float64(crowdRoll(seed, crowdRollYaw))/crowdThresholdOne-0.5, 0.14)
	wave := det.Atan2(z, x) / (2 * det.Pi)
	if wave < 0 {
		wave += 1
	}
	if wave >= 1 {
		wave = 0
	}
	seat := CrowdSeat{
		Index:     l.index,
		Seat:      l.seat - 1,
		Section:   section,
		Stand:     si,
		Row:       row,
		Position:  Vector3{X: x, Y: y, Z: z},
		Yaw:       yaw,
		Wave:      wave,
		Seed:      seed,
		Color:     crowdSlot(crowdRoll(seed, crowdRollColor), sec.ColorThresholds),
		Accent:    crowdSlot(crowdRoll(seed, crowdRollAccent), sec.AccentThresholds),
		Prop:      crowdPropFor(crowdRoll(seed, crowdRollProp), sec.PropThresholds),
		Variant:   uint8(crowdRoll(seed, crowdRollVariant) >> 16),
		Phase:     uint16(crowdRoll(seed, crowdRollPhase) >> 8),
		Eagerness: uint8(crowdRoll(seed, crowdRollEager) >> 16),
	}
	l.index++
	return l.emit(seat)
}

func crowdSlot(roll uint32, thresholds []uint32) int {
	for i, limit := range thresholds {
		if roll < limit {
			return i
		}
	}
	return 0
}

func crowdPropFor(roll uint32, thresholds [4]uint32) CrowdProp {
	for i, limit := range thresholds {
		if roll < limit {
			return CrowdProp(i + 1)
		}
	}
	return CrowdPropNone
}
