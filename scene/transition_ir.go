package scene

import "strings"

type TransitionIR struct {
	In     TransitionTimingIR `json:"in,omitempty"`
	Out    TransitionTimingIR `json:"out,omitempty"`
	Update TransitionTimingIR `json:"update,omitempty"`
}

type TransitionTimingIR struct {
	Duration int64  `json:"duration,omitempty"`
	Easing   string `json:"easing,omitempty"`
}

func lowerTransition(transition Transition) TransitionIR {
	return TransitionIR{
		In:     lowerTransitionTiming(transition.In),
		Out:    lowerTransitionTiming(transition.Out),
		Update: lowerTransitionTiming(transition.Update),
	}
}

func lowerTransitionTiming(timing TransitionTiming) TransitionTimingIR {
	return TransitionTimingIR{
		Duration: timing.Duration.Milliseconds(),
		Easing:   strings.TrimSpace(string(timing.Easing)),
	}
}

func (transition TransitionIR) isZero() bool {
	return transition.In.isZero() && transition.Out.isZero() && transition.Update.isZero()
}

func (timing TransitionTimingIR) isZero() bool {
	return timing.Duration == 0 && strings.TrimSpace(timing.Easing) == ""
}

func (transition TransitionIR) legacyProps() map[string]any {
	if transition.isZero() {
		return nil
	}
	record := map[string]any{}
	if in := transition.In.legacyProps(); len(in) > 0 {
		record["in"] = in
	}
	if out := transition.Out.legacyProps(); len(out) > 0 {
		record["out"] = out
	}
	if update := transition.Update.legacyProps(); len(update) > 0 {
		record["update"] = update
	}
	return trimEmptyRecord(record)
}

func (timing TransitionTimingIR) legacyProps() map[string]any {
	if timing.isZero() {
		return nil
	}
	record := map[string]any{}
	if timing.Duration > 0 {
		record["duration"] = timing.Duration
	}
	setString(record, "easing", timing.Easing)
	return trimEmptyRecord(record)
}
