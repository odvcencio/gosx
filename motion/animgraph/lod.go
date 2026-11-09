package animgraph

import (
	"math"
	"sort"
)

// Animation level of detail. A character that covers fewer screen pixels
// updates its pose less often, skips small joints (fingers, eyes, jaw, then
// toes and twist bones), drops IK and upper layers. Every reduction fades:
// IK and layer weights fade here, joint detail fades in the renderer, and a
// level changes only after the character's size passes the threshold by a
// hysteresis margin, so levels do not flicker. Skipped frames keep the last
// pose; the renderer still re-skins a skipped character when it moves.

// LODLevel is one level of detail.
type LODLevel struct {
	// MinPixels is the projected character height, in pixels, at or above
	// which this level applies. Levels go from most to least detailed.
	MinPixels float64
	// UpdateHz is how often the pose updates; 0 means every frame.
	UpdateHz float64
	// Detail selects which joints animate: 0 all; 1 drops fingers, eyes and
	// jaw; 2 also drops toes and bones the humanoid map does not name
	// (twist, accessory and end bones).
	Detail uint8
	// IK keeps IK chains and look-at enabled.
	IK bool
	// Layers keeps the first Layers layers; higher layers fade out. 0 keeps
	// all.
	Layers int
}

// DefaultLOD returns levels tuned for 20 to 30 humanoids on screen: full
// detail above 220 pixels, 30 Hz without fingers above 90, 20 Hz base layer
// only above 35, and 10 Hz below.
func DefaultLOD() []LODLevel {
	return []LODLevel{
		{MinPixels: 220, IK: true},
		{MinPixels: 90, UpdateHz: 30, Detail: 1, IK: true},
		{MinPixels: 35, UpdateHz: 20, Detail: 2, Layers: 1},
		{MinPixels: 0, UpdateHz: 10, Detail: 2, Layers: 1},
	}
}

// LODCamera is what the scheduler needs to project a character's height.
type LODCamera struct {
	Position [3]float64
	// FOVY is the vertical field of view in radians.
	FOVY float64
	// ViewportHeight is the canvas height in device pixels.
	ViewportHeight float64
}

// LODTarget is one character for LODScheduler.Update.
type LODTarget struct {
	Inst     *Instance
	Position [3]float64
	// Height is the character's height in world units (about 1.8 for an
	// adult human).
	Height float64
}

// LODScheduler assigns levels and decides which characters update each
// frame. It is not safe for concurrent use.
type LODScheduler struct {
	Levels []LODLevel
	// FullBudget caps how many characters run level 0 at once; the smallest
	// on screen drop to level 1. 0 means no cap.
	FullBudget int
	// Hysteresis is the fraction of a threshold a character must pass
	// before it changes level. Zero means 0.15.
	Hysteresis float64
	// FadeSeconds is the length of IK, layer and joint-detail fades. Zero
	// means 0.25.
	FadeSeconds float64

	due   []*Instance
	order []int
	seen  uint64
}

type lodState struct {
	level      int
	set        bool
	detail     uint8
	ik         float64
	ikTarget   float64
	layers     int
	layerScale []float64
	period     float64
	accum      float64
	pixels     float64
	fade       float64
}

func newLODState(layers int) lodState {
	s := lodState{ik: 1, ikTarget: 1, layerScale: make([]float64, layers), fade: 0.25}
	for i := range s.layerScale {
		s.layerScale[i] = 1
	}
	return s
}

// LOD reports an instance's current level index and its projected height in
// pixels from the last scheduler update (level 0 and 0 when unscheduled).
func (in *Instance) LOD() (level int, pixels float64) { return in.lod.level, in.lod.pixels }

// advanceLOD moves the IK and layer fades toward the level's targets.
func (in *Instance) advanceLOD(dt float64) {
	s := &in.lod
	if !s.set || dt <= 0 {
		return
	}
	step := dt / s.fade
	s.ik = approach(s.ik, s.ikTarget, step)
	for li := range s.layerScale {
		target := 1.0
		if s.layers > 0 && li >= s.layers {
			target = 0
		}
		s.layerScale[li] = approach(s.layerScale[li], target, step)
	}
}

func approach(v, target, step float64) float64 {
	if v < target {
		return math.Min(target, v+step)
	}
	return math.Max(target, v-step)
}

// projectedPixels is the on-screen height of an upright character.
func projectedPixels(cam LODCamera, pos [3]float64, height float64) float64 {
	dx, dy, dz := pos[0]-cam.Position[0], pos[1]+height/2-cam.Position[1], pos[2]-cam.Position[2]
	dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
	fov := cam.FOVY
	if !(fov > 0) {
		fov = math.Pi / 3
	}
	if dist < 1e-3 {
		return math.Inf(1)
	}
	return height / dist * cam.ViewportHeight / (2 * math.Tan(fov/2))
}

// Update assigns each character a level and returns the instances whose pose
// updates this frame, in input order. The slice is reused by the next call.
// Call it once per rendered frame after stepping the instances, then pass
// the result to Dispatch. Instances not returned keep their last pose.
func (s *LODScheduler) Update(dt float64, cam LODCamera, chars []LODTarget) []*Instance {
	levels := s.Levels
	if len(levels) == 0 {
		levels = DefaultLOD()
		s.Levels = levels
	}
	hysteresis := s.Hysteresis
	if hysteresis <= 0 {
		hysteresis = 0.15
	}
	fade := s.FadeSeconds
	if fade <= 0 {
		fade = 0.25
	}
	s.order = s.order[:0]
	for i, c := range chars {
		if c.Inst == nil {
			continue
		}
		c.Inst.lod.pixels = projectedPixels(cam, c.Position, c.Height)
		s.order = append(s.order, i)
	}
	// Largest first, so the full-detail budget goes to the biggest
	// characters on screen.
	sort.SliceStable(s.order, func(a, b int) bool {
		return chars[s.order[a]].Inst.lod.pixels > chars[s.order[b]].Inst.lod.pixels
	})
	full := 0
	for _, i := range s.order {
		in := chars[i].Inst
		st := &in.lod
		st.fade = fade
		want := levelFor(levels, st.pixels)
		if st.set && want != st.level {
			// Hysteresis: move up only once well above the better level's
			// threshold, down only once well below the current one.
			if want < st.level && st.pixels < levels[want].MinPixels*(1+hysteresis) {
				want = st.level
			} else if want > st.level && st.pixels > levels[st.level].MinPixels*(1-hysteresis) {
				want = st.level
			}
		}
		if want == 0 && s.FullBudget > 0 {
			if full >= s.FullBudget && len(levels) > 1 {
				want = 1
			} else {
				full++
			}
		}
		if !st.set {
			// Stagger first updates so characters at one rate spread over
			// frames instead of all updating together.
			s.seen++
			st.accum = math.Mod(float64(s.seen)*0.6180339887, 1)
			st.ik = boolWeight(levels[want].IK)
			for li := range st.layerScale {
				st.layerScale[li] = 1
				if levels[want].Layers > 0 && li >= levels[want].Layers {
					st.layerScale[li] = 0
				}
			}
		}
		st.set = true
		st.level = want
		lv := levels[want]
		st.detail = lv.Detail
		st.ikTarget = boolWeight(lv.IK)
		st.layers = lv.Layers
		if lv.UpdateHz > 0 {
			st.period = 1 / lv.UpdateHz
		} else {
			st.period = 0
		}
	}
	s.due = s.due[:0]
	for _, c := range chars {
		in := c.Inst
		if in == nil {
			continue
		}
		st := &in.lod
		if st.period <= 0 {
			s.due = append(s.due, in)
			continue
		}
		// accum counts periods since the last update, seeded with a
		// fraction for the stagger.
		st.accum += dt / st.period
		if st.accum >= 1 {
			st.accum -= math.Floor(st.accum)
			s.due = append(s.due, in)
		}
	}
	return s.due
}

func levelFor(levels []LODLevel, pixels float64) int {
	for i, lv := range levels {
		if pixels >= lv.MinPixels {
			return i
		}
	}
	return len(levels) - 1
}

func boolWeight(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
