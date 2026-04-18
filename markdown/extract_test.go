package markdown

import (
	"strings"
	"testing"
	"time"
)

func TestWordCount(t *testing.T) {
	doc := Parse([]byte("Hello world. This is a test."))
	got := doc.WordCount()
	if got != 6 {
		t.Errorf("WordCount() = %d, want 6", got)
	}
}

func TestWordCountIgnoresCode(t *testing.T) {
	src := "Hello world\n\n```go\nfunc main() {}\n```\n\nGoodbye friend\n"
	doc := Parse([]byte(src))
	got := doc.WordCount()
	// "Hello world" = 2 words, "Goodbye friend" = 2 words = 4 total
	// Code block content must be excluded.
	if got != 4 {
		t.Errorf("WordCount() = %d, want 4 (should ignore code block)", got)
	}
}

func TestWordCountIgnoresDiagrams(t *testing.T) {
	src := "Hello world\n\n```mermaid\nflowchart TD\n  Several Words --> More Words\n```\n\nGoodbye friend\n"
	doc := Parse([]byte(src))
	got := doc.WordCount()
	if got != 4 {
		t.Errorf("WordCount() = %d, want 4 (should ignore diagram block)", got)
	}
}

func TestReadingTime(t *testing.T) {
	// Build a document with ~200 words across multiple paragraphs.
	var paragraphs []string
	for i := 0; i < 10; i++ {
		words := make([]string, 20)
		for j := range words {
			words[j] = "word"
		}
		paragraphs = append(paragraphs, strings.Join(words, " "))
	}
	src := strings.Join(paragraphs, "\n\n") + "\n"
	doc := Parse([]byte(src))
	got := doc.ReadingTime()
	if got != 1*time.Minute {
		t.Errorf("ReadingTime() = %v, want 1m0s", got)
	}
}

func TestReadingTimeMinimum(t *testing.T) {
	doc := Parse([]byte("Short."))
	got := doc.ReadingTime()
	if got != 1*time.Minute {
		t.Errorf("ReadingTime() = %v, want 1m0s (minimum)", got)
	}
}

func TestReadingTimeZero(t *testing.T) {
	doc := Parse([]byte(""))
	got := doc.ReadingTime()
	if got != 0 {
		t.Errorf("ReadingTime() = %v, want 0 for empty document", got)
	}
}

func TestHeadings(t *testing.T) {
	src := "# One\n## Two\n### Three\n"
	doc := Parse([]byte(src))
	headings := doc.Headings()
	if len(headings) != 3 {
		t.Fatalf("Headings() returned %d headings, want 3", len(headings))
	}

	tests := []struct {
		level int
		text  string
		id    string
	}{
		{1, "One", "one"},
		{2, "Two", "two"},
		{3, "Three", "three"},
	}

	for i, tt := range tests {
		h := headings[i]
		if h.Level != tt.level {
			t.Errorf("heading[%d].Level = %d, want %d", i, h.Level, tt.level)
		}
		if h.Text != tt.text {
			t.Errorf("heading[%d].Text = %q, want %q", i, h.Text, tt.text)
		}
		if h.ID != tt.id {
			t.Errorf("heading[%d].ID = %q, want %q", i, h.ID, tt.id)
		}
	}
}

func TestTableOfContents(t *testing.T) {
	src := "# Introduction\n## Background\n### Details\n"
	doc := Parse([]byte(src))
	toc := doc.TableOfContents()
	if len(toc) != 3 {
		t.Fatalf("TableOfContents() returned %d entries, want 3", len(toc))
	}

	if toc[0].Level != 1 || toc[0].Text != "Introduction" || toc[0].ID != "introduction" {
		t.Errorf("toc[0] = %+v, want Level=1 Text=Introduction ID=introduction", toc[0])
	}
	if toc[1].Level != 2 || toc[1].Text != "Background" || toc[1].ID != "background" {
		t.Errorf("toc[1] = %+v, want Level=2 Text=Background ID=background", toc[1])
	}
	if toc[2].Level != 3 || toc[2].Text != "Details" || toc[2].ID != "details" {
		t.Errorf("toc[2] = %+v, want Level=3 Text=Details ID=details", toc[2])
	}
}

func TestFrontmatter(t *testing.T) {
	src := "---\ntitle: Hello\ntags:\n  - go\n  - markdown\n---\n\n# Content\n"
	doc := Parse([]byte(src))
	fm := doc.Frontmatter()
	if fm == nil {
		t.Fatal("Frontmatter() returned nil, want map with title")
	}
	if fm["title"] != "Hello" {
		t.Errorf("Frontmatter()[\"title\"] = %v, want \"Hello\"", fm["title"])
	}
	tags, ok := fm["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Errorf("Frontmatter()[\"tags\"] = %v, want [go markdown]", fm["tags"])
	}
}

func TestFrontmatterMissing(t *testing.T) {
	doc := Parse([]byte("# No frontmatter here\n"))
	fm := doc.Frontmatter()
	if fm != nil {
		t.Errorf("Frontmatter() = %v, want nil for document without frontmatter", fm)
	}
}
