package gosx

import "strings"

// If returns child when cond is true, otherwise an empty text node.
func If(cond bool, child Node) Node {
	if cond {
		return child
	}
	return Text("")
}

// IfElse returns ifTrue when cond is true, otherwise ifFalse.
func IfElse(cond bool, ifTrue, ifFalse Node) Node {
	if cond {
		return ifTrue
	}
	return ifFalse
}

// Map applies fn to each item and returns a fragment of the results.
func Map[T any](items []T, fn func(T, int) Node) Node {
	nodes := make([]Node, len(items))
	for i, item := range items {
		nodes[i] = fn(item, i)
	}
	return Fragment(nodes...)
}

// Show renders content when cond is true, with an optional fallback.
func Show(cond bool, content Node, fallback ...Node) Node {
	if cond {
		return content
	}
	if len(fallback) > 0 {
		return fallback[0]
	}
	return Text("")
}

// Classes joins non-empty class names with spaces.
func Classes(names ...string) string {
	var result []string
	for _, n := range names {
		if n != "" {
			result = append(result, n)
		}
	}
	return strings.Join(result, " ")
}

// Style builds an inline style string from key-value pairs.
func Style(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(pairs[i])
		b.WriteString(": ")
		b.WriteString(pairs[i+1])
	}
	return b.String()
}
