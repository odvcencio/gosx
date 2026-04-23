package editor

import "strings"

// parseFrontMatter extracts a YAML-ish front-matter block delimited by `---`
// lines at the very start of content. Returns (meta, body, true) when a
// well-formed block is found, otherwise (nil, content, false).
//
// Supported syntax: `key: value` per line, `"double"` and `'single'` quoted
// values, inline lists `[a, b, c]` flattened to `a, b, c`. Blank lines and
// `# comment` lines inside the block are ignored. A line containing only
// `---` or `...` closes the block.
func parseFrontMatter(content string) (map[string]string, string, bool) {
	var opening int
	switch {
	case strings.HasPrefix(content, "---\n"):
		opening = len("---\n")
	case strings.HasPrefix(content, "---\r\n"):
		opening = len("---\r\n")
	default:
		return nil, content, false
	}

	rest := content[opening:]
	lines := strings.Split(rest, "\n")

	endIdx := -1
	for i, ln := range lines {
		trim := strings.TrimRight(ln, "\r")
		if trim == "---" || trim == "..." {
			endIdx = i
			break
		}
	}
	if endIdx == -1 {
		return nil, content, false
	}

	meta := make(map[string]string, endIdx)
	for _, ln := range lines[:endIdx] {
		ln = strings.TrimRight(ln, "\r")
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.IndexByte(ln, ':')
		if colon <= 0 {
			continue
		}
		key := strings.TrimSpace(ln[:colon])
		value := strings.TrimSpace(ln[colon+1:])
		value = unquoteFrontMatterValue(value)
		value = flattenInlineList(value)
		if key != "" {
			meta[key] = value
		}
	}

	body := strings.Join(lines[endIdx+1:], "\n")
	return meta, body, true
}

func unquoteFrontMatterValue(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

func flattenInlineList(s string) string {
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return s
	}
	inner := s[1 : len(s)-1]
	parts := strings.Split(inner, ",")
	for i, p := range parts {
		parts[i] = unquoteFrontMatterValue(strings.TrimSpace(p))
	}
	return strings.Join(parts, ", ")
}

// applyFrontMatter parses a front-matter block from o.Content and, if one is
// present, copies recognized keys into the corresponding Options fields
// (only when those fields are currently empty — explicit Options values win).
// Unknown keys are copied into ExtraFields under the same rule. The parsed
// block is stripped from o.Content.
func (o *Options) applyFrontMatter() {
	if o.Content == "" {
		return
	}
	meta, body, ok := parseFrontMatter(o.Content)
	if !ok {
		return
	}
	o.Content = body

	setIfEmpty := func(dst *string, key string) {
		if *dst != "" {
			return
		}
		if v, present := meta[key]; present {
			*dst = v
		}
	}

	setIfEmpty(&o.Title, "title")
	setIfEmpty(&o.Slug, "slug")
	setIfEmpty(&o.Excerpt, "excerpt")
	setIfEmpty(&o.Tags, "tags")
	setIfEmpty(&o.CoverImage, "cover_image")
	setIfEmpty(&o.PublishAt, "publish_at")
	setIfEmpty(&o.Mood, "mood")
	setIfEmpty(&o.Music, "music")

	if o.Status == "" {
		if v, present := meta["status"]; present {
			o.Status = Status(v)
		}
	}

	known := map[string]struct{}{
		"title": {}, "slug": {}, "excerpt": {}, "tags": {},
		"cover_image": {}, "publish_at": {}, "status": {},
		"mood": {}, "music": {},
	}
	if o.ExtraFields == nil {
		o.ExtraFields = map[string]string{}
	}
	for k, v := range meta {
		if _, isKnown := known[k]; isKnown {
			continue
		}
		if _, exists := o.ExtraFields[k]; exists {
			continue
		}
		o.ExtraFields[k] = v
	}
}
