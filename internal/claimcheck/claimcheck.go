// Package claimcheck verifies the claims that comments make about OTHER files.
//
// A comment that describes code in the same file breaks loudly: the reader sees
// the code. A comment that describes code in a DIFFERENT file breaks silently.
// The other file changes, no compiler and no test looks at the comment, and the
// comment now lies. The worst direction is a comment that says a feature does
// not work when it does, because it tells the next author to delete working
// code.
//
// A claim marker makes one such sentence checkable. Write one line inside the
// ordinary comment, next to the prose it supports:
//
//	// The browser bright pass uses a soft knee, not a hard cut, so a pixel just
//	// over the threshold fades in instead of popping.
//	//	gosx:claim has internal/claimcheck/repo_test.go `func TestRepoClaimsHold`
//
// The marker names a verb, a repository-relative target path, and a regular
// expression in backticks. Three verbs exist:
//
//   - has — the expression must match the target at least once.
//   - lacks — the expression must not match the target at all.
//   - count=N — the expression must match the target exactly N times.
//
// TestRepoClaimsHold walks the tree, parses every marker, and checks it. The
// walk fails when a target file is gone, when a marker does not parse, and when
// a claim is false. Those three states carry different messages, so a broken
// marker never reads as a broken claim.
//
// The scan is line based. It does not prove that a marker sits inside a comment,
// and a marker inside a string literal gets checked like any other. That costs
// nothing and keeps the parser at one regular expression.
package claimcheck

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Marker is the token that starts a claim.
//
// The literal is split in two pieces on purpose. This file would otherwise
// carry the token on the line that declares it, the scanner would read that
// line as a marker, and the package would fail its own check. Every other
// spelling in this package derives from this constant, so the token has exactly
// one definition.
const Marker = "gosx" + ":claim"

// Verb names what a claim asserts about its target file.
type Verb string

// The three verbs a marker may use.
const (
	VerbHas   Verb = "has"
	VerbLacks Verb = "lacks"
	VerbCount Verb = "count"
)

// markerRe parses a well-formed marker. triggerRe finds a line that means to be
// a marker. A line that trips triggerRe but not markerRe is malformed, which is
// reported apart from a false claim.
var (
	triggerRe = regexp.MustCompile(regexp.QuoteMeta(Marker))
	markerRe  = regexp.MustCompile(
		regexp.QuoteMeta(Marker) + `[ \t]+([A-Za-z]+)(=([0-9]+))?[ \t]+(\S+)[ \t]+` + "`([^`\n]*)`")
)

// Claim is one parsed marker.
type Claim struct {
	Source  string // repository-relative path of the file that carries the marker
	Line    int    // 1-based line number of the marker
	Verb    Verb
	Want    int    // match count VerbCount requires; the other verbs ignore it
	Target  string // repository-relative path of the file the claim describes
	Pattern string // regular expression checked against the target
	Text    string // the marker as written, for failure output
}

// String renders a claim the way an author wrote it.
func (c Claim) String() string {
	if c.Verb == VerbCount {
		return fmt.Sprintf("%s count=%d %s `%s`", Marker, c.Want, c.Target, c.Pattern)
	}
	return fmt.Sprintf("%s %s %s `%s`", Marker, c.Verb, c.Target, c.Pattern)
}

// Where names the marker site for failure output.
func (c Claim) Where() string {
	return fmt.Sprintf("%s:%d", c.Source, c.Line)
}

// Malformed is a line that carries the marker token but does not parse.
//
// It is a separate type from a false claim on purpose. A checker that treats a
// typo as a passing claim is the exact defect this package exists to catch.
type Malformed struct {
	Source string
	Line   int
	Text   string
	Reason string
}

// Where names the malformed site for failure output.
func (m Malformed) Where() string {
	return fmt.Sprintf("%s:%d", m.Source, m.Line)
}

// Result is the verdict on one claim.
//
// OK is true only when the target was read and the claim held. Problem carries
// the reason for every other outcome, including a missing target file.
type Result struct {
	Claim   Claim
	Matches int
	OK      bool
	Problem string
}

// scanExts are the file extensions the walk reads.
var scanExts = map[string]bool{
	".go":  true,
	".js":  true,
	".mjs": true,
}

// skipDirs are directory names the walk never enters.
//
// Each name holds generated output, third-party code, or fixtures that carry
// deliberately false markers. A marker inside one of these is never checked, so
// do not put a real claim there.
var skipDirs = map[string]bool{
	".git":         true,
	".canopy":      true,
	"node_modules": true,
	"vendor":       true,
	"testdata":     true,
	"dist":         true,
	"build":        true,
	"tmp":          true,
}

// skipFile reports whether a file is generated output rather than source.
//
// The bootstrap bundles under client/js are compiled from client/js/bootstrap-src
// by cmd/buildbootstrap. Reading both copies would count every marker twice, and
// the minifier can cut a marker in half and turn it into a false report of a
// malformed line.
func skipFile(rel string) bool {
	dir, base := path.Split(rel)
	if dir == "client/js/" && strings.HasPrefix(base, "bootstrap") {
		return true
	}
	return strings.HasSuffix(base, ".min.js")
}

// Scan walks root and returns every claim it parsed and every marker line it
// could not parse.
//
// It returns an error only when the walk itself fails. A malformed marker is
// data, not an error, so a caller can report all of them at once.
func Scan(root string) ([]Claim, []Malformed, error) {
	var claims []Claim
	var malformed []Malformed

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !scanExts[strings.ToLower(filepath.Ext(p))] || skipFile(rel) {
			return nil
		}
		data, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		c, m := ParseFile(rel, string(data))
		claims = append(claims, c...)
		malformed = append(malformed, m...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return claims, malformed, nil
}

// ParseFile returns the claims and the malformed markers in one file body.
func ParseFile(rel, body string) ([]Claim, []Malformed) {
	var claims []Claim
	var malformed []Malformed
	for i, line := range strings.Split(body, "\n") {
		lineNo := i + 1
		if !triggerRe.MatchString(line) {
			continue
		}
		c, reason := parseLine(rel, lineNo, line)
		if reason != "" {
			malformed = append(malformed, Malformed{
				Source: rel,
				Line:   lineNo,
				Text:   strings.TrimSpace(line),
				Reason: reason,
			})
			continue
		}
		claims = append(claims, c)
	}
	return claims, malformed
}

// parseLine parses one marker line. It returns a reason when the line carries
// the token but is not a usable claim.
func parseLine(rel string, lineNo int, line string) (Claim, string) {
	m := markerRe.FindStringSubmatch(line)
	if m == nil {
		return Claim{}, fmt.Sprintf(
			"the line carries %s but does not parse. Write: %s <has|lacks|count=N> <repo/relative/path> `<regexp>`",
			Marker, Marker)
	}
	verb, countText, target, pattern := m[1], m[3], m[4], m[5]

	c := Claim{
		Source:  rel,
		Line:    lineNo,
		Target:  target,
		Pattern: pattern,
		Text:    strings.TrimSpace(line),
	}

	switch Verb(verb) {
	case VerbHas, VerbLacks:
		if countText != "" {
			return Claim{}, fmt.Sprintf("verb %q takes no count, but the marker reads %q", verb, verb+"="+countText)
		}
		c.Verb = Verb(verb)
	case VerbCount:
		if countText == "" {
			return Claim{}, "verb \"count\" needs a number. Write count=N"
		}
		n, err := strconv.Atoi(countText)
		if err != nil {
			return Claim{}, fmt.Sprintf("count %q is not a number: %v", countText, err)
		}
		c.Verb = VerbCount
		c.Want = n
	default:
		return Claim{}, fmt.Sprintf("unknown verb %q. Use has, lacks, or count=N", verb)
	}

	if pattern == "" {
		return Claim{}, "the pattern between the backticks is empty, so the claim asserts nothing"
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return Claim{}, fmt.Sprintf("the pattern does not compile: %v", err)
	}
	if reason := checkTargetPath(target); reason != "" {
		return Claim{}, reason
	}
	if target == rel {
		return Claim{}, "the target is the file that carries the marker. A claim about the same file needs no marker; the reader sees the code"
	}
	return c, ""
}

// checkTargetPath rejects a path shape the walk cannot resolve against the
// repository root.
func checkTargetPath(target string) string {
	switch {
	case strings.HasPrefix(target, "/"):
		return fmt.Sprintf("target %q is absolute. Write the path relative to the repository root", target)
	case strings.HasPrefix(target, "./"), strings.HasPrefix(target, "../"):
		return fmt.Sprintf("target %q is relative to the marker. Write the path relative to the repository root", target)
	case target != path.Clean(target):
		return fmt.Sprintf("target %q is not a clean path. Write %q", target, path.Clean(target))
	}
	return ""
}

// Verify reads the target of one claim and returns the verdict.
//
// A target that cannot be read fails. It never skips. A guard that turns itself
// off when its input moves is worse than no guard, because the tree still shows
// a passing test.
func Verify(root string, c Claim) Result {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(c.Target)))
	if err != nil {
		return Result{Claim: c, Problem: fmt.Sprintf(
			"%s claims about %s, and that file cannot be read: %v\n  claim: %s\n  Fix the path or delete the claim. Do not skip it.",
			c.Where(), c.Target, err, c)}
	}
	re, err := regexp.Compile(c.Pattern)
	if err != nil {
		// parseLine already compiled the pattern, so this cannot happen from a
		// scanned claim. Report it rather than panic for a hand-built one.
		return Result{Claim: c, Problem: fmt.Sprintf("%s: pattern does not compile: %v", c.Where(), err)}
	}
	n := len(re.FindAllStringIndex(string(data), -1))
	res := Result{Claim: c, Matches: n}

	switch c.Verb {
	case VerbHas:
		if n > 0 {
			res.OK = true
			return res
		}
		res.Problem = fmt.Sprintf(
			"%s claims %s contains `%s`, and it does not.\n  claim: %s\n  The comment describes code that moved, was renamed, or was deleted. Correct the comment, then the claim.",
			c.Where(), c.Target, c.Pattern, c)
	case VerbLacks:
		if n == 0 {
			res.OK = true
			return res
		}
		res.Problem = fmt.Sprintf(
			"%s claims %s does NOT contain `%s`, and it does, %d time(s).\n  claim: %s\n  A comment that denies a working feature tells the next author to delete it. Correct the comment, then the claim.",
			c.Where(), c.Target, c.Pattern, n, c)
	case VerbCount:
		if n == c.Want {
			res.OK = true
			return res
		}
		res.Problem = fmt.Sprintf(
			"%s claims %s contains `%s` exactly %d time(s), and it contains it %d time(s).\n  claim: %s\n  Correct the comment, then the count.",
			c.Where(), c.Target, c.Pattern, c.Want, n, c)
	default:
		res.Problem = fmt.Sprintf("%s: unknown verb %q", c.Where(), c.Verb)
	}
	return res
}

// VerifyAll returns one result per claim, in the order given.
//
// The caller compares len(results) with len(claims) to prove that no claim was
// dropped between the scan and the check.
func VerifyAll(root string, claims []Claim) []Result {
	out := make([]Result, 0, len(claims))
	for _, c := range claims {
		out = append(out, Verify(root, c))
	}
	return out
}

// FindRoot walks up from dir and returns the first directory that holds go.mod.
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		abs = parent
	}
}
