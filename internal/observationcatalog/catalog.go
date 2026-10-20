// Package observationcatalog selects a bounded catalog from registered routes.
package observationcatalog

import (
	"sort"
	"strings"
)

// Pattern describes registered methods for one kind and owner-relative path.
type Pattern struct {
	Kind, Pattern string
	Methods       []string
}

const DefaultLimit = 512
const MaxMethods = 64

// Collector retains at most limit registered routes, ordered by pattern then
// kind priority, independent of traversal order. Derived page-error rows do
// not consume route capacity. Method sets are bounded too; either truncation
// reports overflow. Result contains at most twice the route limit.
type Collector struct {
	limit    int
	rows     []Pattern
	overflow bool
}

func New(limit int) *Collector {
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Collector{limit: limit}
}

// Register uses the existing ServeMux method-prefix convention, without
// matching a request or changing the registered path syntax.
func (c *Collector) Register(kind, pattern string) {
	pattern = strings.TrimSpace(pattern)
	method := "*"
	if i := strings.IndexAny(pattern, " \t"); i >= 0 {
		method, pattern = pattern[:i], strings.TrimSpace(pattern[i+1:])
	}
	c.Add(Pattern{Kind: kind, Pattern: pattern, Methods: []string{method}})
	if method == "GET" {
		c.Add(Pattern{Kind: kind, Pattern: pattern, Methods: []string{"HEAD"}})
	}
}

func (c *Collector) Add(row Pattern) {
	// Error rows inherit admitted pages and methods at Result time. Provider
	// error rows cannot displace the pages that generate the actual traffic.
	if row.Kind == "error" {
		return
	}
	i := sort.Search(len(c.rows), func(i int) bool {
		return compareRoute(c.rows[i], row) >= 0
	})
	if i == len(c.rows) || c.rows[i].Kind != row.Kind || c.rows[i].Pattern != row.Pattern {
		if len(c.rows) == c.limit {
			c.overflow = true
			if i == len(c.rows) {
				return
			}
			c.rows = c.rows[:len(c.rows)-1]
		}
		c.rows = append(c.rows, Pattern{})
		copy(c.rows[i+1:], c.rows[i:])
		c.rows[i] = Pattern{Kind: row.Kind, Pattern: row.Pattern}
	}
	for _, method := range row.Methods {
		methods := c.rows[i].Methods
		j := sort.SearchStrings(methods, method)
		if j < len(methods) && methods[j] == method {
			continue
		}
		if len(methods) == MaxMethods {
			c.overflow = true
			if j == len(methods) {
				continue
			}
			methods = methods[:len(methods)-1]
		}
		methods = append(methods, "")
		copy(methods[j+1:], methods[j:])
		methods[j] = method
		c.rows[i].Methods = methods
	}
}

func (c *Collector) Overflow() { c.overflow = true }

// Result returns a private catalog, sorted by kind and pattern. Each admitted
// page contributes an error row with the same methods without another slot.
func (c *Collector) Result() ([]Pattern, bool) {
	rows := make([]Pattern, 0, 2*len(c.rows))
	for _, row := range c.rows {
		rows = append(rows, row)
		if row.Kind == "page" {
			rows = append(rows, Pattern{Kind: "error", Pattern: row.Pattern, Methods: append([]string(nil), row.Methods...)})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Kind < rows[j].Kind || rows[i].Kind == rows[j].Kind && rows[i].Pattern < rows[j].Pattern
	})
	return rows, c.overflow
}

func compareRoute(a, b Pattern) int {
	if n := strings.Compare(a.Pattern, b.Pattern); n != 0 {
		return n
	}
	if n := kindPriority(a.Kind) - kindPriority(b.Kind); n != 0 {
		return n
	}
	return strings.Compare(a.Kind, b.Kind)
}

func kindPriority(kind string) int {
	switch kind {
	case "page":
		return 0
	case "api":
		return 1
	case "action":
		return 2
	case "redirect":
		return 3
	case "rewrite":
		return 4
	case "mount":
		return 5
	default:
		return 6
	}
}

func Clone(rows []Pattern) []Pattern {
	result := append([]Pattern(nil), rows...)
	for i := range result {
		result[i].Methods = append([]string(nil), result[i].Methods...)
	}
	return result
}
