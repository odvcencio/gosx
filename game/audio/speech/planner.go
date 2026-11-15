package speech

import "math"

// FragRef is one fragment in an utterance. GapMs is the pause to leave after
// it (on top of the sequencer's default gap), or zero.
type FragRef struct {
	ID    FragID
	GapMs int16
}

// Utterance is a planned line: fragments in order plus how it ranks.
type Utterance struct {
	// Phrase and Variant index the grammar, for tests and logging.
	Phrase, Variant int
	Category        int
	Priority        int
	Interruptible   bool
	N               int
	Frags           [MaxFragments]FragRef
}

// Playing describes what the voice is saying now. The caller fills it from
// Sequencer.Playing.
type Playing struct {
	Active        bool
	Priority      int
	Interruptible bool
}

// Action is what the planner decided to do with an event.
type Action uint8

const (
	// Drop means nothing is said; Reason says why.
	Drop Action = iota
	// Play means the voice is idle: start the utterance.
	Play
	// Interrupt means cut the current line and start the utterance.
	Interrupt
	// Queued means the utterance waits; Poll returns it when the voice frees up.
	Queued
)

// Reason explains a Drop.
type Reason uint8

const (
	ReasonNone Reason = iota
	// ReasonNoMatch: no phrase answers the event kind, or none passes its conditions.
	ReasonNoMatch
	// ReasonCooldown: every candidate is on a phrase or category cooldown.
	ReasonCooldown
	// ReasonUnbound: every candidate has a slot the event cannot fill.
	ReasonUnbound
	// ReasonBusy: the voice is busy with an equal or higher priority line.
	ReasonBusy
	// ReasonQueueFull: the line should have waited but the queue was full.
	ReasonQueueFull
)

// Decision is the result of Submit or Poll.
type Decision struct {
	Action Action
	Reason Reason
	Utt    Utterance
}

// PlannerConfig tunes a Planner.
type PlannerConfig struct {
	// Seed makes selection deterministic. The same seed, grammar and event
	// stream give the same speech.
	Seed uint64
	// History is how many recent phrase variants are kept out of the draw.
	// Default 6. When every candidate is in the window, the window is ignored.
	History int
	// QueueSize bounds the waiting lines. Default 4.
	QueueSize int
	// Context is read by conditions after the event's own fields. Optional;
	// the Planner makes its own when nil.
	Context *Context
}

type cand struct {
	phrase, variant int32
	weight          float64
}

type queued struct {
	utt     Utterance
	expires float64
	order   uint64
	used    bool
}

// Planner turns events into utterances. It is not safe for concurrent use;
// drive it from the game loop. All scratch space is sized in NewPlanner.
type Planner struct {
	c        *Compiled
	rng      rng
	ctx      *Context
	lastAt   []float64 // per phrase
	lastCat  []float64 // per category
	everAt   []bool
	everCat  []bool
	history  []int32 // ring of phrase<<12|variant keys; -1 empty
	histPos  int
	cands    []cand
	tmp      Utterance
	queue    []queued
	queueSeq uint64
}

// NewPlanner builds a planner over c.
func NewPlanner(c *Compiled, cfg PlannerConfig) *Planner {
	if cfg.History == 0 {
		cfg.History = 6
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 4
	}
	p := &Planner{
		c:       c,
		rng:     rng{s: cfg.Seed},
		ctx:     cfg.Context,
		lastAt:  make([]float64, len(c.phrases)),
		everAt:  make([]bool, len(c.phrases)),
		lastCat: make([]float64, len(c.catNames)),
		everCat: make([]bool, len(c.catNames)),
		history: make([]int32, cfg.History),
		cands:   make([]cand, 0, c.maxCand),
		queue:   make([]queued, cfg.QueueSize),
	}
	if p.ctx == nil {
		p.ctx = &Context{}
	}
	for i := range p.history {
		p.history[i] = -1
	}
	return p
}

// Context returns the planner's context for the game to update.
func (p *Planner) Context() *Context { return p.ctx }

func histKey(phrase, variant int32) int32 { return phrase<<12 | variant&0xfff }

func (p *Planner) recent(key int32) bool {
	for _, h := range p.history {
		if h == key {
			return true
		}
	}
	return false
}

// Submit plans a line for ev at time now (seconds, on any monotonic clock the
// caller keeps consistent). playing tells the planner what the voice is doing.
func (p *Planner) Submit(ev *Event, now float64, playing Playing) Decision {
	list := p.c.byKind[ev.Kind]
	if len(list) == 0 {
		return Decision{Action: Drop, Reason: ReasonNoMatch}
	}
	p.cands = p.cands[:0]
	var sawMatch, sawCool, sawBind bool
	var total, totalFresh float64
	for _, pi := range list {
		ph := &p.c.phrases[pi]
		if !condsHold(ph.conds, ev, p.ctx) {
			continue
		}
		sawMatch = true
		if ph.cooldown > 0 && p.everAt[pi] && now-p.lastAt[pi] < ph.cooldown {
			sawCool = true
			continue
		}
		if ph.cat >= 0 && p.c.catCool[ph.cat] > 0 && p.everCat[ph.cat] && now-p.lastCat[ph.cat] < p.c.catCool[ph.cat] {
			sawCool = true
			continue
		}
		for vi := range ph.variants {
			v := &ph.variants[vi]
			if !condsHold(v.conds, ev, p.ctx) {
				continue
			}
			sawMatch = true
			if !p.c.bind(v, ev, &p.tmp) {
				sawBind = true
				continue
			}
			w := ph.weight * v.weight
			if w <= 0 {
				continue
			}
			p.cands = append(p.cands, cand{phrase: pi, variant: int32(vi), weight: w})
			total += w
			if !p.recent(histKey(pi, int32(vi))) {
				totalFresh += w
			}
		}
	}
	if len(p.cands) == 0 {
		switch {
		case sawBind:
			return Decision{Action: Drop, Reason: ReasonUnbound}
		case sawCool:
			return Decision{Action: Drop, Reason: ReasonCooldown}
		default:
			_ = sawMatch
			return Decision{Action: Drop, Reason: ReasonNoMatch}
		}
	}
	useFresh := totalFresh > 0
	sum := total
	if useFresh {
		sum = totalFresh
	}
	pick := p.rng.float() * sum
	chosen := -1
	for i, cd := range p.cands {
		if useFresh && p.recent(histKey(cd.phrase, cd.variant)) {
			continue
		}
		chosen = i
		pick -= cd.weight
		if pick < 0 {
			break
		}
	}
	cd := p.cands[chosen]
	ph := &p.c.phrases[cd.phrase]

	var d Decision
	p.c.bind(&ph.variants[cd.variant], ev, &d.Utt)
	d.Utt.Phrase, d.Utt.Variant = int(cd.phrase), int(cd.variant)
	d.Utt.Category = ph.cat
	d.Utt.Priority = ph.priority
	if ev.Priority != 0 {
		d.Utt.Priority = ev.Priority
	}
	d.Utt.Interruptible = ph.interruptible

	switch {
	case !playing.Active:
		d.Action = Play
	case d.Utt.Priority > playing.Priority && playing.Interruptible:
		d.Action = Interrupt
	case ph.maxWait > 0:
		if !p.enqueue(&d.Utt, now+ph.maxWait) {
			return Decision{Action: Drop, Reason: ReasonQueueFull}
		}
		d.Action = Queued
	default:
		return Decision{Action: Drop, Reason: ReasonBusy}
	}
	p.commit(cd.phrase, cd.variant, ph, now)
	return d
}

func (p *Planner) commit(pi, vi int32, ph *cphrase, now float64) {
	p.lastAt[pi], p.everAt[pi] = now, true
	if ph.cat >= 0 {
		p.lastCat[ph.cat], p.everCat[ph.cat] = now, true
	}
	p.history[p.histPos] = histKey(pi, vi)
	p.histPos = (p.histPos + 1) % len(p.history)
}

func (p *Planner) enqueue(u *Utterance, expires float64) bool {
	slot := -1
	for i := range p.queue {
		if !p.queue[i].used {
			slot = i
			break
		}
	}
	if slot < 0 {
		// Replace the lowest-priority waiting line if the new one outranks it.
		low := 0
		for i := range p.queue {
			if p.queue[i].utt.Priority < p.queue[low].utt.Priority {
				low = i
			}
		}
		if p.queue[low].utt.Priority >= u.Priority {
			return false
		}
		slot = low
	}
	p.queueSeq++
	p.queue[slot] = queued{utt: *u, expires: expires, order: p.queueSeq, used: true}
	return true
}

// Poll returns the best waiting line when the voice can take it, dropping
// lines that waited past their limit. Call it once per frame, or whenever the
// voice goes idle.
func (p *Planner) Poll(now float64, playing Playing) Decision {
	best := -1
	for i := range p.queue {
		q := &p.queue[i]
		if !q.used {
			continue
		}
		if now > q.expires {
			q.used = false
			continue
		}
		if best < 0 || q.utt.Priority > p.queue[best].utt.Priority ||
			q.utt.Priority == p.queue[best].utt.Priority && q.order < p.queue[best].order {
			best = i
		}
	}
	if best < 0 {
		return Decision{}
	}
	q := &p.queue[best]
	act := Play
	if playing.Active {
		if q.utt.Priority > playing.Priority && playing.Interruptible {
			act = Interrupt
		} else {
			return Decision{}
		}
	}
	d := Decision{Action: act, Utt: q.utt}
	q.used = false
	return d
}

// Pending is the number of lines waiting in the queue.
func (p *Planner) Pending() int {
	n := 0
	for i := range p.queue {
		if p.queue[i].used {
			n++
		}
	}
	return n
}

// bind fills u from variant v and ev. It reports false when a required slot
// has no fragment (an unknown name, an out-of-range number).
func (c *Compiled) bind(v *cvariant, ev *Event, u *Utterance) bool {
	u.N = 0
	push := func(id FragID, gap int16) bool {
		if u.N >= MaxFragments {
			return false
		}
		u.Frags[u.N] = FragRef{ID: id, GapMs: gap}
		u.N++
		return true
	}
	for i := range v.parts {
		pt := &v.parts[i]
		switch pt.kind {
		case partLit:
			if !push(pt.frag, 0) {
				return false
			}
		case partPause:
			if u.N == 0 {
				continue
			}
			u.Frags[u.N-1].GapMs += pt.pauseMs
		case partPool:
			id, ok := pt.pool.resolve(ev, pt.field)
			if !ok {
				if pt.optional {
					continue
				}
				return false
			}
			if !push(id, 0) {
				return false
			}
		case partNumber:
			f, ok := ev.find(pt.field)
			ok = ok && f.Num == math.Trunc(f.Num)
			if ok {
				ok = c.pushNumber(int(f.Num), push)
			}
			if !ok {
				if pt.optional {
					continue
				}
				return false
			}
		}
	}
	return u.N > 0
}

func (p *cpool) resolve(ev *Event, field string) (FragID, bool) {
	f, ok := ev.find(field)
	if !ok {
		return 0, false
	}
	if f.Str != "" {
		id, ok := p.byStr[f.Str]
		return id, ok
	}
	if f.Num == math.Trunc(f.Num) {
		id, ok := p.byNum[int(f.Num)]
		return id, ok
	}
	return 0, false
}

// pushNumber speaks n as cardinal words: 47 is "forty" "seven", 305 is
// "three" "hundred" "five".
func (c *Compiled) pushNumber(n int, push func(FragID, int16) bool) bool {
	if n < 0 || n > c.numMax {
		return false
	}
	if h := n / 100; h > 0 {
		if !push(c.numUnits[h], 0) || !push(c.numHund, 0) {
			return false
		}
		n %= 100
		if n == 0 {
			return true
		}
	}
	if n < 20 {
		return push(c.numUnits[n], 0)
	}
	if !push(c.numTens[n/10], 0) {
		return false
	}
	if n%10 != 0 {
		return push(c.numUnits[n%10], 0)
	}
	return true
}
