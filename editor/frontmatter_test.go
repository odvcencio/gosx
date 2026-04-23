package editor

import "testing"

func TestFrontMatter_PopulatesKnownFields(t *testing.T) {
	content := "---\n" +
		"title: Hello World\n" +
		"slug: hello-world\n" +
		"excerpt: A short excerpt.\n" +
		"tags: go, editor\n" +
		"cover_image: /img/cover.png\n" +
		"publish_at: 2026-04-23T10:00:00Z\n" +
		"status: scheduled\n" +
		"mood: quixotic\n" +
		"music: https://youtu.be/abc\n" +
		"---\n" +
		"# Body heading\n\nBody text."

	ed := New("t", Options{Content: content})
	opts := ed.Options

	if opts.Title != "Hello World" {
		t.Fatalf("Title = %q", opts.Title)
	}
	if opts.Slug != "hello-world" {
		t.Fatalf("Slug = %q", opts.Slug)
	}
	if opts.Excerpt != "A short excerpt." {
		t.Fatalf("Excerpt = %q", opts.Excerpt)
	}
	if opts.Tags != "go, editor" {
		t.Fatalf("Tags = %q", opts.Tags)
	}
	if opts.CoverImage != "/img/cover.png" {
		t.Fatalf("CoverImage = %q", opts.CoverImage)
	}
	if opts.PublishAt != "2026-04-23T10:00:00Z" {
		t.Fatalf("PublishAt = %q", opts.PublishAt)
	}
	if opts.Status != StatusScheduled {
		t.Fatalf("Status = %q", opts.Status)
	}
	if opts.Mood != "quixotic" {
		t.Fatalf("Mood = %q", opts.Mood)
	}
	if opts.Music != "https://youtu.be/abc" {
		t.Fatalf("Music = %q", opts.Music)
	}
	if got := ed.Document().Content(); got != "# Body heading\n\nBody text." {
		t.Fatalf("body = %q", got)
	}
}

func TestFrontMatter_DoesNotOverrideExplicitOptions(t *testing.T) {
	content := "---\ntitle: From Matter\nslug: from-matter\n---\nbody"

	ed := New("t", Options{
		Content: content,
		Title:   "Explicit Title",
		Slug:    "explicit-slug",
	})

	if ed.Options.Title != "Explicit Title" {
		t.Fatalf("Title overwritten: %q", ed.Options.Title)
	}
	if ed.Options.Slug != "explicit-slug" {
		t.Fatalf("Slug overwritten: %q", ed.Options.Slug)
	}
}

func TestFrontMatter_UnknownKeysGoToExtraFields(t *testing.T) {
	content := "---\ntitle: T\ncanonical_url: https://example.com/x\nseries: diaries\n---\nhi"

	ed := New("t", Options{Content: content})

	if got := ed.Options.ExtraFields["canonical_url"]; got != "https://example.com/x" {
		t.Fatalf("canonical_url = %q", got)
	}
	if got := ed.Options.ExtraFields["series"]; got != "diaries" {
		t.Fatalf("series = %q", got)
	}
}

func TestFrontMatter_ExtraFieldsDoesNotOverrideExisting(t *testing.T) {
	content := "---\nseries: from-matter\n---\nhi"

	ed := New("t", Options{
		Content:     content,
		ExtraFields: map[string]string{"series": "explicit"},
	})

	if got := ed.Options.ExtraFields["series"]; got != "explicit" {
		t.Fatalf("series overwritten: %q", got)
	}
}

func TestFrontMatter_AbsentLeavesContentAndFieldsAlone(t *testing.T) {
	content := "# Just a heading\n\nNo front matter here."

	ed := New("t", Options{Content: content})

	if ed.Options.Title != "" {
		t.Fatalf("Title = %q, want empty", ed.Options.Title)
	}
	if ed.Document().Content() != content {
		t.Fatalf("content mutated: %q", ed.Document().Content())
	}
}

func TestFrontMatter_UnclosedIsTreatedAsBody(t *testing.T) {
	content := "---\ntitle: Never closes\nbody text without terminator"

	ed := New("t", Options{Content: content})

	if ed.Options.Title != "" {
		t.Fatalf("Title = %q, want empty (malformed front matter)", ed.Options.Title)
	}
	if ed.Document().Content() != content {
		t.Fatalf("content mutated on malformed front matter: %q", ed.Document().Content())
	}
}

func TestFrontMatter_QuotedValuesAreUnquoted(t *testing.T) {
	content := "---\n" +
		"title: \"Quoted: with colon\"\n" +
		"excerpt: 'single quoted'\n" +
		"---\nbody"

	ed := New("t", Options{Content: content})

	if ed.Options.Title != "Quoted: with colon" {
		t.Fatalf("Title = %q", ed.Options.Title)
	}
	if ed.Options.Excerpt != "single quoted" {
		t.Fatalf("Excerpt = %q", ed.Options.Excerpt)
	}
}

func TestFrontMatter_TagsInlineListFlattensToCSV(t *testing.T) {
	content := "---\ntags: [go, editor, markdown]\n---\nbody"

	ed := New("t", Options{Content: content})

	if ed.Options.Tags != "go, editor, markdown" {
		t.Fatalf("Tags = %q", ed.Options.Tags)
	}
}

func TestFrontMatter_IgnoresBlankAndCommentLines(t *testing.T) {
	content := "---\n" +
		"# this is a comment\n" +
		"\n" +
		"title: T\n" +
		"---\nbody"

	ed := New("t", Options{Content: content})

	if ed.Options.Title != "T" {
		t.Fatalf("Title = %q", ed.Options.Title)
	}
}
