package evalparity

import (
	"fmt"
	"strings"
)

// status is one backend's classification for one case, derived from the
// same Unsupported/Diverges/Want fields TestExpressionParity checks
// against — the matrix and the test can never disagree about what a case
// claims, because both read the same Case value.
type status struct {
	kind   string // "agrees", "diverges", or "unsupported"
	detail string // the rendered/pinned value, or the one-line reason
}

func backendStatus(c Case, b Backend) status {
	if reason, ok := c.Unsupported[b]; ok {
		return status{kind: "unsupported", detail: reason}
	}
	if value, ok := c.Diverges[b]; ok {
		return status{kind: "diverges", detail: value}
	}
	return status{kind: "agrees", detail: c.Want}
}

func (s status) cell() string {
	detail := s.detail
	if detail == "" {
		detail = "(empty)"
	}
	switch s.kind {
	case "unsupported":
		return "unsupported"
	case "diverges":
		return "diverges: `" + detail + "`"
	default:
		return "agrees: `" + detail + "`"
	}
}

// GenerateMatrix renders cases into the checked-in support matrix
// document (docs/expression-support-matrix.md). See doc.go for the
// regeneration command and matrix_test.go for the up-to-date check.
func GenerateMatrix(cases []Case) string {
	var b strings.Builder
	b.WriteString("# GoSX expression support matrix\n\n")
	b.WriteString("Generated file. Do not edit by hand. Regenerate it with:\n\n")
	b.WriteString("```\ngo test ./internal/evalparity/... -run TestSupportMatrixUpToDate -update\n```\n\n")
	b.WriteString("This matrix compares GoSX's three expression evaluators:\n\n")
	b.WriteString("- **transpile**: real Go source, compiled by the Go compiler (`m31labs.dev/gosx/transpile`).\n")
	b.WriteString("- **route**: the file router's per-request reflect interpreter (`m31labs.dev/gosx/route`). This is what `router.AddDir` serves.\n")
	b.WriteString("- **client-vm**: the same compiled IR, lowered to island bytecode and walked by the client VM (`m31labs.dev/gosx/client/vm`) — the code that runs in the browser under WASM, run natively here.\n\n")
	b.WriteString("Each row is one case from `internal/evalparity`'s differential test table (`cases_table.go`). ")
	b.WriteString("\"Agrees\" means the backend produces the pinned expected value. \"Diverges\" means the backend runs without error but produces a different, pinned value. ")
	b.WriteString("\"Unsupported\" means the backend cannot run the case at all (a compile or lower error). See each case's notes for why.\n\n")

	b.WriteString(summaryTable(cases))

	order, byCategory := groupByCategory(cases)
	for _, category := range order {
		list := byCategory[category]
		fmt.Fprintf(&b, "## %s\n\n", category)
		b.WriteString("| Case | Expression | transpile | route | client-vm |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, c := range list {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s | %s | %s |\n",
				c.ID, c.Expr,
				backendStatus(c, Transpile).cell(),
				backendStatus(c, Route).cell(),
				backendStatus(c, VM).cell(),
			)
		}
		b.WriteString("\nNotes:\n\n")
		for _, c := range list {
			fmt.Fprintf(&b, "- `%s`: %s\n", c.ID, c.Note)
			for _, bk := range backendOrder {
				st := backendStatus(c, bk)
				if st.kind == "agrees" {
					continue
				}
				fmt.Fprintf(&b, "  - %s %s: %s\n", bk, st.kind, backendReason(c, bk))
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

// backendReason returns the one-line explanation for a non-agreeing
// backend: DivergesReason for a Diverges entry, or the Unsupported
// message itself (which already doubles as its own reason).
func backendReason(c Case, b Backend) string {
	if reason, ok := c.Unsupported[b]; ok {
		return reason
	}
	if reason, ok := c.DivergesReason[b]; ok {
		return reason
	}
	return ""
}

// summaryTable renders the per-backend agree/diverge/unsupported counts,
// the same counts TestExpressionParity logs at the end of a run.
func summaryTable(cases []Case) string {
	var b strings.Builder
	b.WriteString("## Summary\n\n")
	b.WriteString("| Backend | Agrees | Diverges | Unsupported | Total |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, bk := range backendOrder {
		var agree, diverge, unsupported int
		for _, c := range cases {
			switch backendStatus(c, bk).kind {
			case "agrees":
				agree++
			case "diverges":
				diverge++
			case "unsupported":
				unsupported++
			}
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %d | %d |\n", bk, agree, diverge, unsupported, len(cases))
	}
	b.WriteString("\n")
	return b.String()
}

// groupByCategory returns cases grouped by Category, and the category
// names in first-seen order (the order categories appear in
// cases_table.go), so the generated document's section order is stable
// and matches the source table instead of sorting alphabetically.
func groupByCategory(cases []Case) ([]string, map[string][]Case) {
	order := make([]string, 0, 8)
	byCategory := make(map[string][]Case, 8)
	for _, c := range cases {
		if _, seen := byCategory[c.Category]; !seen {
			order = append(order, c.Category)
		}
		byCategory[c.Category] = append(byCategory[c.Category], c)
	}
	return order, byCategory
}
