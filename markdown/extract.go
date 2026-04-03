package markdown

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Heading represents a heading found in the document.
type Heading struct {
	Level int
	Text  string
	ID    string
}

// WordCount returns the number of words in the document's prose content,
// excluding code blocks.
func (d *Document) WordCount() int {
	if d.Root == nil {
		return 0
	}
	return countWords(d.Root)
}

// countWords recursively counts words in text nodes, skipping code blocks.
func countWords(n *Node) int {
	if n == nil {
		return 0
	}
	// Skip code blocks entirely.
	if n.Type == NodeCodeBlock {
		return 0
	}
	// Count words in text node literals.
	if n.Type == NodeText {
		return len(strings.Fields(n.Literal))
	}
	count := 0
	for _, child := range n.Children {
		count += countWords(child)
	}
	return count
}

// ReadingTime estimates how long it takes to read the document at 200 words
// per minute. Returns a minimum of 1 minute if the document has any words.
func (d *Document) ReadingTime() time.Duration {
	wc := d.WordCount()
	if wc == 0 {
		return 0
	}
	minutes := wc / 200
	if minutes < 1 {
		minutes = 1
	}
	return time.Duration(minutes) * time.Minute
}

// Headings returns all headings in document order with their level, text,
// and a slugified ID.
func (d *Document) Headings() []Heading {
	if d.Root == nil {
		return nil
	}
	var headings []Heading
	collectHeadings(d.Root, &headings)
	return headings
}

// collectHeadings walks the AST and appends headings to the slice.
func collectHeadings(n *Node, out *[]Heading) {
	if n == nil {
		return
	}
	if n.Type == NodeHeading {
		level := 1
		if s, ok := n.Attrs["level"]; ok {
			if v, err := strconv.Atoi(s); err == nil {
				level = v
			}
		}
		text := collectNodeText(n)
		*out = append(*out, Heading{
			Level: level,
			Text:  text,
			ID:    slugify(text),
		})
	}
	for _, child := range n.Children {
		collectHeadings(child, out)
	}
}

// TableOfContents returns a table of contents derived from all headings
// in the document.
func (d *Document) TableOfContents() []TOCEntry {
	headings := d.Headings()
	if len(headings) == 0 {
		return nil
	}
	entries := make([]TOCEntry, len(headings))
	for i, h := range headings {
		entries[i] = TOCEntry{
			Level: h.Level,
			ID:    h.ID,
			Text:  h.Text,
		}
	}
	return entries
}

// extractFrontmatter parses YAML frontmatter from the document source.
// Frontmatter must be delimited by --- on its own line at the start
// of the document.
func (d *Document) extractFrontmatter() {
	if d.Source == nil {
		return
	}
	src := d.Source
	if !bytes.HasPrefix(src, []byte("---\n")) {
		return
	}
	// Find the closing ---
	rest := src[4:]
	idx := bytes.Index(rest, []byte("\n---\n"))
	if idx < 0 {
		// Also handle --- at EOF without trailing newline
		if bytes.HasSuffix(rest, []byte("\n---")) {
			idx = len(rest) - 4
		} else {
			return
		}
	}
	yamlBlock := rest[:idx]
	var data map[string]any
	if err := yaml.Unmarshal(yamlBlock, &data); err != nil {
		return
	}
	d.frontmatterData = data
}
