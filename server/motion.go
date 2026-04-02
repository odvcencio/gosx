package server

import (
	"strconv"
	"strings"

	"github.com/odvcencio/gosx"
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
	Tag                  string        `json:"-"`
	Preset               MotionPreset  `json:"preset,omitempty"`
	Trigger              MotionTrigger `json:"trigger,omitempty"`
	Duration             int           `json:"duration,omitempty"`
	Delay                int           `json:"delay,omitempty"`
	Easing               string        `json:"easing,omitempty"`
	Distance             float64       `json:"distance,omitempty"`
	RespectReducedMotion *bool         `json:"respectReducedMotion,omitempty"`
}

// Motion renders a DOM element opted into the shared bootstrap motion layer.
func Motion(props MotionProps, args ...any) gosx.Node {
	props = normalizeMotionProps(props)
	renderArgs := []any{
		gosx.Attrs(
			gosx.Attr("data-gosx-motion", ""),
			gosx.Attr("data-gosx-enhance", "motion"),
			gosx.Attr("data-gosx-enhance-layer", "bootstrap"),
			gosx.Attr("data-gosx-fallback", "html"),
			gosx.Attr("data-gosx-motion-preset", string(props.Preset)),
			gosx.Attr("data-gosx-motion-trigger", string(props.Trigger)),
			gosx.Attr("data-gosx-motion-duration", props.Duration),
			gosx.Attr("data-gosx-motion-delay", props.Delay),
			gosx.Attr("data-gosx-motion-easing", props.Easing),
			gosx.Attr("data-gosx-motion-distance", formatMotionFloat(props.Distance)),
			gosx.Attr("data-gosx-motion-respect-reduced", strconv.FormatBool(motionRespectReducedMotion(props))),
			gosx.Attr("data-gosx-motion-state", "idle"),
		),
	}
	renderArgs = append(renderArgs, args...)
	return gosx.El(props.Tag, renderArgs...)
}

// Motion renders a bootstrap-managed motion element for the current page.
func (r *PageRuntime) Motion(props MotionProps, args ...any) gosx.Node {
	if r != nil {
		r.EnableBootstrap()
	}
	return Motion(props, args...)
}

// Motion renders a bootstrap-managed motion element for the current page.
func (s *PageState) Motion(props MotionProps, args ...any) gosx.Node {
	if s == nil {
		return Motion(props, args...)
	}
	return s.Runtime().Motion(props, args...)
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
