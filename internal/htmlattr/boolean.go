// Package htmlattr contains shared HTML attribute classification used by the
// server renderer and the island VM.
package htmlattr

import "strings"

// IsBoolean reports whether name uses HTML presence semantics when its GoSX
// expression evaluates to a bool. Hidden is included as a bool-input attribute
// while string states such as "until-found" remain representable.
func IsBoolean(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "allowfullscreen", "alpha", "async", "autofocus", "autoplay",
		"checked", "controls", "default", "defer", "disabled",
		"formnovalidate", "headingreset", "hidden", "inert", "ismap",
		"itemscope", "loop", "multiple", "muted", "nomodule", "novalidate",
		"open", "playsinline", "readonly", "required", "reversed", "selected",
		"shadowrootclonable", "shadowrootcustomelementregistry",
		"shadowrootdelegatesfocus", "shadowrootserializable":
		return true
	default:
		return false
	}
}
