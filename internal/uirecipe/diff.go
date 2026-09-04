package uirecipe

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrDifferences lets the CLI return diff's conventional non-zero status while
// still printing every deterministic comparison row first.
var ErrDifferences = errors.New("recipe differs from the installed source")

// DiffEntry is one comparison against the embedded catalog.
type DiffEntry struct {
	Path   string
	Status string
	Patch  string
}

// DiffResult reports whether a recipe dependency closure matches the catalog.
type DiffResult struct {
	Recipe  string
	Version string
	Clean   bool
	Entries []DiffEntry
}

// Diff compares application-owned files without changing them.
func (c *Catalog) Diff(root, name string) (DiffResult, error) {
	app, err := openAppRoot(root)
	if err != nil {
		return DiffResult{}, err
	}
	defer app.Close()
	closure, err := c.closure(name)
	if err != nil {
		return DiffResult{}, err
	}
	selected := c.recipes[name]
	result := DiffResult{Recipe: name, Version: selected.Version, Clean: true}
	for _, item := range closure {
		for _, file := range item.files {
			current, readErr := readRootFile(app, file.Target, maxSourceBytes)
			switch {
			case readErr == nil && contentHash(current) == file.SHA256:
				result.Entries = append(result.Entries, DiffEntry{Path: file.Target, Status: "unchanged"})
			case readErr == nil:
				result.Clean = false
				result.Entries = append(result.Entries, DiffEntry{
					Path:   file.Target,
					Status: "modified",
					Patch:  unifiedDiff(current, file.Content, file.Target),
				})
			case errors.Is(readErr, os.ErrNotExist):
				result.Clean = false
				result.Entries = append(result.Entries, DiffEntry{
					Path:   file.Target,
					Status: "missing",
					Patch:  unifiedDiff(nil, file.Content, file.Target),
				})
			default:
				return DiffResult{}, fmt.Errorf("read %s: %w", file.Target, readErr)
			}
		}
	}
	sort.Slice(result.Entries, func(i, j int) bool { return result.Entries[i].Path < result.Entries[j].Path })
	return result, nil
}

type diffLine struct {
	prefix byte
	text   string
}

func unifiedDiff(current, desired []byte, file string) string {
	// Keep terminal output and the quadratic LCS table bounded independently
	// of file size. Binary inputs are described, never emitted to a terminal.
	if !utf8.Valid(current) || bytes.IndexByte(current, 0) >= 0 || !utf8.Valid(desired) || bytes.IndexByte(desired, 0) >= 0 {
		return diffSummary(current, desired, file, "binary content omitted")
	}
	if len(current) > 64<<10 || len(desired) > 64<<10 || bytes.Count(current, []byte{'\n'}) > 512 || bytes.Count(desired, []byte{'\n'}) > 512 {
		return diffSummary(current, desired, file, "large text omitted")
	}
	left := splitDiffLines(current)
	right := splitDiffLines(desired)
	rows := make([][]int, len(left)+1)
	for i := range rows {
		rows[i] = make([]int, len(right)+1)
	}
	for i := len(left) - 1; i >= 0; i-- {
		for j := len(right) - 1; j >= 0; j-- {
			if left[i] == right[j] {
				rows[i][j] = rows[i+1][j+1] + 1
			} else if rows[i+1][j] >= rows[i][j+1] {
				rows[i][j] = rows[i+1][j]
			} else {
				rows[i][j] = rows[i][j+1]
			}
		}
	}
	var lines []diffLine
	for i, j := 0, 0; i < len(left) || j < len(right); {
		switch {
		case i < len(left) && j < len(right) && left[i] == right[j]:
			lines = append(lines, diffLine{prefix: ' ', text: left[i]})
			i++
			j++
		case j >= len(right) || (i < len(left) && rows[i+1][j] >= rows[i][j+1]):
			lines = append(lines, diffLine{prefix: '-', text: left[i]})
			i++
		default:
			lines = append(lines, diffLine{prefix: '+', text: right[j]})
			j++
		}
	}
	from := file
	if len(current) == 0 {
		from = "/dev/null"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s\n+++ catalog/%s\n", from, file)
	fmt.Fprintf(&b, "@@ -1,%d +1,%d @@\n", len(left), len(right))
	for _, line := range lines {
		b.WriteByte(line.prefix)
		b.WriteString(terminalText(line.text))
		b.WriteByte('\n')
	}
	if b.Len() > 128<<10 {
		return diffSummary(current, desired, file, "escaped text exceeds output limit")
	}
	return b.String()
}

func diffSummary(current, desired []byte, file, reason string) string {
	return fmt.Sprintf("--- %s\n+++ catalog/%s\n%s; local %d bytes sha256:%s; catalog %d bytes sha256:%s\n", terminalText(file), terminalText(file), reason, len(current), contentHash(current), len(desired), contentHash(desired))
}

func terminalText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func splitDiffLines(content []byte) []string {
	if len(content) == 0 {
		return nil
	}
	text := strings.TrimSuffix(string(content), "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}
