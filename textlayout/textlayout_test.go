package textlayout

import (
	"slices"
	"testing"

	"github.com/rivo/uniseg"
)

func TestPrepareNormalCollapsesWhitespace(t *testing.T) {
	prepared := Prepare("hello\t \nworld", PrepareOptions{WhiteSpace: WhiteSpaceNormal})

	if prepared.WhiteSpace != WhiteSpaceNormal {
		t.Fatalf("whiteSpace: expected normal, got %q", prepared.WhiteSpace)
	}

	got := tokenTexts(prepared.Tokens)
	want := []string{"hello", " ", "world"}
	if !slices.Equal(got, want) {
		t.Fatalf("token texts: expected %v, got %v", want, got)
	}
	if prepared.ByteLen != len(prepared.Source) {
		t.Fatalf("byteLen: expected %d, got %d", len(prepared.Source), prepared.ByteLen)
	}
	if prepared.RuneCount != 13 {
		t.Fatalf("runeCount: expected 13, got %d", prepared.RuneCount)
	}
	space := prepared.Tokens[1]
	if space.ByteStart != 5 || space.ByteEnd != 8 {
		t.Fatalf("space byte span: expected [5,8), got [%d,%d)", space.ByteStart, space.ByteEnd)
	}
	if space.RuneStart != 5 || space.RuneEnd != 8 {
		t.Fatalf("space rune span: expected [5,8), got [%d,%d)", space.RuneStart, space.RuneEnd)
	}
}

func TestPreparePreWrapPreservesBreaksAndTabs(t *testing.T) {
	prepared := Prepare("hi\tthere\nfriend", PrepareOptions{
		WhiteSpace: WhiteSpacePreWrap,
		TabSize:    2,
	})

	kinds := tokenKinds(prepared.Tokens)
	wantKinds := []TokenKind{TokenWord, TokenTab, TokenWord, TokenNewline, TokenWord}
	if !slices.Equal(kinds, wantKinds) {
		t.Fatalf("token kinds: expected %v, got %v", wantKinds, kinds)
	}

	got := tokenTexts(prepared.Tokens)
	want := []string{"hi", "\t", "there", "\n", "friend"}
	if !slices.Equal(got, want) {
		t.Fatalf("token texts: expected %v, got %v", want, got)
	}
	if prepared.TabSize != 2 {
		t.Fatalf("tabSize: expected 2, got %d", prepared.TabSize)
	}
}

func TestLayoutNormalWrapsAtSpaces(t *testing.T) {
	result, err := LayoutText(
		"hello world from gosx",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 11, LineHeight: 2},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"hello world", "from gosx"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
	if result.LineCount != 2 {
		t.Fatalf("lineCount: expected 2, got %d", result.LineCount)
	}
	if result.Height != 4 {
		t.Fatalf("height: expected 4, got %v", result.Height)
	}
	if result.MaxLineWidth != 11 {
		t.Fatalf("maxLineWidth: expected 11, got %v", result.MaxLineWidth)
	}
}

func TestLayoutPreWrapPreservesHardBreaks(t *testing.T) {
	result, err := LayoutText(
		"hero\nquote",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpacePreWrap},
		LayoutOptions{MaxWidth: 99, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"hero", "quote"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
	if !result.Lines[0].HardBreak {
		t.Fatal("expected first line to be a hard break")
	}
	if result.Lines[0].RuneStart != 0 || result.Lines[0].RuneEnd != 4 {
		t.Fatalf("line 0 rune span: got [%d,%d)", result.Lines[0].RuneStart, result.Lines[0].RuneEnd)
	}
	if result.Lines[1].RuneStart != 5 || result.Lines[1].RuneEnd != 10 {
		t.Fatalf("line 1 rune span: got [%d,%d)", result.Lines[1].RuneStart, result.Lines[1].RuneEnd)
	}
}

func TestLayoutPreIgnoresWrapWidth(t *testing.T) {
	result, err := LayoutText(
		"supercalifragilistic",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpacePre},
		LayoutOptions{MaxWidth: 4, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	if result.LineCount != 1 {
		t.Fatalf("lineCount: expected 1, got %d", result.LineCount)
	}
	if result.Lines[0].Text != "supercalifragilistic" {
		t.Fatalf("line text: got %q", result.Lines[0].Text)
	}
}

func TestWalkLinesMatchesLayout(t *testing.T) {
	prepared := Prepare("one two three", PrepareOptions{WhiteSpace: WhiteSpaceNormal})
	measured, err := Measure(prepared, MonospaceMeasurer{Advance: 1}, "mono")
	if err != nil {
		t.Fatalf("measure: %v", err)
	}

	var walked []string
	WalkLines(measured, LayoutOptions{MaxWidth: 7}, func(line Line) bool {
		walked = append(walked, line.Text)
		return true
	})

	want := []string{"one two", "three"}
	if !slices.Equal(walked, want) {
		t.Fatalf("walked lines: expected %v, got %v", want, walked)
	}
}

func TestWalkLineRangesAndMetricsMatchLayout(t *testing.T) {
	prepared := Prepare("hello,world from gosx", PrepareOptions{WhiteSpace: WhiteSpaceNormal})
	measured, err := Measure(prepared, MonospaceMeasurer{Advance: 1}, "mono")
	if err != nil {
		t.Fatalf("measure: %v", err)
	}

	result := Layout(measured, LayoutOptions{MaxWidth: 11, LineHeight: 2})
	metrics := LayoutMetrics(measured, LayoutOptions{MaxWidth: 11, LineHeight: 2})
	if metrics.LineCount != result.LineCount || metrics.Height != result.Height || metrics.MaxLineWidth != result.MaxLineWidth {
		t.Fatalf("metrics mismatch: got %+v want count=%d height=%v width=%v", metrics, result.LineCount, result.Height, result.MaxLineWidth)
	}

	var ranges []LineRange
	WalkLineRanges(measured, LayoutOptions{MaxWidth: 11, LineHeight: 2}, func(line LineRange) bool {
		ranges = append(ranges, line)
		return true
	})
	if len(ranges) != len(result.Lines) {
		t.Fatalf("range count: got %d want %d", len(ranges), len(result.Lines))
	}
	for i := range ranges {
		if ranges[i].Width != result.Lines[i].Width || ranges[i].ByteStart != result.Lines[i].ByteStart || ranges[i].ByteEnd != result.Lines[i].ByteEnd {
			t.Fatalf("range %d mismatch: got %+v want %+v", i, ranges[i], result.Lines[i])
		}
	}

	rangeResult, err := LayoutTextRanges("hello,world from gosx", MonospaceMeasurer{Advance: 1}, "mono", PrepareOptions{WhiteSpace: WhiteSpaceNormal}, LayoutOptions{MaxWidth: 11, LineHeight: 2})
	if err != nil {
		t.Fatalf("layout text ranges: %v", err)
	}
	if len(rangeResult.Lines) != len(result.Lines) || rangeResult.LineCount != result.LineCount {
		t.Fatalf("range result mismatch: got %+v want %+v", rangeResult, result)
	}
}

func TestLayoutConsecutiveNewlinesKeepsEmptyLinePosition(t *testing.T) {
	result, err := LayoutText(
		"a\n\nb",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpacePreWrap},
		LayoutOptions{MaxWidth: 99, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"a", "", "b"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}

	empty := result.Lines[1]
	if !empty.HardBreak {
		t.Fatal("expected empty line to be a hard break")
	}
	if empty.ByteStart != 2 || empty.ByteEnd != 2 {
		t.Fatalf("empty line byte span: expected [2,2), got [%d,%d)", empty.ByteStart, empty.ByteEnd)
	}
	if empty.RuneStart != 2 || empty.RuneEnd != 2 {
		t.Fatalf("empty line rune span: expected [2,2), got [%d,%d)", empty.RuneStart, empty.RuneEnd)
	}
}

func TestLayoutTrailingNewlineAppendsTerminalEmptyLine(t *testing.T) {
	result, err := LayoutText(
		"hi\n",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpacePreWrap},
		LayoutOptions{MaxWidth: 99, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"hi", ""}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}

	last := result.Lines[1]
	if last.ByteStart != 3 || last.ByteEnd != 3 {
		t.Fatalf("terminal empty line byte span: expected [3,3), got [%d,%d)", last.ByteStart, last.ByteEnd)
	}
	if last.RuneStart != 3 || last.RuneEnd != 3 {
		t.Fatalf("terminal empty line rune span: expected [3,3), got [%d,%d)", last.RuneStart, last.RuneEnd)
	}
}

func TestLayoutCollapsedWhitespaceRetainsTerminalPosition(t *testing.T) {
	result, err := LayoutText(
		"   ",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 99, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	if result.LineCount != 1 {
		t.Fatalf("lineCount: expected 1, got %d", result.LineCount)
	}
	if result.Lines[0].Text != "" {
		t.Fatalf("expected empty line text, got %q", result.Lines[0].Text)
	}
	if result.Lines[0].ByteStart != 3 || result.Lines[0].ByteEnd != 3 {
		t.Fatalf("collapsed whitespace byte span: expected [3,3), got [%d,%d)", result.Lines[0].ByteStart, result.Lines[0].ByteEnd)
	}
	if result.Lines[0].RuneStart != 3 || result.Lines[0].RuneEnd != 3 {
		t.Fatalf("collapsed whitespace rune span: expected [3,3), got [%d,%d)", result.Lines[0].RuneStart, result.Lines[0].RuneEnd)
	}
}

func TestLayoutBreaksLongWordsAtGraphemeBoundaries(t *testing.T) {
	result, err := LayoutText(
		"abcdef",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 4, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"abcd", "ef"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
	if result.MaxLineWidth != 4 {
		t.Fatalf("maxLineWidth: expected 4, got %v", result.MaxLineWidth)
	}
}

func TestLayoutNormalTrailingSpacesHangWithoutInflatingWidth(t *testing.T) {
	result, err := LayoutText(
		"hello ",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 99, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	if result.MaxLineWidth != 5 {
		t.Fatalf("maxLineWidth: expected 5, got %v", result.MaxLineWidth)
	}
	if result.Lines[0].Text != "hello" {
		t.Fatalf("line text: expected collapsed trailing space to disappear, got %q", result.Lines[0].Text)
	}
}

func TestLayoutClampsToMaxLinesWithEllipsis(t *testing.T) {
	result, err := LayoutText(
		"hello world from gosx",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 11, LineHeight: 2, MaxLines: 1, Overflow: OverflowEllipsis},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	if result.LineCount != 1 {
		t.Fatalf("lineCount: expected 1, got %d", result.LineCount)
	}
	if !result.Truncated {
		t.Fatal("expected result to be truncated")
	}
	if !result.Lines[0].Truncated || !result.Lines[0].Ellipsis {
		t.Fatalf("expected truncated ellipsis line, got %+v", result.Lines[0])
	}
	if got := result.Lines[0].Text; got != "hello worl…" {
		t.Fatalf("expected ellipsis text, got %q", got)
	}
	if result.Lines[0].Width != 11 {
		t.Fatalf("expected clamped width 11, got %v", result.Lines[0].Width)
	}
}

func TestLayoutClampsToMaxLinesWithoutEllipsis(t *testing.T) {
	result, err := LayoutText(
		"hello world from gosx",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 11, LineHeight: 2, MaxLines: 1, Overflow: OverflowClip},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	if result.LineCount != 1 {
		t.Fatalf("lineCount: expected 1, got %d", result.LineCount)
	}
	if !result.Truncated {
		t.Fatal("expected result to be truncated")
	}
	if result.Lines[0].Ellipsis {
		t.Fatalf("did not expect ellipsis line, got %+v", result.Lines[0])
	}
	if got := result.Lines[0].Text; got != "hello world" {
		t.Fatalf("expected clipped text, got %q", got)
	}
}

func TestLayoutPreWrapUsesTabStops(t *testing.T) {
	result, err := LayoutText(
		"a\tb",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpacePreWrap},
		LayoutOptions{MaxWidth: 99, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	if result.LineCount != 1 {
		t.Fatalf("lineCount: expected 1, got %d", result.LineCount)
	}
	if result.MaxLineWidth != 9 {
		t.Fatalf("maxLineWidth: expected 9, got %v", result.MaxLineWidth)
	}
	if result.Lines[0].Text != "a\tb" {
		t.Fatalf("line text: got %q", result.Lines[0].Text)
	}
}

func TestLayoutBreaksAtSoftHyphen(t *testing.T) {
	result, err := LayoutText(
		"ab\u00adcd",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 3, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"ab-", "cd"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
	if result.Lines[0].Width != 3 {
		t.Fatalf("line 0 width: expected 3, got %v", result.Lines[0].Width)
	}
}

func TestLayoutBreaksAtZeroWidthBreak(t *testing.T) {
	result, err := LayoutText(
		"foo\u200bbar",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 3, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"foo", "bar"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
}

func TestLayoutPrefersWordBoundariesInsideRuns(t *testing.T) {
	result, err := LayoutText(
		"hello,world",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 7, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"hello,", "world"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
}

func TestLayoutKeepsCJKClosingPunctuationOffLineStart(t *testing.T) {
	result, err := LayoutText(
		"あ。い",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 1, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"あ。", "い"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
	if result.Lines[0].Width != 2 {
		t.Fatalf("line 0 width: expected 2, got %v", result.Lines[0].Width)
	}
}

func TestLayoutKeepsOpeningPunctuationWithFollowingGlyph(t *testing.T) {
	result, err := LayoutText(
		"(a",
		MonospaceMeasurer{Advance: 1},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 1, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"(a"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
	if result.Lines[0].Width != 2 {
		t.Fatalf("line 0 width: expected 2, got %v", result.Lines[0].Width)
	}
}

func TestLayoutKeepsEmojiGraphemeClustersIntact(t *testing.T) {
	result, err := LayoutText(
		"👨‍👩‍👧‍👦a",
		graphemeMeasurer{},
		"mono",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 1, LineHeight: 1},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}

	lines := lineTexts(result.Lines)
	want := []string{"👨‍👩‍👧‍👦", "a"}
	if !slices.Equal(lines, want) {
		t.Fatalf("line texts: expected %v, got %v", want, lines)
	}
}

func TestCachingMeasurerReusesWidths(t *testing.T) {
	spy := &spyMeasurer{}
	cached := NewCachingMeasurer(spy)

	_, err := cached.MeasureBatch("body", []string{"hello", "world", "hello"})
	if err != nil {
		t.Fatalf("first measure: %v", err)
	}
	_, err = cached.MeasureBatch("body", []string{"world", "hello"})
	if err != nil {
		t.Fatalf("second measure: %v", err)
	}

	if spy.calls != 1 {
		t.Fatalf("expected 1 inner call, got %d", spy.calls)
	}
	if !slices.Equal(spy.lastTexts, []string{"hello", "world"}) {
		t.Fatalf("lastTexts: got %v", spy.lastTexts)
	}
}

func TestApproximateMeasurerProducesStableServerHints(t *testing.T) {
	result, err := LayoutText(
		"hello world from gosx",
		ApproximateMeasurer{},
		"600 16px serif",
		PrepareOptions{WhiteSpace: WhiteSpaceNormal},
		LayoutOptions{MaxWidth: 88, LineHeight: 20},
	)
	if err != nil {
		t.Fatalf("layout text: %v", err)
	}
	if result.LineCount < 2 {
		t.Fatalf("expected wrapped estimate, got %+v", result)
	}
	if result.Height != float64(result.LineCount)*20 {
		t.Fatalf("expected height to match line count, got %+v", result)
	}
}

type spyMeasurer struct {
	calls     int
	lastTexts []string
}

func (m *spyMeasurer) MeasureBatch(_ string, texts []string) ([]float64, error) {
	m.calls++
	m.lastTexts = append([]string(nil), texts...)
	widths := make([]float64, len(texts))
	for i, text := range texts {
		widths[i] = float64(len(text))
	}
	return widths, nil
}

type graphemeMeasurer struct{}

func (graphemeMeasurer) MeasureBatch(_ string, texts []string) ([]float64, error) {
	widths := make([]float64, len(texts))
	for i, text := range texts {
		graphemes := uniseg.NewGraphemes(text)
		for graphemes.Next() {
			widths[i]++
		}
	}
	return widths, nil
}

func tokenTexts(tokens []Token) []string {
	out := make([]string, len(tokens))
	for i, token := range tokens {
		out[i] = token.Text
	}
	return out
}

func tokenKinds(tokens []Token) []TokenKind {
	out := make([]TokenKind, len(tokens))
	for i, token := range tokens {
		out[i] = token.Kind
	}
	return out
}

func lineTexts(lines []Line) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = line.Text
	}
	return out
}
