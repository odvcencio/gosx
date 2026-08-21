package syntax

import "testing"

func TestRenderTextUsesOneLineWisePolicy(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "empty", src: "", want: ""},
		{name: "same line content", src: "hello", want: "hello"},
		{name: "same line separators remain intentional", src: " hello ", want: " hello "},
		{name: "wrapped lines collapse indentation", src: "\n\t  hello\r\n\t  world\r\n", want: "hello world"},
		{name: "blank lines disappear", src: "\n\n  hello  \n\n", want: "hello"},
		{name: "same line edge spaces survive around a hole", src: "hello\n  world ", want: "hello world "},
		{name: "leading hole separator survives", src: " world", want: " world"},
		{name: "trailing hole separator survives", src: "hello ", want: "hello "},
		{name: "crlf and entities", src: "\r\n  Tom &amp; Jerry\r\n", want: "Tom & Jerry"},
		{name: "bare CR line breaks", src: "\r\tTom\r\t&amp;\r", want: "Tom &"},
		{name: "punctuation before expression", src: "(\n", want: "("},
		{name: "punctuation after expression", src: "\n)", want: ")"},
		{name: "whitespace only", src: " \t\r\n\t ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderText(tt.src); got != tt.want {
				t.Fatalf("RenderText(%q) = %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestRenderTextPreservesSemanticWhitespaceOnMultilineLiteral(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{name: "nbsp", src: "\n\t\u00a0\n", want: "\u00a0"},
		{name: "narrow nbsp", src: "\n\t\u202f\n", want: "\u202f"},
		{name: "em space", src: "\n\t\u2003\n", want: "\u2003"},
		{name: "line separator", src: "\n\t\u2028\n", want: "\u2028"},
		{name: "paragraph separator", src: "\n\t\u2029\n", want: "\u2029"},
		{name: "next line", src: "\n\t\u0085\n", want: "\u0085"},
		{name: "form feed", src: "\n\t\f\n", want: "\f"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RenderText(tt.src); got != tt.want {
				t.Fatalf("RenderText(%q) = %q, want exact semantic whitespace %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestRenderTextTreatsOnlyASCIIHorizontalLinesAsLayout(t *testing.T) {
	for _, source := range []string{"", " ", "\t", " \t\t "} {
		if got := RenderText("\n" + source + "\n"); got != "" {
			t.Fatalf("RenderText(%q) = %q, want ASCII layout line discarded", source, got)
		}
	}
}

func TestRenderTextDoesNotDoubleDecodeEntities(t *testing.T) {
	if got, want := RenderText("&amp;amp;"), "&amp;"; got != want {
		t.Fatalf("RenderText double-decoded entity: got %q, want %q", got, want)
	}
}
