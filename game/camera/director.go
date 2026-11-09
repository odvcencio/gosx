package camera

import "math"

// Blend describes how a handoff moves from the old pose to the new source.
// A zero Duration is a cut.
type Blend struct {
	Duration float64 // seconds
	Ease     Ease    // default EaseSmooth
}

// EventKind names a Director event.
type EventKind uint8

const (
	// EventHandoffDone fires when a blend finishes.
	EventHandoffDone EventKind = iota + 1
	// EventSequenceStart fires when Play begins a sequence.
	EventSequenceStart
	// EventSequenceStep fires as each step begins; Index is the step.
	EventSequenceStep
	// EventSequenceEnd fires when a sequence finishes by itself.
	EventSequenceEnd
	// EventSequenceInterrupted fires when Interrupt or Stop ends a sequence.
	EventSequenceInterrupted
)

// Event reports Director progress.
type Event struct {
	Kind     EventKind
	Sequence string
	Index    int
}

// ReducedPolicy says what a Sequence does when motion is reduced.
type ReducedPolicy uint8

const (
	// ReducedSkip skips the sequence and returns to the base source with a cut.
	ReducedSkip ReducedPolicy = iota
	// ReducedCuts plays the steps as cuts, keeping their Hold times. Use it for
	// a shot that conveys something the player needs to see.
	ReducedCuts
)

// Step is one shot of a Sequence.
type Step struct {
	// Shot names a bookmark. It is used when Source is nil.
	Shot string
	// Source drives the camera during the step. It overrides Shot.
	Source Source
	// Blend moves into the step.
	Blend Blend
	// Hold is how long the step stays after the blend ends, in seconds.
	Hold float64
	// Until, if set, keeps the step running until it returns true, and then
	// Hold also has to have elapsed. A Path step uses it with Path.Done.
	Until func() bool
}

// Sequence is an ordered list of steps: an intro, a focus on an objective, a tour.
type Sequence struct {
	Name  string
	Steps []Step
	// Loop repeats the steps until Stop or Interrupt.
	Loop bool
	// Locked stops Interrupt from ending the sequence. Stop still does.
	Locked bool
	// Return moves back to the base source when the sequence ends or is interrupted.
	Return Blend
	// Reduced picks the behaviour when motion is reduced.
	Reduced ReducedPolicy
}

// DirectorConfig wires a Director to the settings.
type DirectorConfig struct {
	// ReducedMotion reports the player's preference. When true, blends become
	// cuts and sequences follow their Reduced policy.
	ReducedMotion func() bool
	// ShakeScale gates the shake (see settings.Manager.ShakeScale).
	ShakeScale func() float64
	Shake      ShakeConfig
	// OnEvent receives progress events.
	OnEvent func(Event)
}

// Director is the one owner of the game camera. It always runs the base
// source (gameplay follow or framing) and decides which source is visible:
// the base, a bookmark, or a sequence. Every change of source blends from the
// pose on screen, so control handoffs never pop. Add shake with AddTrauma.
// Not safe for concurrent use.
type Director struct {
	cfg       DirectorConfig
	base      Source
	cur       Source
	shake     *Shake
	bookmarks map[string]Pose

	last     Pose // pose on screen before shake
	from     Pose
	blending bool
	blendT   float64
	blend    Blend

	seq      *Sequence
	step     int
	stepHold float64
	stepLive bool // blend finished, hold running
	out      Pose
	started  bool
}

// NewDirector creates a Director with base as the gameplay source.
func NewDirector(cfg DirectorConfig, base Source) *Director {
	return &Director{
		cfg: cfg, base: base, cur: base,
		shake:     NewShake(cfg.Shake, cfg.ShakeScale),
		bookmarks: map[string]Pose{},
	}
}

func (d *Director) reduced() bool { return d.cfg.ReducedMotion != nil && d.cfg.ReducedMotion() }

func (d *Director) emit(e Event) {
	if d.cfg.OnEvent != nil {
		d.cfg.OnEvent(e)
	}
}

// SetBase replaces the gameplay source and cuts to it.
func (d *Director) SetBase(src Source) {
	d.base, d.cur, d.seq, d.blending = src, src, nil, false
}

// Bookmark stores a named pose.
func (d *Director) Bookmark(name string, p Pose) { d.bookmarks[name] = p }

// Bookmarked returns the stored pose for name.
func (d *Director) Bookmarked(name string) (Pose, bool) {
	p, ok := d.bookmarks[name]
	return p, ok
}

// AddTrauma adds shake (0..1).
func (d *Director) AddTrauma(amount float64) { d.shake.AddTrauma(amount) }

// Shake returns the shake generator, for tuning or reading Trauma.
func (d *Director) Shake() *Shake { return d.shake }

func (d *Director) startBlend(b Blend) {
	if d.reduced() || b.Duration <= 0 || !d.started {
		d.blending = false
		return
	}
	d.from, d.blend, d.blendT, d.blending = d.last, b, 0, true
}

// Take hands the camera to src, blending from the pose on screen. The base
// source keeps running underneath.
func (d *Director) Take(src Source, b Blend) {
	d.endSequence(true)
	d.cur = src
	d.startBlend(b)
}

// Release hands the camera back to the base source.
func (d *Director) Release(b Blend) {
	d.endSequence(true)
	d.cur = d.base
	d.startBlend(b)
}

// GoTo moves to a bookmark and holds it until Release.
func (d *Director) GoTo(name string, b Blend) bool {
	p, ok := d.bookmarks[name]
	if !ok {
		return false
	}
	d.Take(Static(p), b)
	return true
}

// Busy reports whether the camera is not simply showing the base source: a
// blend, bookmark or sequence is active.
func (d *Director) Busy() bool { return d.blending || d.cur != d.base || d.seq != nil }

// Playing returns the name of the running sequence, or "".
func (d *Director) Playing() string {
	if d.seq == nil {
		return ""
	}
	return d.seq.Name
}

// Play starts a sequence. A running sequence is replaced. With reduced
// motion and ReducedSkip the sequence is skipped and Play returns false.
func (d *Director) Play(seq Sequence) bool {
	if len(seq.Steps) == 0 {
		return false
	}
	if d.reduced() && seq.Reduced == ReducedSkip {
		d.endSequence(false)
		d.cur = d.base
		d.blending = false
		return false
	}
	d.seq = &seq
	d.emit(Event{Kind: EventSequenceStart, Sequence: seq.Name})
	d.beginStep(0)
	return true
}

func (d *Director) beginStep(i int) {
	st := d.seq.Steps[i]
	d.step, d.stepHold, d.stepLive = i, 0, false
	switch {
	case st.Source != nil:
		d.cur = st.Source
	default:
		p, ok := d.bookmarks[st.Shot]
		if !ok {
			p = d.last // unknown shot: hold where we are rather than jump
		}
		d.cur = Static(p)
	}
	d.startBlend(st.Blend)
	if !d.blending {
		d.stepLive = true
	}
	d.emit(Event{Kind: EventSequenceStep, Sequence: d.seq.Name, Index: i})
}

// Interrupt ends the running sequence unless it is Locked, and blends back to
// the base source. Call it when the player takes control. It reports whether
// anything was interrupted.
func (d *Director) Interrupt() bool {
	if d.seq == nil || d.seq.Locked {
		return false
	}
	d.Stop()
	return true
}

// Stop ends the running sequence even if Locked and blends back to the base.
func (d *Director) Stop() {
	if d.seq == nil {
		return
	}
	ret := d.seq.Return
	d.endSequence(true)
	d.cur = d.base
	d.startBlend(ret)
}

func (d *Director) endSequence(interrupted bool) {
	if d.seq == nil {
		return
	}
	kind := EventSequenceEnd
	if interrupted {
		kind = EventSequenceInterrupted
	}
	name := d.seq.Name
	d.seq = nil
	d.emit(Event{Kind: kind, Sequence: name})
}

func (d *Director) advanceSequence(dt float64) {
	if d.seq == nil || !d.stepLive {
		return
	}
	st := d.seq.Steps[d.step]
	d.stepHold += dt
	if d.stepHold < st.Hold || (st.Until != nil && !st.Until()) {
		return
	}
	if next := d.step + 1; next < len(d.seq.Steps) {
		d.beginStep(next)
		return
	}
	if d.seq.Loop {
		d.beginStep(0)
		return
	}
	ret := d.seq.Return
	d.endSequence(false)
	d.cur = d.base
	d.startBlend(ret)
}

// Update advances the camera by dt seconds and returns the pose to render,
// including shake.
func (d *Director) Update(dt float64) Pose {
	if !finite(dt) || dt < 0 {
		dt = 0
	}
	if d.reduced() && d.seq != nil && d.seq.Reduced == ReducedSkip {
		d.endSequence(true)
		d.cur = d.base
		d.blending = false
	}
	d.advanceSequence(dt)
	basePose := d.base.Update(dt)
	pose := basePose
	if d.cur != d.base {
		pose = d.cur.Update(dt)
	}
	if d.blending {
		d.blendT += dt
		k := 1.0
		if d.blend.Duration > 0 {
			k = math.Min(1, d.blendT/d.blend.Duration)
		}
		pose = LerpPose(d.from, pose, d.blend.Ease.at(k))
		if k >= 1 {
			d.blending = false
			d.emit(Event{Kind: EventHandoffDone})
			if d.seq != nil {
				d.stepLive = true
			}
		}
	}
	if d.seq != nil && !d.blending {
		d.stepLive = true
	}
	d.last, d.started = pose, true
	d.out = d.applyShake(pose, dt)
	return d.out
}

// Pose returns the pose from the last Update.
func (d *Director) Pose() Pose { return d.out }

func (d *Director) applyShake(p Pose, dt float64) Pose {
	off, roll := d.shake.Update(dt)
	if off == (Vec3{}) && roll == 0 {
		return p
	}
	back := p.Position.Sub(p.Target).Norm()
	right := Vec3{0, 1, 0}.Cross(back).Norm()
	if right == (Vec3{}) {
		right = Vec3{1, 0, 0}
	}
	up := back.Cross(right)
	delta := right.Scale(off.X).Add(up.Scale(off.Y)).Add(back.Scale(off.Z))
	p.Position = p.Position.Add(delta)
	p.Target = p.Target.Add(delta)
	p.Roll += roll
	return p
}
