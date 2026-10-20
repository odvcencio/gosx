package motion

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// TransportOptions gives a motion scope one seekable playhead. Curves driven
// by Playhead sample absolute seconds, so DOM, camera and material bindings
// reproduce the same pose when seeking backwards. Stateful simulations need
// their own replay state; a transport does not rewind simulation history.
type TransportOptions struct {
	Duration time.Duration
	AutoPlay bool
	Labels   map[string]time.Duration
	// Scenes selects Scene3D mounts inside the scope whose shader clocks follow
	// this playhead. Deferred mounts receive the latest time when they become ready.
	Scenes []string
}

// TransportSpec is the serializable browser transport contract. Durations and
// labels use milliseconds, matching browser animation and capture controls.
type TransportSpec struct {
	DurationMS float64            `json:"durationMs"`
	AutoPlay   bool               `json:"autoPlay,omitempty"`
	Labels     map[string]float64 `json:"labels,omitempty"`
	Scenes     []string           `json:"scenes,omitempty"`
}

// WithTransport configures the scope's playhead and returns p for chaining.
func (p *Program) WithTransport(options TransportOptions) *Program {
	spec := &TransportSpec{DurationMS: float64(options.Duration) / float64(time.Millisecond), AutoPlay: options.AutoPlay,
		Scenes: append([]string(nil), options.Scenes...)}
	if len(options.Labels) > 0 {
		spec.Labels = make(map[string]float64, len(options.Labels))
		for label, at := range options.Labels {
			spec.Labels[label] = float64(at) / float64(time.Millisecond)
		}
	}
	p.Transport = spec
	for _, binding := range p.Bindings {
		if binding.Target == BindingSceneTransition {
			p.addTransitionScene(binding.Selector)
		}
	}
	return p
}

// Playhead adds the transport's absolute time in seconds. Feed it to Curve,
// Map or Clamp and use the existing DOM, camera, node and uniform bindings.
func (p *Program) Playhead(id string) SignalRef {
	return p.add(SignalSpec{ID: id, Kind: SignalPlayhead})
}

func (s *TransportSpec) validate() error {
	if s == nil {
		return nil
	}
	if math.IsNaN(s.DurationMS) || math.IsInf(s.DurationMS, 0) || s.DurationMS < 0 || s.DurationMS > 86_400_000 {
		return fmt.Errorf("motion: transport duration must be 0..86400000 milliseconds")
	}
	if len(s.Labels) > 128 || len(s.Scenes) > 64 {
		return fmt.Errorf("motion: transport exceeds 128 labels or 64 scene selectors")
	}
	for name, at := range s.Labels {
		if strings.TrimSpace(name) == "" || len(name) > 128 || math.IsNaN(at) || math.IsInf(at, 0) || at < 0 || at > s.DurationMS {
			return fmt.Errorf("motion: invalid transport label %q", name)
		}
	}
	for _, selector := range s.Scenes {
		if strings.TrimSpace(selector) == "" {
			return fmt.Errorf("motion: transport scene selector is required")
		}
	}
	return nil
}
