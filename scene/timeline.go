package scene

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"m31labs.dev/gosx/motion"
)

// Tween animates one scalar Scene3D property in scene units or radians.
// Node names a stable object ID. Set Camera instead to animate the active camera.
// From and To are absolute values; unspecified properties keep their current values.
type Tween struct {
	Node     string
	Camera   bool
	Property string
	From     float64
	To       float64
	At       time.Duration
	Duration time.Duration
	Ease     motion.Ease
}

// Timeline sequences finite tweens. At adds parallel steps; Then appends steps
// after everything already scheduled. JSON is a versioned browser playback plan.
type Timeline struct {
	ID     string
	Tweens []Tween
}

func NewTimeline(id string) *Timeline { return &Timeline{ID: id} }

// At schedules all tweens at offset, measured from playback start.
func (t *Timeline) At(offset time.Duration, tweens ...Tween) *Timeline {
	for _, tween := range tweens {
		tween.At = offset
		tween.Ease.Args = append([]float64(nil), tween.Ease.Args...)
		t.Tweens = append(t.Tweens, tween)
	}
	return t
}

// Then schedules parallel tweens after the latest existing end time.
func (t *Timeline) Then(tweens ...Tween) *Timeline { return t.At(t.Duration(), tweens...) }

// Duration returns the latest end time. Validate before playing an authored plan.
func (t *Timeline) Duration() time.Duration {
	var duration time.Duration
	if t != nil {
		for _, tween := range t.Tweens {
			if end := tween.At + tween.Duration; end > duration {
				duration = end
			}
		}
	}
	return duration
}

var tweenNodeProperties = map[string]bool{
	"x": true, "y": true, "z": true, "rotationX": true, "rotationY": true, "rotationZ": true,
	"scaleX": true, "scaleY": true, "scaleZ": true, "opacity": true,
}

var tweenCameraProperties = map[string]bool{
	"x": true, "y": true, "z": true, "rotationX": true, "rotationY": true, "rotationZ": true, "fov": true,
}

type tweenTrackKey struct {
	camera         bool
	node, property string
}

// A comparable key avoids formatting strings while validating and sampling.
func tweenKey(t Tween) tweenTrackKey { return tweenTrackKey{t.Camera, t.Node, t.Property} }

// Validate rejects unsupported properties, non-finite values, and overlapping
// tracks for the same property. Durations are bounded to one day and 1,024 tweens.
func (t *Timeline) Validate() error {
	if t == nil || t.ID == "" || len(t.Tweens) == 0 || len(t.Tweens) > 1024 {
		return fmt.Errorf("scene timeline needs an ID and 1..1024 tweens")
	}
	finite := func(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
	for i, tween := range t.Tweens {
		validProperty := tweenNodeProperties[tween.Property] && tween.Node != ""
		if tween.Camera {
			validProperty = tweenCameraProperties[tween.Property] && tween.Node == ""
		}
		if !validProperty || !finite(tween.From) || !finite(tween.To) || tween.At < 0 || tween.Duration < 0 ||
			tween.At > 24*time.Hour || tween.Duration > 24*time.Hour-tween.At {
			return fmt.Errorf("scene timeline tween %d has an invalid target, value, or timing", i)
		}
		if tween.Ease.Kind > motion.EaseInOutBack {
			return fmt.Errorf("scene timeline tween %d has an unknown easing kind", i)
		}
		for _, arg := range tween.Ease.Args {
			if !finite(arg) {
				return fmt.Errorf("scene timeline tween %d has a non-finite easing argument", i)
			}
		}
		if (tween.Ease.Kind >= motion.EaseInPow && tween.Ease.Kind <= motion.EaseInOutPow || tween.Ease.Kind == motion.EaseSteps) &&
			len(tween.Ease.Args) > 0 && tween.Ease.Args[0] <= 0 {
			return fmt.Errorf("scene timeline tween %d needs a positive easing argument", i)
		}
		if tween.Ease.Kind == motion.EaseCubicBezier && (len(tween.Ease.Args) != 4 || tween.Ease.Args[0] < 0 || tween.Ease.Args[0] > 1 || tween.Ease.Args[2] < 0 || tween.Ease.Args[2] > 1) {
			return fmt.Errorf("scene timeline tween %d needs four bezier arguments with x in [0,1]", i)
		}
		for _, previous := range t.Tweens[:i] {
			if tweenKey(previous) == tweenKey(tween) && (previous.At == tween.At || previous.At < tween.At+tween.Duration && tween.At < previous.At+previous.Duration) {
				return fmt.Errorf("scene timeline tween %d overlaps the same property", i)
			}
		}
	}
	return nil
}

type timelineTweenJSON struct {
	Node     string            `json:"node,omitempty"`
	Camera   bool              `json:"camera,omitempty"`
	Property string            `json:"property"`
	From     float64           `json:"from"`
	To       float64           `json:"to"`
	At       float64           `json:"at"`
	Duration float64           `json:"duration"`
	Ease     motion.MotionEase `json:"ease"`
}

// MarshalJSON validates the plan and writes seconds rather than Go nanoseconds.
func (t *Timeline) MarshalJSON() ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	tracks := make([]timelineTweenJSON, len(t.Tweens))
	for i, tween := range t.Tweens {
		tracks[i] = timelineTweenJSON{tween.Node, tween.Camera, tween.Property, tween.From, tween.To,
			tween.At.Seconds(), tween.Duration.Seconds(), motion.MotionEase{Kind: tween.Ease.Kind, Args: tween.Ease.Args}}
	}
	return json.Marshal(struct {
		Version int                 `json:"version"`
		ID      string              `json:"id"`
		Tweens  []timelineTweenJSON `json:"tweens"`
	}{1, t.ID, tracks})
}

// UnmarshalJSON reads the versioned seconds-based browser plan.
func (t *Timeline) UnmarshalJSON(data []byte) error {
	var wire struct {
		Version int                 `json:"version"`
		ID      string              `json:"id"`
		Tweens  []timelineTweenJSON `json:"tweens"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Version != 1 {
		return fmt.Errorf("unsupported scene timeline version %d", wire.Version)
	}
	next := Timeline{ID: wire.ID}
	for _, tween := range wire.Tweens {
		if tween.At < 0 || tween.Duration < 0 || tween.At > 86400 || tween.Duration > 86400-tween.At {
			return fmt.Errorf("scene timeline timing is outside one day")
		}
		next.Tweens = append(next.Tweens, Tween{Node: tween.Node, Camera: tween.Camera, Property: tween.Property,
			From: tween.From, To: tween.To, At: time.Duration(math.Round(tween.At * float64(time.Second))),
			Duration: time.Duration(math.Round(tween.Duration * float64(time.Second))),
			Ease:     motion.Ease{Kind: tween.Ease.Kind, Args: tween.Ease.Args}})
	}
	if err := next.Validate(); err != nil {
		return err
	}
	*t = next
	return nil
}

// TimelinePlayer prepares and validates tracks once for repeated native sampling.
type TimelinePlayer struct {
	tracks []Tween
}

func NewTimelinePlayer(t *Timeline) (*TimelinePlayer, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	tracks := append([]Tween(nil), t.Tweens...)
	for i := range tracks {
		tracks[i].Ease.Args = append([]float64(nil), tracks[i].Ease.Args...)
	}
	sort.SliceStable(tracks, func(i, j int) bool { return tracks[i].At < tracks[j].At })
	return &TimelinePlayer{tracks: tracks}, nil
}

// Sample evaluates seconds since playback began into compact property patches.
// The first From value holds before a property's start; the last To holds after
// its end. Gaps hold the previous To. Sampling is deterministic and seekable.
func (t *Timeline) Sample(seconds float64) ([]Command, error) {
	player, err := NewTimelinePlayer(t)
	if err != nil {
		return nil, err
	}
	return player.Sample(seconds)
}

// Sample evaluates a prepared plan without sorting or validating its tracks again.
func (p *TimelinePlayer) Sample(seconds float64) ([]Command, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return nil, fmt.Errorf("scene timeline sample time must be finite")
	}
	if p == nil {
		return nil, fmt.Errorf("scene timeline player is nil")
	}
	commands := []Command{}
	indices := map[string]int{}
	seen := map[tweenTrackKey]bool{}
	for _, tween := range p.tracks {
		key := tweenKey(tween)
		if seconds < tween.At.Seconds() && seen[key] {
			continue
		}
		seen[key] = true
		progress := 0.0
		if seconds >= tween.At.Seconds() {
			progress = 1
			if tween.Duration > 0 {
				progress = (seconds - tween.At.Seconds()) / tween.Duration.Seconds()
			}
		}
		value := tween.From + (tween.To-tween.From)*tween.Ease.Apply(progress)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("scene timeline sample overflowed")
		}
		target := "node:" + tween.Node
		kind := CommandSetTransform
		if tween.Camera {
			target, kind = "camera:", CommandSetCamera
		}
		index, exists := indices[target]
		if !exists {
			index = len(commands)
			indices[target] = index
			commands = append(commands, Command{Kind: kind, ObjectID: tween.Node, Data: map[string]float64{}})
		}
		commands[index].Data.(map[string]float64)[tween.Property] = value
	}
	return commands, nil
}
