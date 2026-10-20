package evalparity

import "fmt"

// validateCases checks the table-level invariants the harness and the
// generated support matrix both depend on: unique IDs, props fields/
// literal/value all present or all absent together, and no case marking
// one backend both Unsupported and Diverges (those are mutually
// exclusive: a backend either fails to run at all, or runs and produces a
// pinned wrong answer).
func validateCases(cs []Case) error {
	seen := make(map[string]bool, len(cs))
	for _, c := range cs {
		if c.ID == "" {
			return fmt.Errorf("case with empty ID (Category %q)", c.Category)
		}
		if seen[c.ID] {
			return fmt.Errorf("duplicate case ID %q", c.ID)
		}
		seen[c.ID] = true

		hasFields := c.PropsFields != ""
		hasLiteral := c.PropsLiteral != ""
		hasValue := c.PropsValue != nil
		if hasFields != hasLiteral || hasFields != hasValue {
			return fmt.Errorf("case %q: PropsFields (%v), PropsLiteral (%v), and PropsValue (%v) must all be set together or all empty",
				c.ID, hasFields, hasLiteral, hasValue)
		}

		for b, reason := range c.Unsupported {
			if reason == "" {
				return fmt.Errorf("case %q: Unsupported[%s] has no reason", c.ID, b)
			}
			if _, also := c.Diverges[b]; also {
				return fmt.Errorf("case %q: %s is marked both Unsupported and Diverges", c.ID, b)
			}
		}
		for b := range c.Diverges {
			reason, hasReason := c.DivergesReason[b]
			if !hasReason || reason == "" {
				return fmt.Errorf("case %q: Diverges[%s] has no DivergesReason", c.ID, b)
			}
		}
	}
	return nil
}
