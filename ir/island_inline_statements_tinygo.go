//go:build tinygo || js

package ir

func islandInlineStatements(source string) []string { return []string{source} }
