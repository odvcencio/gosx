package budget

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Trailers contains only validated acknowledgment fields. Free-form reasons
// stay in the original change description and are never copied into reports.
type Trailers struct{ Entries []Ack }

var budgetTrailerPattern = regexp.MustCompile(`^Perf-Budget: scope=(\S+) metric=(raw|gzip|brotli|totalBytes|frameworkBytes) delta=\+([1-9][0-9]*)B disposition=(temporary|permanent) issue=#([1-9][0-9]*)(?: expires=([0-9]{4}-[0-9]{2}-[0-9]{2}))?; because=(.+)$`)
var timingTrailerPattern = regexp.MustCompile(`^Perf-Timing: cell=(\S+) delta=\+([1-9][0-9]*)ms issue=#([1-9][0-9]*); because=(.+)$`)
var gitTrailerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*:.*$`)

func trailerFailure(line int) error {
	return &InputError{Code: "invalid-trailer", Reference: "trailers", Pointer: "/lines/" + strconv.Itoa(line)}
}

// ParseTrailers recognizes only a final contiguous Git-trailer block after a
// blank line, outside Markdown fences and indented examples. It grants no
// allocation allowance and performs no configuration or filesystem access.
func ParseTrailers(text string) (Trailers, error) {
	out := Trailers{Entries: []Ack{}}
	if len(text) > maxInputBytes || !utf8.ValidString(text) {
		return out, trailerFailure(0)
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if end == 0 {
		return out, nil
	}
	start := end
	for start > 0 && strings.TrimSpace(lines[start-1]) != "" {
		start--
	}
	if start == 0 || fencedAt(lines, start) || indentedCode(lines[start]) {
		return out, nil
	}
	hasPerf := false
	for _, line := range lines[start:end] {
		hasPerf = hasPerf || perfTrailerHeader(line)
	}
	if !hasPerf {
		return out, nil
	}
	// Ordinary trailing prose means this is not a terminal trailer block.
	// A continuation after a perf line is instead an invalid multiline footer.
	for i := start; i < end; i++ {
		if gitTrailerPattern.MatchString(lines[i]) {
			continue
		}
		if perfTrailerHeader(lines[i]) || strings.HasPrefix(lines[i], " ") || strings.HasPrefix(lines[i], "\t") {
			return out, trailerFailure(i)
		}
		return out, nil
	}
	seen := map[string]bool{}
	for i := start; i < end; i++ {
		line := lines[i]
		if !perfTrailerHeader(line) {
			continue
		}
		var ack Ack
		if parts := budgetTrailerPattern.FindStringSubmatch(line); parts != nil {
			delta, e1 := strconv.ParseInt(parts[3], 10, 64)
			issue, e2 := strconv.ParseInt(parts[5], 10, 64)
			if e1 != nil || e2 != nil || !validTrailerScope(parts[1]) || !printableReason(parts[7]) {
				return out, trailerFailure(i)
			}
			ack = Ack{Kind: "budget", Scope: parts[1], Metric: parts[2], Delta: delta, Issue: issue, Disposition: parts[4], ReasonCode: "growth-ack"}
			if (parts[4] == "temporary") != (parts[6] != "") {
				return out, trailerFailure(i)
			}
			if parts[6] != "" {
				if _, err := time.Parse("2006-01-02", parts[6]); err != nil {
					return out, trailerFailure(i)
				}
				expiry := parts[6]
				ack.Expires = &expiry
			}
		} else if parts := timingTrailerPattern.FindStringSubmatch(line); parts != nil {
			cell, ok := timingCell(parts[1])
			delta, e1 := strconv.ParseInt(parts[2], 10, 64)
			issue, e2 := strconv.ParseInt(parts[3], 10, 64)
			if !ok || e1 != nil || e2 != nil || !printableReason(parts[4]) {
				return out, trailerFailure(i)
			}
			ack = Ack{Kind: "timing", Scope: parts[1], Metric: cell.Metric, Delta: delta, Issue: issue, Disposition: "timing", ReasonCode: "growth-ack"}
		} else {
			return out, trailerFailure(i)
		}
		data, _ := json.Marshal(ack)
		var checked Ack
		if decodeInput(data, "Ack", &checked) != nil {
			return out, trailerFailure(i)
		}
		key := ackKey(ack.Kind, ack.Scope, ack.Metric)
		if seen[key] {
			return out, trailerFailure(i)
		}
		seen[key] = true
		out.Entries = append(out.Entries, ack)
	}
	return out, nil
}

func ackKey(kind, scope, metric string) string { return kind + "\x00" + scope + "\x00" + metric }
func perfTrailerHeader(line string) bool {
	key, _, ok := strings.Cut(strings.TrimLeft(line, " \t"), ":")
	return ok && (strings.EqualFold(key, "Perf-Budget") || strings.EqualFold(key, "Perf-Timing"))
}
func indentedCode(line string) bool {
	return strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t")
}
func printableReason(reason string) bool {
	if len(reason) < 12 || len(reason) > 240 || strings.TrimSpace(reason) == "" {
		return false
	}
	for _, r := range reason {
		if !unicode.IsPrint(r) || r == ';' {
			return false
		}
	}
	return true
}
func validTrailerScope(scope string) bool {
	kind, target, ok := strings.Cut(scope, ":")
	if !ok {
		return false
	}
	switch kind {
	case "asset":
		return safePath(target)
	case "type":
		return knownPageType(target)
	case "route":
		app, route, ok := strings.Cut(target, ":")
		return ok && validateInput(app, inputDefinitions["ID"]) == nil && validRoute(route)
	default:
		return false
	}
}
func timingCell(key string) (Cell, bool) {
	parts := strings.Split(key, "|")
	if len(parts) != 6 {
		return Cell{}, false
	}
	cell := Cell{App: parts[0], RouteTemplate: parts[1], PageType: parts[2], Scenario: parts[3], Backend: parts[4], Metric: parts[5], Unit: "ms"}
	return cell, metricUnit(cell.Metric) == "ms" && validateCellSchema(cell) == nil
}

// fencedAt follows matching backtick/tilde fence lengths; indented code does
// not open a fence. Closing fences allow whitespace only after the marker.
func fencedAt(lines []string, end int) bool {
	var marker byte
	length := 0
	for _, line := range lines[:end] {
		if indentedCode(line) {
			continue
		}
		text := strings.TrimLeft(line, " ")
		if len(text) < 3 || text[0] != '`' && text[0] != '~' {
			continue
		}
		n := 0
		for n < len(text) && text[n] == text[0] {
			n++
		}
		if n < 3 {
			continue
		}
		if marker == 0 {
			if text[0] == '`' && strings.ContainsRune(text[n:], '`') {
				continue
			}
			marker, length = text[0], n
		} else if text[0] == marker && n >= length && strings.TrimSpace(text[n:]) == "" {
			marker, length = 0, 0
		}
	}
	return marker != 0
}
