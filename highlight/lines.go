package highlight

import (
	"sort"
	"strconv"
	"strings"
)

// HighlightStep is one click step in a code-walkthrough range spec.
//
// It represents either an explicit set of 1-based line numbers (Lines, sorted
// and deduplicated) or every line (All). The two are mutually exclusive: when
// All is true, Lines is nil and consumers should highlight the whole block.
type HighlightStep struct {
	// All highlights every line in the block (the "all" sentinel).
	All bool
	// Lines is the sorted, deduplicated set of 1-based line numbers for this
	// step. It is nil when All is true.
	Lines []int
}

// HTMLLines returns one HTML fragment per source line, each wrapped as
//
//	<span class="ts-line" data-line="N">…inner…</span>
//
// where N is the 1-based line number and inner is that line's
// syntax-highlighted markup. Token coloring is identical to HTML: the whole
// block is rendered once via the shared highlight machinery and then split on
// line boundaries. A token span that straddles a newline (e.g. a multi-line
// raw string) is closed at the end of one line and reopened with the same
// class on the next, so every returned fragment has balanced spans and never
// contains a literal newline.
//
// The returned slice always has len == LineCount(source); empty source yields
// a single empty line wrapper.
func HTMLLines(lang, source string) []string {
	rendered := HTML(lang, source)
	rawLines := splitHighlightedLines(rendered)

	// Reconcile against the source line count. HTML("") == "" collapses to a
	// single empty fragment, which splitHighlightedLines already yields; this
	// guards any renderer that might diverge from LineCount semantics.
	want := LineCount(source)
	for len(rawLines) < want {
		rawLines = append(rawLines, "")
	}
	if len(rawLines) > want {
		rawLines = rawLines[:want]
	}

	out := make([]string, len(rawLines))
	for i, inner := range rawLines {
		var b strings.Builder
		b.WriteString(`<span class="ts-line" data-line="`)
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(`">`)
		b.WriteString(inner)
		b.WriteString(`</span>`)
		out[i] = b.String()
	}
	return out
}

// splitHighlightedLines splits already-rendered highlight HTML on newlines
// while keeping per-line <span> markup balanced. Spans open with `<span ...>`
// and close with `</span>`; all other text (already HTML-escaped, so a bare
// '<' only ever starts a span tag) is copied through verbatim. When a newline
// is reached, every currently-open span is closed (in reverse) to finish the
// line and reopened (in order) to start the next, preserving each span's
// original opening tag — and therefore its class.
func splitHighlightedLines(rendered string) []string {
	var (
		lines []string
		cur   strings.Builder
		open  []string // stack of open <span ...> tags, in order
	)

	flush := func() {
		// Close open spans to balance the line just before emitting it.
		for i := len(open) - 1; i >= 0; i-- {
			cur.WriteString("</span>")
		}
		lines = append(lines, cur.String())
		cur.Reset()
		// Reopen the same spans so the next line continues inside them.
		for _, tag := range open {
			cur.WriteString(tag)
		}
	}

	for i := 0; i < len(rendered); {
		c := rendered[i]
		switch {
		case c == '\n':
			flush()
			i++
		case c == '<' && strings.HasPrefix(rendered[i:], "</span>"):
			cur.WriteString("</span>")
			if n := len(open); n > 0 {
				open = open[:n-1]
			}
			i += len("</span>")
		case c == '<' && strings.HasPrefix(rendered[i:], "<span"):
			end := strings.IndexByte(rendered[i:], '>')
			if end < 0 {
				// Malformed; copy the rest as-is.
				cur.WriteString(rendered[i:])
				i = len(rendered)
				continue
			}
			tag := rendered[i : i+end+1]
			cur.WriteString(tag)
			open = append(open, tag)
			i += end + 1
		default:
			cur.WriteByte(c)
			i++
		}
	}
	// Final line (no trailing newline closes it). Balance any open spans.
	for i := len(open) - 1; i >= 0; i-- {
		cur.WriteString("</span>")
	}
	lines = append(lines, cur.String())
	return lines
}

// parseRangeSpec parses the click-step range DSL stored by mdpp in a code
// block's Attrs["highlights"] (the raw inside of a fence's {…}).
//
// Steps are separated by '|'; each step is one click. Within a step, items are
// comma-separated and each item is one of:
//
//	N      a single 1-based line
//	N-M    an inclusive range (M >= N)
//	all    every line (sets All on that step)
//
// Whitespace around separators and items is ignored. Lines within a step are
// sorted and deduplicated. Empty, whitespace-only, or unparseable items are
// skipped; a step that contributes no lines (and is not "all") is dropped, so
// garbage input yields no steps rather than empty ones.
//
// Example: parseRangeSpec("1-3|5|all") returns
//
//	[]HighlightStep{{Lines: []int{1, 2, 3}}, {Lines: []int{5}}, {All: true}}
func parseRangeSpec(spec string) []HighlightStep {
	var steps []HighlightStep
	for _, group := range strings.Split(spec, "|") {
		step, ok := parseStep(group)
		if ok {
			steps = append(steps, step)
		}
	}
	return steps
}

// parseStep parses a single '|'-delimited group into one HighlightStep. The
// bool is false when the group contributes nothing (empty/garbage), so the
// caller can drop it.
func parseStep(group string) (HighlightStep, bool) {
	seen := map[int]bool{}
	var lines []int
	all := false

	for _, item := range strings.Split(group, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.EqualFold(item, "all") {
			all = true
			continue
		}
		if lo, hi, ok := parseRangeItem(item); ok {
			for n := lo; n <= hi; n++ {
				if !seen[n] {
					seen[n] = true
					lines = append(lines, n)
				}
			}
		}
	}

	if all {
		// "all" subsumes any explicit lines in the same step.
		return HighlightStep{All: true}, true
	}
	if len(lines) == 0 {
		return HighlightStep{}, false
	}
	sort.Ints(lines)
	return HighlightStep{Lines: lines}, true
}

// parseRangeItem parses a single "N" or "N-M" item into an inclusive [lo, hi].
// It returns ok == false for anything malformed, for non-positive lines, or
// for an inverted range (M < N).
func parseRangeItem(item string) (lo, hi int, ok bool) {
	if dash := strings.IndexByte(item, '-'); dash >= 0 {
		a := strings.TrimSpace(item[:dash])
		b := strings.TrimSpace(item[dash+1:])
		lo, err1 := strconv.Atoi(a)
		hi, err2 := strconv.Atoi(b)
		if err1 != nil || err2 != nil || lo < 1 || hi < lo {
			return 0, 0, false
		}
		return lo, hi, true
	}
	n, err := strconv.Atoi(item)
	if err != nil || n < 1 {
		return 0, 0, false
	}
	return n, n, true
}
