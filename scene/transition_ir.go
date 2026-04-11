package scene

import "strings"

type TransitionIR struct {
	In     TransitionTimingIR `json:"in,omitzero"`
	Out    TransitionTimingIR `json:"out,omitzero"`
	Update TransitionTimingIR `json:"update,omitzero"`
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

// IsZero reports whether all three transition phases are empty. Exported
// (capital I) so encoding/json's `omitzero` tag recognizes it on fields
// of type TransitionIR. That lets parent structs with
// `json:"transition,omitzero"` skip the field entirely on the marshal
// fast path, matching the old legacyProps behavior of not emitting a
// transition key for zero-valued transitions.
func (transition TransitionIR) IsZero() bool {
	return transition.In.IsZero() && transition.Out.IsZero() && transition.Update.IsZero()
}

// IsZero is the TransitionTimingIR counterpart to TransitionIR.IsZero —
// same reason (json omitzero tag recognition).
func (timing TransitionTimingIR) IsZero() bool {
	return timing.Duration == 0 && strings.TrimSpace(timing.Easing) == ""
}

func (transition TransitionIR) legacyProps() map[string]any {
	if transition.IsZero() {
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
	if timing.IsZero() {
		return nil
	}
	record := map[string]any{}
	if timing.Duration > 0 {
		record["duration"] = timing.Duration
	}
	setString(record, "easing", timing.Easing)
	return trimEmptyRecord(record)
}
