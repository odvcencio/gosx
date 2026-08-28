package ir

import "strings"

// IsSharedImportPath reports whether path is a shared import per the shared
// components design: a "./"- or "../"-prefixed relative directory
// reference, never a legal Go import path in module mode. It is the ir-side
// twin of transpile's unexported isSharedImportPath — duplicated rather
// than shared, because ir cannot import transpile (transpile already
// imports ir) and the rule itself is a two-line string check unlikely to
// drift.
func IsSharedImportPath(path string) bool {
	return strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

// SplitMemberTag splits a dotted component tag ("ui.TeamMark") into its
// alias ("ui") and component ("TeamMark") segments. It is the ir-side twin
// of transpile's unexported splitMemberTag; see IsSharedImportPath's doc
// comment for why the two packages each carry their own copy.
func SplitMemberTag(tag string) (alias, component string, ok bool) {
	parts := strings.Split(tag, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
