package markup

import "strings"

// Managed-form and progressive-enhancement attribute names. They live here,
// not with the rest of the runtime contract in package gosx, because
// RenderHTML expands ManagedFormShorthandAttr on a <form> element. Package
// gosx re-exports each one under the same name.
const (
	RuntimeFallbackAttr = "data-gosx-fallback"

	EnhancementAttr      = "data-gosx-enhance"
	EnhancementLayerAttr = "data-gosx-enhance-layer"
	ManagedFormAttr      = "data-gosx-form"
	ManagedFormModeAttr  = "data-gosx-form-mode"
	ManagedFormStateAttr = "data-gosx-form-state"
	// ManagedFormShorthandAttr is a single-attribute alternative to writing
	// out the ManagedFormAttrs default set by hand on a <form> element. A
	// .gsx template author writes `<form data-gosx-managed>` (or
	// `data-gosx-managed="true"`) instead of the five contract attributes.
	// All three server render paths expand it on a <form> element at
	// render time: RenderHTML (see expandManagedFormAttrs in node.go) for
	// the Go Node API, the route package's file-program renderer for .gsx
	// pages, and the island package's resolved-tree renderer for island
	// forms. See ManagedFormShorthandTruthy for the shared truthy rule.
	ManagedFormShorthandAttr = "data-gosx-managed"
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

// ManagedFormShorthandTruthy reports whether a ManagedFormShorthandAttr
// value opts a <form> element into the managed-form contract. The exact
// rule: a bare attribute (presence with no value) is truthy; a nil value —
// the attribute was never found — is falsy; every other value is truthy
// unless it equals "false" (case-insensitive, surrounding whitespace
// ignored). This makes `data-gosx-managed`, `data-gosx-managed=""`, and
// `data-gosx-managed="true"` all opt in; only `data-gosx-managed="false"`
// opts out. Treating an empty string as truthy mirrors the DOM: a bare
// HTML boolean attribute and its `=""` spelling parse identically, and the
// browser-side mirror of this rule (managedFormShorthandTruthy in
// client/runtime/host/navigation.ts) has no way to tell them apart either,
// since both read back as the empty string from getAttribute.
//
// This is the single definition of the shorthand's truthy rule. All three
// server render paths call it: node.go's expandManagedFormAttrs for the Go
// Node API, the route package's file-program renderer for .gsx pages, and
// the island package's resolved-tree renderer for island forms — so a form
// written in any surface expands the same way. A caller decides separately
// whether the attribute is present at all; this function only judges a
// value already known to exist (or its explicit absence, via a nil value).
func ManagedFormShorthandTruthy(presence bool, value any) bool {
	if presence {
		return true
	}
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return !strings.EqualFold(strings.TrimSpace(v), "false")
	default:
		return true
	}
}
