package gosx

import (
	"strings"
)

// Runtime contract attribute names shared by server-rendered GoSX surfaces and
// the browser bootstrap. Keeping these in the core package lets ecosystem
// packages describe their markup without importing an optional product module.
const (
	RuntimeSurfaceAttr        = "data-gosx-runtime-surface"
	RuntimeSurfaceVersionAttr = "data-gosx-runtime-surface-version"
	RuntimeSurfaceStateAttr   = "data-gosx-runtime-state"
	RuntimeFallbackAttr       = "data-gosx-fallback"

	EnhancementAttr      = "data-gosx-enhance"
	EnhancementLayerAttr = "data-gosx-enhance-layer"
	ManagedFormAttr      = "data-gosx-form"
	ManagedFormModeAttr  = "data-gosx-form-mode"
	ManagedFormStateAttr = "data-gosx-form-state"

	ActionAttr           = "data-gosx-action"
	ActionResetAttr      = "data-gosx-reset"
	ActionSubmitOnAttr   = "data-gosx-submit-on"
	ActionEventAttr      = "data-gosx-action-event"
	ActionSignalAttr     = "data-gosx-action-signal"
	ActionTargetAttr     = "data-gosx-action-target"
	RegionAttr           = "data-gosx-region"
	RegionURLAttr        = "data-gosx-region-url"
	RegionSignalAttr     = "data-gosx-region-signal"
	RegionEventsAttr     = "data-gosx-region-on"
	RegionFieldAttr      = "data-gosx-region-field"
	RegionAllowEmptyAttr = "data-gosx-region-allow-empty"
)

// ProgressiveEnhancementOptions describes a browser enhancement while
// leaving the server-rendered HTML as the fallback. The vocabulary is shared
// by navigation, motion, text-layout, editor, and other optional surfaces.
type ProgressiveEnhancementOptions struct {
	Kind     string
	Layer    string
	Fallback string
}

// ProgressiveEnhancementAttrs returns the framework-owned enhancement
// attributes. Empty values are omitted so a caller can add only the contract
// fields it needs.
func ProgressiveEnhancementAttrs(opts ProgressiveEnhancementOptions) AttrList {
	attrs := AttrList(nil)
	if value := strings.TrimSpace(opts.Kind); value != "" {
		attrs = append(attrs, Attr(EnhancementAttr, value))
	}
	if value := strings.TrimSpace(opts.Layer); value != "" {
		attrs = append(attrs, Attr(EnhancementLayerAttr, value))
	}
	if value := strings.TrimSpace(opts.Fallback); value != "" {
		attrs = append(attrs, Attr(RuntimeFallbackAttr, value))
	}
	return attrs
}

// ManagedFormOptions describes the bootstrap-managed form contract. Native
// form submission remains the fallback when the browser runtime is absent or
// the form cannot be safely enhanced.
type ManagedFormOptions struct {
	Method   string
	State    string
	Layer    string
	Fallback string
}

// ManagedFormAttrs returns the standard managed-form and progressive-
// enhancement attributes. Method is normalized to the GET/POST vocabulary the
// navigation runtime accepts; the HTML method attribute remains authoritative
// when Method is omitted.
func ManagedFormAttrs(opts ManagedFormOptions) AttrList {
	attrs := Attrs(BoolAttr(ManagedFormAttr))
	if method := strings.ToLower(strings.TrimSpace(opts.Method)); method == "get" || method == "post" {
		attrs = append(attrs, Attr(ManagedFormModeAttr, method))
	}
	state := strings.TrimSpace(opts.State)
	if state == "" {
		state = "idle"
	}
	attrs = append(attrs, Attr(ManagedFormStateAttr, state))
	layer := strings.TrimSpace(opts.Layer)
	if layer == "" {
		layer = "bootstrap"
	}
	fallback := strings.TrimSpace(opts.Fallback)
	if fallback == "" {
		fallback = "native-form"
	}
	attrs = append(attrs, ProgressiveEnhancementAttrs(ProgressiveEnhancementOptions{
		Kind:     "form",
		Layer:    layer,
		Fallback: fallback,
	})...)
	return attrs
}

// RuntimeSurfaceOptions describes the framework-owned attributes on an
// optional browser surface. The rendered HTML remains a complete fallback;
// the bootstrap only enhances it when a matching factory is registered.
type RuntimeSurfaceOptions struct {
	Name     string
	Version  string
	Fallback string
}

// RuntimeSurfaceAttrs returns the standard attributes for an opt-in browser
// surface. Empty optional values are omitted so callers can add their own
// product-specific data attributes without producing noisy markup.
func RuntimeSurfaceAttrs(opts RuntimeSurfaceOptions) AttrList {
	attrs := Attrs(
		Attr(RuntimeSurfaceAttr, strings.TrimSpace(opts.Name)),
	)
	if version := strings.TrimSpace(opts.Version); version != "" {
		attrs = append(attrs, Attr(RuntimeSurfaceVersionAttr, version))
	}
	if fallback := strings.TrimSpace(opts.Fallback); fallback != "" {
		attrs = append(attrs, Attr(RuntimeFallbackAttr, fallback))
	}
	return attrs
}

// RuntimeSurface renders an element opted into a named GoSX browser surface.
// The caller may provide additional attributes and children; the server
// rendered element remains the progressive-enhancement fallback.
func RuntimeSurface(tag string, opts RuntimeSurfaceOptions, args ...any) Node {
	return runtimeContractElement(tag, RuntimeSurfaceAttrs(opts), args...)
}

// ActionOptions describes a declarative request handled by the GoSX
// bootstrap. URL is kept separate from Method so server code cannot silently
// disagree about the wire format used by data-gosx-action.
type ActionOptions struct {
	Method   string
	URL      string
	Reset    bool
	SubmitOn string
	// Event names an additional browser lifecycle event for the response.
	Event string
	// Signal receives result.value (or result.data.value) when supplied by the
	// action response.
	Signal string
	// Target receives result.html (or result.data.html) when supplied by the
	// action response.
	Target string
}

// ActionAttrs returns the typed declarative-action contract as HTML
// attributes. The action value is the stable "METHOD URL" transport string.
func ActionAttrs(opts ActionOptions) AttrList {
	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	url := strings.TrimSpace(opts.URL)
	spec := url
	if method != "" && url != "" {
		spec = method + " " + url
	}
	attrs := Attrs(Attr(ActionAttr, spec))
	if opts.Reset {
		attrs = append(attrs, BoolAttr(ActionResetAttr))
	}
	if value := strings.TrimSpace(opts.SubmitOn); value != "" {
		attrs = append(attrs, Attr(ActionSubmitOnAttr, value))
	}
	if value := strings.TrimSpace(opts.Event); value != "" {
		attrs = append(attrs, Attr(ActionEventAttr, value))
	}
	if value := strings.TrimSpace(opts.Signal); value != "" {
		attrs = append(attrs, Attr(ActionSignalAttr, value))
	}
	if value := strings.TrimSpace(opts.Target); value != "" {
		attrs = append(attrs, Attr(ActionTargetAttr, value))
	}
	return attrs
}

// Action renders an element with the typed declarative-action contract.
func Action(tag string, opts ActionOptions, args ...any) Node {
	return runtimeContractElement(tag, ActionAttrs(opts), args...)
}

// RegionOptions describes a server-rendered HTML region that the bootstrap
// may refresh after a signal or hub event. The initial children remain the
// progressive-enhancement fallback.
type RegionOptions struct {
	URL        string
	Signal     string
	Events     []string
	Field      string
	AllowEmpty bool
}

// RegionAttrs returns the typed declarative-region contract as HTML
// attributes. Events are normalized to the space-separated form consumed by
// the browser runtime.
func RegionAttrs(opts RegionOptions) AttrList {
	attrs := Attrs(
		BoolAttr(RegionAttr),
		Attr(RegionURLAttr, strings.TrimSpace(opts.URL)),
	)
	if value := strings.TrimSpace(opts.Signal); value != "" {
		attrs = append(attrs, Attr(RegionSignalAttr, value))
	}
	if len(opts.Events) > 0 {
		events := make([]string, 0, len(opts.Events))
		for _, event := range opts.Events {
			if value := strings.TrimSpace(event); value != "" {
				events = append(events, value)
			}
		}
		if len(events) > 0 {
			attrs = append(attrs, Attr(RegionEventsAttr, strings.Join(events, " ")))
		}
	}
	if value := strings.TrimSpace(opts.Field); value != "" {
		attrs = append(attrs, Attr(RegionFieldAttr, value))
	}
	if opts.AllowEmpty {
		attrs = append(attrs, BoolAttr(RegionAllowEmptyAttr))
	}
	return attrs
}

// Region renders an element with the typed declarative-region contract. Its
// children are the server-rendered fallback shown before enhancement.
func Region(tag string, opts RegionOptions, args ...any) Node {
	return runtimeContractElement(tag, RegionAttrs(opts), args...)
}

func runtimeContractElement(tag string, attrs AttrList, args ...any) Node {
	values := make([]any, 0, len(args)+1)
	values = append(values, attrs)
	values = append(values, args...)
	return El(tag, values...)
}
