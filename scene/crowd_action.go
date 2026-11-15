package scene

import "strings"

// CrowdAction is one animation in the crowd's action library. Each action is
// a loop baked into the crowd's bone-palette texture; a section plays one
// action at a time and blends to the next.
type CrowdAction string

const (
	// CrowdIdle sits with small fidgets. Standing sections idle on their feet.
	CrowdIdle CrowdAction = "idle"
	// CrowdStandUp stands and sways.
	CrowdStandUp CrowdAction = "stand"
	// CrowdCheer stands and pumps both fists overhead.
	CrowdCheer CrowdAction = "cheer"
	// CrowdClap stands and claps in front of the chest, one clap per cycle.
	CrowdClap CrowdAction = "clap"
	// CrowdWaveArms stands with both arms overhead, swaying side to side.
	CrowdWaveArms CrowdAction = "wave-arms"
	// CrowdJump jumps on the spot, one landing per cycle.
	CrowdJump CrowdAction = "jump"
	// CrowdTwirl stands and circles one hand overhead, for towels and scarves.
	CrowdTwirl CrowdAction = "twirl"
	// CrowdHoldUp stands and holds both hands up, for lights and scarves.
	CrowdHoldUp CrowdAction = "hold-up"
	// CrowdRideWave is the travelling-wave motion: rise with arms up, then sit. A
	// section never plays it as its action; CrowdDirector.StartWave drives
	// it fan by fan as the wave front passes.
	CrowdRideWave CrowdAction = "wave"
)

// CrowdActionInfo describes one library action.
type CrowdActionInfo struct {
	Action CrowdAction
	// Index is the action's row group in the palette texture and its code in
	// crowd frames.
	Index int
	// Cycle is the loop length in seconds at playback rate 1.
	Cycle float64
	// Beat is the fraction of the cycle at which the action makes its sound
	// (hands meet, feet land, a shout peaks), or -1 when it has none.
	Beat float64
	// Loudness is the action's contribution to crowd noise, from 0 to 1, for
	// a fully excited section.
	Loudness float64
}

// crowdActions is the library in index order. The browser's crowd-rig.ts
// bakes the same actions in the same order; a fixture test pins the two.
var crowdActions = [...]CrowdActionInfo{
	{CrowdIdle, 0, 4.0, -1, 0.08},
	{CrowdStandUp, 1, 3.0, -1, 0.3},
	{CrowdCheer, 2, 0.8, 0.25, 1.0},
	{CrowdClap, 3, 0.5, 0.5, 0.65},
	{CrowdWaveArms, 4, 1.6, -1, 0.75},
	{CrowdJump, 5, 0.7, 0.92, 0.9},
	{CrowdTwirl, 6, 0.6, -1, 0.7},
	{CrowdHoldUp, 7, 2.4, -1, 0.45},
	{CrowdRideWave, 8, 1.0, 0.5, 0.85},
}

// CrowdActions lists the action library in index order.
func CrowdActions() []CrowdActionInfo {
	out := make([]CrowdActionInfo, len(crowdActions))
	copy(out, crowdActions[:])
	return out
}

// CrowdActionByName returns the library entry for action. Empty means
// CrowdIdle.
func CrowdActionByName(action CrowdAction) (CrowdActionInfo, bool) {
	index, ok := crowdActionIndex(action)
	if !ok {
		return CrowdActionInfo{}, false
	}
	return crowdActions[index], true
}

func crowdActionIndex(action CrowdAction) (int, bool) {
	name := CrowdAction(strings.TrimSpace(string(action)))
	if name == "" {
		return 0, true
	}
	for _, info := range crowdActions {
		if info.Action == name {
			return info.Index, true
		}
	}
	return 0, false
}
