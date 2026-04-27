package game

import (
	"strings"
	"time"

	"github.com/odvcencio/gosx/engine"
)

const (
	// ProfileInteractive is the default game/runtime profile.
	ProfileInteractive = "interactive"
	// ProfileScientific trims gamepad/audio affordances and favors compute and
	// storage for academic and scientific visualization workloads.
	ProfileScientific = "scientific"
)

// Profile describes the browser/native capabilities and timing defaults that
// an interactive runtime expects.
type Profile struct {
	Name                 string
	FixedStep            time.Duration
	MaxDelta             time.Duration
	MaxSubsteps          int
	Capabilities         []engine.Capability
	RequiredCapabilities []engine.Capability
	Bindings             []Binding
}

// InteractiveProfile returns the default profile for game-like runtimes.
func InteractiveProfile() Profile {
	return Profile{
		Name:        ProfileInteractive,
		FixedStep:   time.Second / 60,
		MaxDelta:    250 * time.Millisecond,
		MaxSubsteps: 5,
		Capabilities: []engine.Capability{
			engine.CapCanvas,
			engine.CapWebGL,
			engine.CapAnimation,
			engine.CapWASM,
			engine.CapKeyboard,
			engine.CapPointer,
			engine.CapGamepad,
			engine.CapAudio,
		},
		RequiredCapabilities: []engine.Capability{
			engine.CapCanvas,
			engine.CapAnimation,
		},
		Bindings: []Binding{
			Key("move.left", "ArrowLeft"),
			Key("move.right", "ArrowRight"),
			Key("move.up", "ArrowUp"),
			Key("move.down", "ArrowDown"),
			Key("confirm", "Enter"),
			Key("cancel", "Escape"),
		},
	}
}

// ScientificProfile returns a profile for interactive academic/scientific
// scenes where pointer inspection, storage, and compute are more relevant than
// gamepad or audio.
func ScientificProfile() Profile {
	return Profile{
		Name:        ProfileScientific,
		FixedStep:   time.Second / 60,
		MaxDelta:    500 * time.Millisecond,
		MaxSubsteps: 10,
		Capabilities: []engine.Capability{
			engine.CapCanvas,
			engine.CapWebGL,
			engine.CapWebGPU,
			engine.CapCompute,
			engine.CapAnimation,
			engine.CapWASM,
			engine.CapKeyboard,
			engine.CapPointer,
			engine.CapStorage,
		},
		RequiredCapabilities: []engine.Capability{
			engine.CapCanvas,
			engine.CapAnimation,
		},
		Bindings: []Binding{
			Key("inspect", "Enter"),
			Key("cancel", "Escape"),
			Key("pan.left", "ArrowLeft"),
			Key("pan.right", "ArrowRight"),
			Key("pan.up", "ArrowUp"),
			Key("pan.down", "ArrowDown"),
		},
	}
}

func normalizeProfile(profile Profile) Profile {
	if strings.TrimSpace(profile.Name) == "" {
		profile = InteractiveProfile()
	}
	if profile.FixedStep <= 0 {
		profile.FixedStep = time.Second / 60
	}
	if profile.MaxDelta <= 0 {
		profile.MaxDelta = 250 * time.Millisecond
	}
	if profile.MaxSubsteps <= 0 {
		profile.MaxSubsteps = 5
	}
	profile.Capabilities = mergeCapabilities(profile.Capabilities, nil)
	profile.RequiredCapabilities = mergeCapabilities(profile.RequiredCapabilities, nil)
	return profile
}

func mergeCapabilities(primary, extra []engine.Capability) []engine.Capability {
	seen := map[engine.Capability]struct{}{}
	out := make([]engine.Capability, 0, len(primary)+len(extra))
	appendOne := func(capability engine.Capability) {
		normalized := engine.Capability(strings.ToLower(strings.TrimSpace(string(capability))))
		if normalized == "" {
			return
		}
		if _, ok := seen[normalized]; ok {
			return
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	for _, capability := range primary {
		appendOne(capability)
	}
	for _, capability := range extra {
		appendOne(capability)
	}
	return out
}
