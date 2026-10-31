package ir

import "testing"

// The validation patterns compile on first use (internal/lazyre), so a mistyped
// expression would panic inside `gosx check` instead of at start-up. This test
// compiles each one and spot-checks what it accepts.
func TestValidationPatternsCompileAndMatch(t *testing.T) {
	cases := []struct {
		name    string
		pattern interface{ MatchString(string) bool }
		yes, no string
	}{
		{"countdownThresholdIntegerPattern", countdownThresholdIntegerPattern, "30", "3s"},
		{"countdownThresholdDurationPattern", countdownThresholdDurationPattern, "1h30m", "1d"},
		{"countdownWarnClassTokenPattern", countdownWarnClassTokenPattern, "soon", "two words"},
		{"pollIntervalValuePattern", pollIntervalValuePattern, "90s", "1h"},
		{"liveBindKeyPattern", liveBindKeyPattern, "ship.speed", "ship..speed"},
		{"liveBindAttrTargetNamePattern", liveBindAttrTargetNamePattern, "aria-label", "1st"},
		{"watchEffectTokenPattern", watchEffectTokenPattern, "cue:chime", "cue:"},
		{"strictBindingNamePattern", strictBindingNamePattern, "row2", "Row"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !tc.pattern.MatchString(tc.yes) {
				t.Errorf("does not match %q", tc.yes)
			}
			if tc.pattern.MatchString(tc.no) {
				t.Errorf("matches %q", tc.no)
			}
		})
	}
}
