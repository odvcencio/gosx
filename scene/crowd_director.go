package scene

import (
	"fmt"
	"sort"

	"m31labs.dev/gosx/det"
)

// CrowdDirector drives the sections of one Crowd at run time: what they do,
// how excited they are, gestures on a shared clock, travelling waves and
// raised props. It is plain deterministic Go with no browser dependency, so a
// game runs it in its simulation and on a server alike.
//
// Every method takes t, the caller's clock in seconds (a game or simulation
// clock that starts near zero). Frame returns the changes since the previous
// Frame as a CrowdFrame for DispatchCrowdFrame; FullFrame returns every
// section, for a page that mounts late or reconnects. Audio and Beats expose
// the same state to the audio mixer.
//
// A section selector is a section ID, a stand ID (all its sections), a group
// name from Group, or "*" for every section. No selector means every section.
type CrowdDirector struct {
	crowd    string
	ids      map[string]int
	stands   map[string][]int
	groups   map[string][]int
	sections []crowdSectionState
	layout   []CrowdSectionLayout
	dirty    []bool
	waves    []CrowdWaveFrame
	pending  []CrowdWaveFrame
	replace  bool
	// Blend is the seconds an action change takes. Zero means 0.4.
	Blend float64
}

type crowdSectionState struct {
	from, to          int
	blendStart        float64
	blend             float64
	excFrom, excTo    float64
	excStart, excRamp float64
	rate              float64
	synced            bool
	epoch, jitter     float64
	props             bool
	standing, noWave  bool
}

// CrowdWave describes a travelling wave for StartWave.
type CrowdWave struct {
	// Origin is the starting azimuth in turns from +X toward +Z (see
	// CrowdSeat.Wave).
	Origin float64
	// Speed is in turns per second; negative runs the other way. Zero means
	// 0.08, about 12 seconds a lap.
	Speed float64
	// Width is the risen band in turns. Zero means 0.035.
	Width float64
	// Laps is how many times the wave goes round. Zero means 1.
	Laps float64
}

// NewCrowdDirector builds a director for crowd, starting every section in
// its declared state.
func NewCrowdDirector(crowd Crowd) (*CrowdDirector, error) {
	ir, err := crowd.IR()
	if err != nil {
		return nil, err
	}
	layout, err := ir.Layout()
	if err != nil {
		return nil, err
	}
	d := &CrowdDirector{
		crowd:    ir.ID,
		ids:      map[string]int{},
		stands:   map[string][]int{},
		groups:   map[string][]int{},
		sections: make([]crowdSectionState, len(ir.Sections)),
		layout:   layout.Sections,
		dirty:    make([]bool, len(ir.Sections)),
	}
	for i, s := range ir.Sections {
		d.ids[s.ID] = i
		stand := ir.Stands[s.Stand].ID
		d.stands[stand] = append(d.stands[stand], i)
		d.sections[i] = crowdSectionState{
			from: s.Action, to: s.Action, blendStart: -1e6, blend: 0.4,
			excFrom: s.Excitement, excTo: s.Excitement, excStart: -1e6, excRamp: 0.001, rate: 1, jitter: 0.12,
			props: true, standing: s.Standing, noWave: s.NoWave,
		}
	}
	return d, nil
}

// Crowd returns the Crowd.ID the director drives.
func (d *CrowdDirector) Crowd() string { return d.crowd }

// Sections returns the layout summary of every section: ID, index, fan
// count, centre and radius.
func (d *CrowdDirector) Sections() []CrowdSectionLayout {
	return append([]CrowdSectionLayout(nil), d.layout...)
}

// Group names a set of selectors, for example "home" for the home sections.
func (d *CrowdDirector) Group(name string, selectors ...string) error {
	indices, err := d.resolve(selectors)
	if err != nil {
		return err
	}
	d.groups[name] = indices
	return nil
}

func (d *CrowdDirector) resolve(selectors []string) ([]int, error) {
	if len(selectors) == 0 {
		selectors = []string{"*"}
	}
	seen := make([]bool, len(d.sections))
	var out []int
	add := func(i int) {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	for _, sel := range selectors {
		switch {
		case sel == "*":
			for i := range d.sections {
				add(i)
			}
		default:
			if i, ok := d.ids[sel]; ok {
				add(i)
			} else if list, ok := d.stands[sel]; ok {
				for _, i := range list {
					add(i)
				}
			} else if list, ok := d.groups[sel]; ok {
				for _, i := range list {
					add(i)
				}
			} else {
				return nil, fmt.Errorf("scene: crowd %q has no section, stand or group %q", d.crowd, sel)
			}
		}
	}
	sort.Ints(out)
	return out, nil
}

func (d *CrowdDirector) blend() float64 {
	if d.Blend > 0 {
		return d.Blend
	}
	return 0.4
}

// excitementAt is the section's excitement at t, following its ramp.
func (s crowdSectionState) excitementAt(t float64) float64 {
	k := det.Max(0, det.Min(1, (t-s.excStart)/det.Max(s.excRamp, 0.001)))
	return s.excFrom + crowdMul(s.excTo-s.excFrom, k)
}

// SetAction blends the selected sections to action over Director.Blend
// seconds, starting at t. Fans join one by one over the next 0.6 seconds.
func (d *CrowdDirector) SetAction(t float64, action CrowdAction, selectors ...string) error {
	index, ok := crowdActionIndex(action)
	if !ok || index == crowdActions[len(crowdActions)-1].Index {
		return fmt.Errorf("scene: %q is not a section action", action)
	}
	return d.each(selectors, func(s *crowdSectionState) {
		s.from, s.to = s.to, index
		s.blendStart, s.blend = t, d.blend()
	})
}

// SetExcitement ramps the selected sections to level (0 to 1) over ramp
// seconds from t. Excitement decides how many fans join the section's action
// (all of them at 1, a quarter at 0); the rest idle. Each fan joins or leaves
// with a short blend when the ramp passes its eagerness.
func (d *CrowdDirector) SetExcitement(t, level, ramp float64, selectors ...string) error {
	level = clampUnit(level)
	return d.each(selectors, func(s *crowdSectionState) {
		s.excFrom, s.excTo = s.excitementAt(t), level
		s.excStart, s.excRamp = t, det.Max(ramp, 0.001)
	})
}

// Sync puts the selected sections on one shared beat from t: every fan plays
// action with one cycle per beat at bpm beats per minute, starting together,
// so claps and jumps land on the beat. Beats reports the times.
func (d *CrowdDirector) Sync(t float64, action CrowdAction, bpm float64, selectors ...string) error {
	info, ok := CrowdActionByName(action)
	if !ok || info.Index == crowdActions[len(crowdActions)-1].Index {
		return fmt.Errorf("scene: %q is not a section action", action)
	}
	if bpm <= 0 {
		return fmt.Errorf("scene: crowd sync needs a positive tempo, got %v", bpm)
	}
	rate := crowdMul(bpm/60, info.Cycle)
	return d.each(selectors, func(s *crowdSectionState) {
		s.from, s.to = s.to, info.Index
		s.blendStart, s.blend = t, d.blend()
		s.synced, s.epoch, s.rate, s.jitter = true, t, rate, 0.04
	})
}

// Release returns the selected sections to free-running phases at the
// action's natural rate.
func (d *CrowdDirector) Release(t float64, selectors ...string) error {
	return d.each(selectors, func(s *crowdSectionState) {
		s.synced, s.rate, s.jitter = false, 1, 0.12
	})
}

// RaiseProps shows or hides the selected sections' props from the next frame.
func (d *CrowdDirector) RaiseProps(t float64, raised bool, selectors ...string) error {
	return d.each(selectors, func(s *crowdSectionState) { s.props = raised })
}

// StartWave sends a travelling wave round the crowd from t. Sections declared
// NoWave stay seated. A page runs at most four waves at once.
func (d *CrowdDirector) StartWave(t float64, wave CrowdWave) {
	w := CrowdWaveFrame{Start: t, Origin: wave.Origin - det.Floor(wave.Origin), Speed: wave.Speed, Width: wave.Width, Laps: wave.Laps}
	if w.Speed == 0 {
		w.Speed = 0.08
	}
	if w.Width <= 0 {
		w.Width = 0.035
	}
	if w.Laps <= 0 {
		w.Laps = 1
	}
	d.prune(t)
	if len(d.waves) >= crowdMaxWaves {
		d.waves = d.waves[1:]
	}
	d.waves = append(d.waves, w)
	d.pending = append(d.pending, w)
	if len(d.pending) > crowdMaxWaves {
		d.pending = d.pending[len(d.pending)-crowdMaxWaves:]
	}
}

// StopWaves ends every wave at the next frame.
func (d *CrowdDirector) StopWaves(t float64) {
	d.waves, d.pending, d.replace = nil, nil, true
}

func crowdWaveEnd(w CrowdWaveFrame) float64 {
	return w.Start + (w.Laps+w.Width)/det.Abs(w.Speed)
}

func (d *CrowdDirector) prune(t float64) {
	kept := d.waves[:0]
	for _, w := range d.waves {
		if t < crowdWaveEnd(w) {
			kept = append(kept, w)
		}
	}
	d.waves = kept
}

func (d *CrowdDirector) each(selectors []string, fn func(*crowdSectionState)) error {
	indices, err := d.resolve(selectors)
	if err != nil {
		return err
	}
	for _, i := range indices {
		fn(&d.sections[i])
		d.dirty[i] = true
	}
	return nil
}

func (d *CrowdDirector) sectionFrame(i int) CrowdSectionFrame {
	s := d.sections[i]
	return CrowdSectionFrame{
		Section: i, From: crowdActions[s.from].Action, To: crowdActions[s.to].Action,
		BlendStart: s.blendStart, Blend: s.blend, ExcitementFrom: s.excFrom, ExcitementTo: s.excTo,
		ExcitementStart: s.excStart, ExcitementRamp: s.excRamp,
		Rate: s.rate, Synced: s.synced, Epoch: s.epoch, Jitter: s.jitter, Props: s.props,
	}
}

// Frame returns the sections that changed and the waves started since the
// last Frame, and clears them. A frame with no changes has no sections and no
// waves; there is no need to send it.
func (d *CrowdDirector) Frame(t float64) CrowdFrame {
	f := CrowdFrame{Crowd: d.crowd, Now: t, ReplaceWaves: d.replace}
	for i, dirty := range d.dirty {
		if dirty {
			f.Sections = append(f.Sections, d.sectionFrame(i))
			d.dirty[i] = false
		}
	}
	f.Waves, d.pending, d.replace = d.pending, nil, false
	return f
}

// FullFrame returns every section and every running wave, replacing the
// page's waves. Send it when a page mounts late or reconnects.
func (d *CrowdDirector) FullFrame(t float64) CrowdFrame {
	d.prune(t)
	f := CrowdFrame{Crowd: d.crowd, Now: t, ReplaceWaves: true, Waves: append([]CrowdWaveFrame(nil), d.waves...)}
	for i := range d.sections {
		f.Sections = append(f.Sections, d.sectionFrame(i))
	}
	return f
}

// Changed reports whether Frame has anything to send.
func (d *CrowdDirector) Changed() bool {
	if len(d.pending) > 0 || d.replace {
		return true
	}
	for _, dirty := range d.dirty {
		if dirty {
			return true
		}
	}
	return false
}

// Excitement returns the section's excitement at t (0 to 1), and false for an
// unknown section ID.
func (d *CrowdDirector) Excitement(t float64, section string) (float64, bool) {
	i, ok := d.ids[section]
	if !ok {
		return 0, false
	}
	return d.sections[i].excitementAt(t), true
}

// Action returns the section's current action, and false for an unknown
// section ID.
func (d *CrowdDirector) Action(section string) (CrowdAction, bool) {
	i, ok := d.ids[section]
	if !ok {
		return "", false
	}
	return crowdActions[d.sections[i].to].Action, true
}
