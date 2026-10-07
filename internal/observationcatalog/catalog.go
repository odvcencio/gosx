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

// Collector retains the lexically first limit rows, independent of traversal
// order. Method sets are also bounded; either truncation reports overflow.
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
	i := sort.Search(len(c.rows), func(i int) bool {
		return c.rows[i].Kind > row.Kind || c.rows[i].Kind == row.Kind && c.rows[i].Pattern >= row.Pattern
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

// Result transfers the collector's private slices to the caller.
func (c *Collector) Result() ([]Pattern, bool) { return c.rows, c.overflow }

func Clone(rows []Pattern) []Pattern {
	result := append([]Pattern(nil), rows...)
	for i := range result {
		result[i].Methods = append([]string(nil), result[i].Methods...)
	}
	return result
}
