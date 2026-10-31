// Package lazyre compiles a regular expression the first time it is used.
//
// A package-level regexp.MustCompile runs when the program starts. Its call
// keeps regexp and regexp/syntax (the parser, the compiler and the Unicode
// tables they reach) in every binary that imports the package, even when no code
// in the binary ever matches the pattern. The browser runtime hit this: the
// compiler's validators in package ir and the server's text estimator are never
// called there, but their patterns were compiled at start-up, and the linker
// could not drop regexp. A lazily compiled pattern leaves the linker free to
// drop everything that no reachable code matches against.
package lazyre

import (
	"regexp"
	"sync"
)

// Regexp is a regular expression that compiles on first use. Build one with New.
type Regexp struct {
	expr string
	once sync.Once
	re   *regexp.Regexp
}

// New returns a Regexp for expr. It does not compile expr: like
// regexp.MustCompile, a malformed expression panics, but on first use.
func New(expr string) *Regexp { return &Regexp{expr: expr} }

func (r *Regexp) compiled() *regexp.Regexp {
	r.once.Do(func() { r.re = regexp.MustCompile(r.expr) })
	return r.re
}

// MatchString reports whether s contains a match, as regexp.Regexp.MatchString.
func (r *Regexp) MatchString(s string) bool { return r.compiled().MatchString(s) }

// FindStringSubmatch is regexp.Regexp.FindStringSubmatch.
func (r *Regexp) FindStringSubmatch(s string) []string { return r.compiled().FindStringSubmatch(s) }

// SubexpIndex is regexp.Regexp.SubexpIndex.
func (r *Regexp) SubexpIndex(name string) int { return r.compiled().SubexpIndex(name) }
