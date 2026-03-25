// Package css provides lightweight CSS scoping for GoSX components.
//
// Sidecar CSS files (component.gsx + component.css) are scoped by
// adding a data attribute to the component root and prefixing all
// CSS selectors with that attribute. This prevents style collisions
// between islands without requiring CSS-in-JS.
//
// Example:
//
//	Input (counter.css):
//	  .button { color: red; }
//
//	Output (scoped):
//	  [data-gosx-s="c8a3"] .button { color: red; }
//
// The component root gets: <div data-gosx-s="c8a3">
package css

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ScopeID generates a short scope identifier from the component name.
func ScopeID(componentName string) string {
	h := sha256.Sum256([]byte(componentName))
	return hex.EncodeToString(h[:2]) // 4 hex chars
}

// ScopeCSS prefixes all CSS selectors with the scoping attribute selector.
// This scopes the CSS to only affect elements within the component.
func ScopeCSS(css string, scopeID string) string {
	attr := `[data-gosx-s="` + scopeID + `"]`
	return scopeSelectors(css, attr)
}

// ScopeAttr returns the HTML attribute to add to the component root.
func ScopeAttr(scopeID string) string {
	return `data-gosx-s="` + scopeID + `"`
}

// scopeSelectors is a simple CSS selector prefixer.
// It handles:
//   - Simple selectors: .foo → [data-gosx-s="x"] .foo
//   - Multiple selectors: .a, .b → [data-gosx-s="x"] .a, [data-gosx-s="x"] .b
//   - Nested rules: keeps @media/@keyframes blocks intact
//   - Comments: preserves them
//   - :root pseudo: maps to the scoping attribute
func scopeSelectors(css string, attr string) string {
	var result strings.Builder
	result.Grow(len(css) * 2)

	lines := strings.Split(css, "\n")
	inBlock := 0
	inAtRule := false
	_ = inAtRule

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "//") {
			result.WriteString(line)
			result.WriteByte('\n')
			continue
		}

		// Track @-rules (don't scope their selectors)
		if strings.HasPrefix(trimmed, "@media") || strings.HasPrefix(trimmed, "@keyframes") ||
			strings.HasPrefix(trimmed, "@supports") || strings.HasPrefix(trimmed, "@font-face") {
			inAtRule = true
			result.WriteString(line)
			result.WriteByte('\n')
			continue
		}

		// Track block depth
		openBraces := strings.Count(trimmed, "{")
		closeBraces := strings.Count(trimmed, "}")
		inBlock += openBraces - closeBraces

		// Lines with { are selector lines (unless inside @-rule declarations)
		if openBraces > 0 && !strings.HasPrefix(trimmed, "}") {
			// This is a selector line — scope it
			selectorPart := trimmed[:strings.Index(trimmed, "{")]
			blockPart := trimmed[strings.Index(trimmed, "{"):]

			// Split on commas for multiple selectors
			selectors := strings.Split(selectorPart, ",")
			var scoped []string
			for _, sel := range selectors {
				sel = strings.TrimSpace(sel)
				if sel == "" {
					continue
				}
				// :root → scope attr itself
				if sel == ":root" {
					scoped = append(scoped, attr)
				} else {
					scoped = append(scoped, attr+" "+sel)
				}
			}

			// Preserve original indentation
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			result.WriteString(indent)
			result.WriteString(strings.Join(scoped, ", "))
			result.WriteString(" ")
			result.WriteString(blockPart)
			result.WriteByte('\n')
			continue
		}

		// Closing brace — if we exit all blocks and were in @-rule, reset
		if closeBraces > 0 && inBlock <= 0 {
			inAtRule = false
			inBlock = 0
		}

		result.WriteString(line)
		result.WriteByte('\n')
	}

	return result.String()
}
