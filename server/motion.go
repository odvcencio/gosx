package server

import (
	"strconv"
	"strings"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/motion"
)

type MotionPreset string

const (
	MotionPresetFade       MotionPreset = "fade"
	MotionPresetSlideUp    MotionPreset = "slide-up"
	MotionPresetSlideDown  MotionPreset = "slide-down"
	MotionPresetSlideLeft  MotionPreset = "slide-left"
	MotionPresetSlideRight MotionPreset = "slide-right"
	MotionPresetZoomIn     MotionPreset = "zoom-in"
)

type MotionTrigger string

const (
	MotionTriggerLoad MotionTrigger = "load"
	MotionTriggerView MotionTrigger = "view"
)

const (
	defaultMotionTag      = "div"
	defaultMotionDuration = 220
	defaultMotionDelay    = 0
	defaultMotionDistance = 18
	defaultMotionEasing   = "cubic-bezier(0.16, 1, 0.3, 1)"
)

// MotionProps configures a bootstrap-managed DOM motion primitive.
type MotionProps struct {
	Tag                  string                     `json:"-"`
	Preset               MotionPreset               `json:"preset,omitempty"`
	Trigger              MotionTrigger              `json:"trigger,omitempty"`
	Duration             int                        `json:"duration,omitempty"`
	Delay                int                        `json:"delay,omitempty"`
	Easing               string                     `json:"easing,omitempty"`
	Distance             float64                    `json:"distance,omitempty"`
	RespectReducedMotion *bool                      `json:"respectReducedMotion,omitempty"`
	ReducedMotionPolicy  motion.ReducedMotionPolicy `json:"reducedMotionPolicy,omitempty"`
}

// Motion renders a DOM element opted into the shared bootstrap motion layer.
func Motion(props MotionProps, args ...any) gosx.Node {
	props = normalizeMotionProps(props)
	attrs := gosx.Attrs(gosx.Attr("data-gosx-motion", ""))
	attrs = append(attrs, gosx.ProgressiveEnhancementAttrs(gosx.ProgressiveEnhancementOptions{
		Kind:     "motion",
		Layer:    "bootstrap",
		Fallback: "html",
	})...)
	attrs = append(attrs,
		gosx.Attr("data-gosx-motion-preset", string(props.Preset)),
		gosx.Attr("data-gosx-motion-trigger", string(props.Trigger)),
		gosx.Attr("data-gosx-motion-duration", props.Duration),
		gosx.Attr("data-gosx-motion-delay", props.Delay),
		gosx.Attr("data-gosx-motion-easing", props.Easing),
		gosx.Attr("data-gosx-motion-distance", formatMotionFloat(props.Distance)),
		gosx.Attr("data-gosx-motion-respect-reduced", strconv.FormatBool(motionRespectReducedMotion(props))),
		gosx.Attr("data-gosx-motion-reduced-policy", string(props.ReducedMotionPolicy)),
		gosx.Attr("data-gosx-motion-state", "idle"),
	)
	renderArgs := []any{
		attrs,
	}
	renderArgs = append(renderArgs, args...)
	return gosx.El(props.Tag, renderArgs...)
}

// MotionScope serializes a motion.Program onto a server-rendered element. The
// bootstrap keeps the contents visible if the program is missing or invalid.
func MotionScope(program *motion.Program, args ...any) gosx.Node {
	return MotionScopeWithOptions(program, MotionScopeOptions{}, args...)
}

// MotionScopeOptions configures server-side compilation for a motion scope.
// A nil CompileCSS uses the default, which compiles fixed page-scroll motion
// to CSS (see motion.CompileCSS).
type MotionScopeOptions struct {
	CompileCSS *bool
}

// MotionScopeWithOptions renders a motion scope with explicit compiler policy.
func MotionScopeWithOptions(program *motion.Program, options MotionScopeOptions, args ...any) gosx.Node {
	attrs := gosx.Attrs()
	attrs = append(attrs, gosx.ProgressiveEnhancementAttrs(gosx.ProgressiveEnhancementOptions{
		Kind:     "motion",
		Layer:    "bootstrap",
		Fallback: "html",
	})...)
	remaining := program
	cssText := ""
	compile := options.CompileCSS == nil || *options.CompileCSS
	if compile {
		compiled, rest, err := motion.CompileCSS(program)
		if err == nil {
			cssText, remaining = compiled, rest
		}
	}
	if cssText != "" {
		attrs = append(attrs, gosx.Attr("data-gosx-motion-scope", program.ID))
	}
	// The program always ships in full: bindings compiled to CSS are listed in
	// its cssCompiled field, and the runtime keeps them as the fallback for
	// browsers without scroll timelines.
	if data, err := remaining.Marshal(); err == nil {
		attrs = append(attrs, gosx.Attr("data-gosx-motion-program", string(data)))
	} else {
		attrs = append(attrs, gosx.Attr("data-gosx-motion-error", "invalid-program"))
	}
	children := []any{attrs}
	if cssText != "" {
		// The compiler guarantees cssText holds no "</" or "<!", so raw embedding is safe.
		children = append(children, gosx.El("style", gosx.RawHTML(cssText)))
	}
	children = append(children, args...)
	return gosx.El("div", children...)
}

// Motion renders a bootstrap-managed motion element for the current page.
func (r *PageRuntime) Motion(props MotionProps, args ...any) gosx.Node {
	if r != nil {
		r.EnableBootstrap()
	}
	return Motion(props, args...)
}

// MotionScope serializes and enables a motion program for the current page.
func (r *PageRuntime) MotionScope(program *motion.Program, args ...any) gosx.Node {
	if r != nil {
		r.EnableBootstrap()
	}
	return MotionScope(program, args...)
}

// Motion renders a bootstrap-managed motion element for the current page.
func (s *PageState) Motion(props MotionProps, args ...any) gosx.Node {
	if s == nil {
		return Motion(props, args...)
	}
	return s.Runtime().Motion(props, args...)
}

// MotionScope serializes and enables a motion program for the current page.
func (s *PageState) MotionScope(program *motion.Program, args ...any) gosx.Node {
	if s == nil {
		return MotionScope(program, args...)
	}
	return s.Runtime().MotionScope(program, args...)
}

func normalizeMotionProps(props MotionProps) MotionProps {
	props.Tag = firstNonEmptyMotionString(props.Tag, defaultMotionTag)
	props.Preset = normalizeMotionPreset(props.Preset)
	props.Trigger = normalizeMotionTrigger(props.Trigger)
	if props.Duration <= 0 {
		props.Duration = defaultMotionDuration
	}
	if props.Delay < 0 {
		props.Delay = defaultMotionDelay
	}
	props.Easing = firstNonEmptyMotionString(props.Easing, defaultMotionEasing)
	if props.Distance <= 0 {
		props.Distance = defaultMotionDistance
	}
	switch props.ReducedMotionPolicy {
	case motion.ReducedMotionFade, motion.ReducedMotionStatic:
	default:
		props.ReducedMotionPolicy = motion.ReducedMotionSkip
	}
	return props
}

func normalizeMotionPreset(value MotionPreset) MotionPreset {
	switch MotionPreset(strings.ToLower(strings.TrimSpace(string(value)))) {
	case MotionPresetSlideUp, MotionPresetSlideDown, MotionPresetSlideLeft, MotionPresetSlideRight, MotionPresetZoomIn:
		return MotionPreset(strings.ToLower(strings.TrimSpace(string(value))))
	default:
		return MotionPresetFade
	}
}

func normalizeMotionTrigger(value MotionTrigger) MotionTrigger {
	switch MotionTrigger(strings.ToLower(strings.TrimSpace(string(value)))) {
	case MotionTriggerView:
		return MotionTriggerView
	default:
		return MotionTriggerLoad
	}
}

func motionRespectReducedMotion(props MotionProps) bool {
	return props.RespectReducedMotion == nil || *props.RespectReducedMotion
}

func firstNonEmptyMotionString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func formatMotionFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
