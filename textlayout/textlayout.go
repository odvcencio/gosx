package textlayout

import (
	"fmt"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

// WhiteSpace controls how Prepare normalizes input before measurement/layout.
type WhiteSpace string

const (
	WhiteSpaceNormal  WhiteSpace = "normal"
	WhiteSpacePreWrap WhiteSpace = "pre-wrap"
	WhiteSpacePre     WhiteSpace = "pre"
)

// OverflowMode controls how hidden lines are represented when MaxLines clamps output.
type OverflowMode string

const (
	OverflowClip     OverflowMode = "clip"
	OverflowEllipsis OverflowMode = "ellipsis"
)

// TokenKind identifies the layout role of a prepared token.
type TokenKind string

const (
	TokenWord       TokenKind = "word"
	TokenSpace      TokenKind = "space"
	TokenTab        TokenKind = "tab"
	TokenNewline    TokenKind = "newline"
	TokenSoftHyphen TokenKind = "soft-hyphen"
	TokenBreak      TokenKind = "break"
)

// PrepareOptions configures text normalization and tokenization.
type PrepareOptions struct {
	WhiteSpace WhiteSpace
	TabSize    int
}

// LayoutOptions controls line breaking.
type LayoutOptions struct {
	MaxWidth   float64 `json:"maxWidth"`
	LineHeight float64 `json:"lineHeight"`
	MaxLines   int     `json:"maxLines,omitempty"`
	Overflow   OverflowMode `json:"overflow,omitempty"`
}

// Token is a prepared layout token.
type Token struct {
	Kind      TokenKind `json:"kind"`
	Text      string    `json:"text"`
	ByteStart int       `json:"byteStart"`
	ByteEnd   int       `json:"byteEnd"`
	RuneStart int       `json:"runeStart"`
	RuneEnd   int       `json:"runeEnd"`
}

// Prepared is the normalized representation produced by Prepare.
type Prepared struct {
	Source     string     `json:"source"`
	ByteLen    int        `json:"byteLen"`
	RuneCount  int        `json:"runeCount"`
	WhiteSpace WhiteSpace `json:"whiteSpace"`
	TabSize    int        `json:"tabSize"`
	Tokens     []Token    `json:"tokens"`
}

// BatchMeasurer returns token widths for a specific font key.
type BatchMeasurer interface {
	MeasureBatch(font string, texts []string) ([]float64, error)
}

// MeasuredToken is a prepared token with an attached width.
type MeasuredToken struct {
	Token
	Width float64 `json:"width"`
}

// Measured is the measured representation used by Layout and LayoutNextLine.
type Measured struct {
	Source      string          `json:"source"`
	ByteLen     int             `json:"byteLen"`
	RuneCount   int             `json:"runeCount"`
	WhiteSpace  WhiteSpace      `json:"whiteSpace"`
	TabSize     int             `json:"tabSize"`
	SpaceWidth  float64         `json:"spaceWidth"`
	HyphenWidth float64         `json:"hyphenWidth"`
	EllipsisWidth float64       `json:"ellipsisWidth"`
	Font        string          `json:"font,omitempty"`
	Tokens      []MeasuredToken `json:"tokens"`
}

// Line describes one laid-out line.
type Line struct {
	Start     int     `json:"start"`
	End       int     `json:"end"`
	ByteStart int     `json:"byteStart"`
	ByteEnd   int     `json:"byteEnd"`
	RuneStart int     `json:"runeStart"`
	RuneEnd   int     `json:"runeEnd"`
	Width     float64 `json:"width"`
	Text      string  `json:"text"`
	HardBreak bool    `json:"hardBreak"`
	SoftBreak bool    `json:"softBreak"`
	Truncated bool    `json:"truncated,omitempty"`
	Ellipsis  bool    `json:"ellipsis,omitempty"`
}

// LineRange describes one laid-out line without materializing text.
type LineRange struct {
	Start     int     `json:"start"`
	End       int     `json:"end"`
	ByteStart int     `json:"byteStart"`
	ByteEnd   int     `json:"byteEnd"`
	RuneStart int     `json:"runeStart"`
	RuneEnd   int     `json:"runeEnd"`
	Width     float64 `json:"width"`
	HardBreak bool    `json:"hardBreak"`
	SoftBreak bool    `json:"softBreak"`
	Truncated bool    `json:"truncated,omitempty"`
	Ellipsis  bool    `json:"ellipsis,omitempty"`
}

// Metrics contains aggregate layout geometry for a measured text block.
type Metrics struct {
	LineCount    int     `json:"lineCount"`
	Height       float64 `json:"height"`
	MaxLineWidth float64 `json:"maxLineWidth"`
	ByteLen      int     `json:"byteLen"`
	RuneCount    int     `json:"runeCount"`
	Truncated    bool    `json:"truncated,omitempty"`
}

// RangeResult contains laid-out line geometry without materialized text.
type RangeResult struct {
	Lines []LineRange `json:"lines"`
	Metrics
}

// Result contains the full line layout for a measured text block.
type Result struct {
	Lines []Line `json:"lines"`
	Metrics
}

// MonospaceMeasurer is a small fallback/test measurer.
type MonospaceMeasurer struct {
	Advance float64
}

// MeasureBatch returns widths using a fixed per-rune advance.
func (m MonospaceMeasurer) MeasureBatch(_ string, texts []string) ([]float64, error) {
	advance := m.Advance
	if advance <= 0 {
		advance = 1
	}
	widths := make([]float64, len(texts))
	for i, text := range texts {
		widths[i] = float64(utf8.RuneCountInString(text)) * advance
	}
	return widths, nil
}

// CachingMeasurer memoizes widths by font and token text.
type CachingMeasurer struct {
	inner BatchMeasurer
	mu    sync.Mutex
	cache map[string]float64
}

// NewCachingMeasurer wraps another measurer with an in-memory cache.
func NewCachingMeasurer(inner BatchMeasurer) *CachingMeasurer {
	return &CachingMeasurer{
		inner: inner,
		cache: make(map[string]float64),
	}
}

// MeasureBatch returns cached widths when available.
func (m *CachingMeasurer) MeasureBatch(font string, texts []string) ([]float64, error) {
	if m == nil || m.inner == nil {
		return nil, fmt.Errorf("textlayout: caching measurer requires an inner measurer")
	}
	if len(texts) == 0 {
		return []float64{}, nil
	}

	widths := make([]float64, len(texts))
	pendingTexts := make([]string, 0, len(texts))
	pendingSlots := make([][]int, 0, len(texts))
	pendingByKey := make(map[string]int, len(texts))

	m.mu.Lock()
	for i, text := range texts {
		key := cacheKey(font, text)
		if width, ok := m.cache[key]; ok {
			widths[i] = width
			continue
		}
		if idx, ok := pendingByKey[key]; ok {
			pendingSlots[idx] = append(pendingSlots[idx], i)
			continue
		}
		pendingByKey[key] = len(pendingTexts)
		pendingTexts = append(pendingTexts, text)
		pendingSlots = append(pendingSlots, []int{i})
	}
	m.mu.Unlock()

	if len(pendingTexts) == 0 {
		return widths, nil
	}

	measured, err := m.inner.MeasureBatch(font, pendingTexts)
	if err != nil {
		return nil, err
	}
	if len(measured) != len(pendingTexts) {
		return nil, fmt.Errorf("textlayout: measurer returned %d widths for %d texts", len(measured), len(pendingTexts))
	}

	m.mu.Lock()
	for i, width := range measured {
		for _, slot := range pendingSlots[i] {
			widths[slot] = width
		}
		m.cache[cacheKey(font, pendingTexts[i])] = width
	}
	m.mu.Unlock()

	return widths, nil
}

// Prepare normalizes input text into reusable layout tokens.
func Prepare(text string, opts PrepareOptions) Prepared {
	ws := normalizeWhiteSpace(opts.WhiteSpace)
	tabSize := opts.TabSize
	if tabSize <= 0 {
		tabSize = 8
	}

	text = normalizeNewlines(text)
	tokens := make([]Token, 0, len(text)/2+1)
	var word strings.Builder
	var spaces strings.Builder
	wordByteStart := -1
	wordRuneStart := -1
	spaceByteStart, spaceByteEnd := -1, 0
	spaceRuneStart, spaceRuneEnd := -1, 0

	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		tokens = append(tokens, appendWordRunTokens(word.String(), wordByteStart, wordRuneStart)...)
		word.Reset()
		wordByteStart = -1
		wordRuneStart = -1
	}
	flushSpaces := func() {
		if spaces.Len() == 0 {
			return
		}
		tokens = append(tokens, Token{
			Kind:      TokenSpace,
			Text:      spaces.String(),
			ByteStart: spaceByteStart,
			ByteEnd:   spaceByteEnd,
			RuneStart: spaceRuneStart,
			RuneEnd:   spaceRuneEnd,
		})
		spaces.Reset()
		spaceByteStart, spaceByteEnd = -1, 0
		spaceRuneStart, spaceRuneEnd = -1, 0
	}
	appendCollapsedSpace := func(byteStart, byteEnd, runeStart, runeEnd int) {
		flushWord()
		if len(tokens) == 0 {
			return
		}
		if tokens[len(tokens)-1].Kind == TokenSpace {
			tokens[len(tokens)-1].ByteEnd = byteEnd
			tokens[len(tokens)-1].RuneEnd = runeEnd
			return
		}
		tokens = append(tokens, Token{
			Kind:      TokenSpace,
			Text:      " ",
			ByteStart: byteStart,
			ByteEnd:   byteEnd,
			RuneStart: runeStart,
			RuneEnd:   runeEnd,
		})
	}

	runeIndex := 0
	for byteStart, r := range text {
		_, size := utf8.DecodeRuneInString(text[byteStart:])
		byteEnd := byteStart + size
		runeStart := runeIndex
		runeEnd := runeIndex + 1
		switch {
		case r == '\n':
			if ws == WhiteSpaceNormal {
				appendCollapsedSpace(byteStart, byteEnd, runeStart, runeEnd)
				runeIndex++
				continue
			}
			flushWord()
			flushSpaces()
			tokens = append(tokens, Token{
				Kind:      TokenNewline,
				Text:      "\n",
				ByteStart: byteStart,
				ByteEnd:   byteEnd,
				RuneStart: runeStart,
				RuneEnd:   runeEnd,
			})
		case r == '\t':
			if ws == WhiteSpaceNormal {
				appendCollapsedSpace(byteStart, byteEnd, runeStart, runeEnd)
				runeIndex++
				continue
			}
			flushWord()
			flushSpaces()
			tokens = append(tokens, Token{
				Kind:      TokenTab,
				Text:      "\t",
				ByteStart: byteStart,
				ByteEnd:   byteEnd,
				RuneStart: runeStart,
				RuneEnd:   runeEnd,
			})
		case r == '\u00ad':
			flushWord()
			flushSpaces()
			tokens = append(tokens, Token{
				Kind:      TokenSoftHyphen,
				Text:      "\u00ad",
				ByteStart: byteStart,
				ByteEnd:   byteEnd,
				RuneStart: runeStart,
				RuneEnd:   runeEnd,
			})
		case r == '\u200b':
			flushWord()
			flushSpaces()
			tokens = append(tokens, Token{
				Kind:      TokenBreak,
				Text:      "\u200b",
				ByteStart: byteStart,
				ByteEnd:   byteEnd,
				RuneStart: runeStart,
				RuneEnd:   runeEnd,
			})
		case unicode.IsSpace(r):
			if ws == WhiteSpaceNormal {
				appendCollapsedSpace(byteStart, byteEnd, runeStart, runeEnd)
				runeIndex++
				continue
			}
			flushWord()
			if spaceByteStart < 0 {
				spaceByteStart = byteStart
				spaceRuneStart = runeStart
			}
			spaceByteEnd = byteEnd
			spaceRuneEnd = runeEnd
			spaces.WriteRune(r)
		default:
			flushSpaces()
			if wordByteStart < 0 {
				wordByteStart = byteStart
				wordRuneStart = runeStart
			}
			word.WriteRune(r)
		}
		runeIndex++
	}

	flushWord()
	flushSpaces()

	return Prepared{
		Source:     text,
		ByteLen:    len(text),
		RuneCount:  runeIndex,
		WhiteSpace: ws,
		TabSize:    tabSize,
		Tokens:     tokens,
	}
}

// Measure attaches widths to prepared tokens using the provided measurer.
func Measure(prepared Prepared, measurer BatchMeasurer, font string) (Measured, error) {
	if measurer == nil {
		return Measured{}, fmt.Errorf("textlayout: nil measurer")
	}

	expanded := expandPreparedTokens(prepared.Tokens)
	texts := make([]string, 0, len(expanded))
	indexes := make([]int, 0, len(expanded))
	measured := Measured{
		Source:     prepared.Source,
		ByteLen:    prepared.ByteLen,
		RuneCount:  prepared.RuneCount,
		WhiteSpace: normalizeWhiteSpace(prepared.WhiteSpace),
		TabSize:    prepared.TabSize,
		Font:       font,
		Tokens:     make([]MeasuredToken, len(expanded)),
	}
	needSpaceWidth := false
	needHyphenWidth := false
	needEllipsisWidth := true

	for i, token := range expanded {
		measured.Tokens[i].Token = token
		switch token.Kind {
		case TokenNewline, TokenTab, TokenSoftHyphen, TokenBreak:
			if token.Kind == TokenTab {
				needSpaceWidth = true
			}
			if token.Kind == TokenSoftHyphen {
				needHyphenWidth = true
			}
			continue
		}
		texts = append(texts, token.Text)
		indexes = append(indexes, i)
	}
	spaceIndex := -1
	hyphenIndex := -1
	ellipsisIndex := -1
	if needSpaceWidth {
		spaceIndex = len(texts)
		texts = append(texts, " ")
	}
	if needHyphenWidth {
		hyphenIndex = len(texts)
		texts = append(texts, "-")
	}
	if needEllipsisWidth {
		ellipsisIndex = len(texts)
		texts = append(texts, "…")
	}

	widths, err := measurer.MeasureBatch(font, texts)
	if err != nil {
		return Measured{}, err
	}
	if len(widths) != len(texts) {
		return Measured{}, fmt.Errorf("textlayout: measurer returned %d widths for %d texts", len(widths), len(texts))
	}

	for i, width := range widths {
		if i < len(indexes) {
			measured.Tokens[indexes[i]].Width = width
		}
	}
	if spaceIndex >= 0 && spaceIndex < len(widths) {
		measured.SpaceWidth = widths[spaceIndex]
	}
	if hyphenIndex >= 0 && hyphenIndex < len(widths) {
		measured.HyphenWidth = widths[hyphenIndex]
	}
	if ellipsisIndex >= 0 && ellipsisIndex < len(widths) {
		measured.EllipsisWidth = widths[ellipsisIndex]
	}

	return measured, nil
}

func appendWordRunTokens(text string, byteStart, runeStart int) []Token {
	if text == "" {
		return nil
	}

	out := make([]Token, 0, utf8.RuneCountInString(text)+2)
	byteOffset := byteStart
	runeOffset := runeStart
	havePending := false
	pending := Token{}
	emitted := false

	emit := func(token Token, breakBefore bool) {
		if breakBefore && emitted {
			out = append(out, Token{
				Kind:      TokenBreak,
				Text:      "",
				ByteStart: token.ByteStart,
				ByteEnd:   token.ByteStart,
				RuneStart: token.RuneStart,
				RuneEnd:   token.RuneStart,
			})
		}
		out = append(out, token)
		emitted = true
	}

	appendToPending := func(token Token) {
		if !havePending {
			pending = token
			havePending = true
			return
		}
		pending.Text += token.Text
		pending.ByteEnd = token.ByteEnd
		pending.RuneEnd = token.RuneEnd
	}

	for _, segment := range segmentWordRunStrings(text) {
		if segment == "" {
			continue
		}
		byteLen := len(segment)
		runeLen := utf8.RuneCountInString(segment)
		token := Token{
			Kind:      TokenWord,
			Text:      segment,
			ByteStart: byteOffset,
			ByteEnd:   byteOffset + byteLen,
			RuneStart: runeOffset,
			RuneEnd:   runeOffset + runeLen,
		}
		byteOffset += byteLen
		runeOffset += runeLen

		switch {
		case lineEndProhibited(token.Text):
			appendToPending(token)
		case lineStartProhibited(token.Text):
			if len(out) > 0 && out[len(out)-1].Kind == TokenWord {
				out[len(out)-1].Text += token.Text
				out[len(out)-1].ByteEnd = token.ByteEnd
				out[len(out)-1].RuneEnd = token.RuneEnd
				continue
			}
			if havePending {
				appendToPending(token)
				continue
			}
			emit(token, emitted)
		default:
			if havePending {
				token.Text = pending.Text + token.Text
				token.ByteStart = pending.ByteStart
				token.RuneStart = pending.RuneStart
				havePending = false
			}
			emit(token, emitted)
		}
	}

	if havePending {
		emit(pending, emitted)
	}

	return out
}

func expandPreparedTokens(tokens []Token) []Token {
	if len(tokens) == 0 {
		return nil
	}
	expanded := make([]Token, 0, len(tokens))
	for _, token := range tokens {
		expanded = append(expanded, expandPreparedToken(token)...)
	}
	return expanded
}

func expandPreparedToken(token Token) []Token {
	if token.Kind == TokenNewline || token.Kind == TokenTab || token.Kind == TokenSoftHyphen || token.Kind == TokenBreak || token.Text == "" {
		return []Token{token}
	}
	if utf8.RuneCountInString(token.Text) <= 1 {
		return []Token{token}
	}

	graphemes := uniseg.NewGraphemes(token.Text)
	expanded := make([]Token, 0, utf8.RuneCountInString(token.Text))
	byteOffset := token.ByteStart
	runeOffset := token.RuneStart
	for graphemes.Next() {
		text := graphemes.Str()
		byteLen := len(text)
		runeLen := utf8.RuneCountInString(text)
		expanded = append(expanded, Token{
			Kind:      token.Kind,
			Text:      text,
			ByteStart: byteOffset,
			ByteEnd:   byteOffset + byteLen,
			RuneStart: runeOffset,
			RuneEnd:   runeOffset + runeLen,
		})
		byteOffset += byteLen
		runeOffset += runeLen
	}
	if len(expanded) == 0 {
		return []Token{token}
	}
	return expanded
}

// LayoutText is a convenience wrapper for Prepare + Measure + Layout.
func LayoutText(text string, measurer BatchMeasurer, font string, prepare PrepareOptions, layout LayoutOptions) (Result, error) {
	prepared := Prepare(text, prepare)
	measured, err := Measure(prepared, measurer, font)
	if err != nil {
		return Result{}, err
	}
	return Layout(measured, layout), nil
}

// LayoutTextMetrics is a convenience wrapper for Prepare + Measure + LayoutMetrics.
func LayoutTextMetrics(text string, measurer BatchMeasurer, font string, prepare PrepareOptions, layout LayoutOptions) (Metrics, error) {
	prepared := Prepare(text, prepare)
	measured, err := Measure(prepared, measurer, font)
	if err != nil {
		return Metrics{}, err
	}
	return LayoutMetrics(measured, layout), nil
}

// LayoutTextRanges is a convenience wrapper for Prepare + Measure + LayoutRanges.
func LayoutTextRanges(text string, measurer BatchMeasurer, font string, prepare PrepareOptions, layout LayoutOptions) (RangeResult, error) {
	prepared := Prepare(text, prepare)
	measured, err := Measure(prepared, measurer, font)
	if err != nil {
		return RangeResult{}, err
	}
	return LayoutRanges(measured, layout), nil
}

// Layout walks the measured text and returns every line.
func Layout(measured Measured, opts LayoutOptions) Result {
	lineHeight := opts.LineHeight
	if lineHeight <= 0 {
		lineHeight = 1
	}

	if len(measured.Tokens) == 0 {
		return Result{
			Lines: []Line{{
				ByteStart: measured.ByteLen,
				ByteEnd:   measured.ByteLen,
				RuneStart: measured.RuneCount,
				RuneEnd:   measured.RuneCount,
			}},
			Metrics: Metrics{
				LineCount:    1,
				Height:       lineHeight,
				MaxLineWidth: 0,
				ByteLen:      measured.ByteLen,
				RuneCount:    measured.RuneCount,
			},
		}
	}

	lines := make([]Line, 0, 8)
	metrics := Metrics{
		ByteLen:   measured.ByteLen,
		RuneCount: measured.RuneCount,
	}
	WalkLineRanges(measured, opts, func(lineRange LineRange) bool {
		lines = append(lines, lineFromRange(measured, lineRange))
		metrics.LineCount++
		if lineRange.Width > metrics.MaxLineWidth {
			metrics.MaxLineWidth = lineRange.Width
		}
		if lineRange.Truncated {
			metrics.Truncated = true
		}
		return true
	})
	metrics.Height = float64(metrics.LineCount) * lineHeight

	return Result{
		Lines:   lines,
		Metrics: metrics,
	}
}

// LayoutMetrics computes aggregate layout geometry without materializing line text.
func LayoutMetrics(measured Measured, opts LayoutOptions) Metrics {
	return LayoutRanges(measured, opts).Metrics
}

// LayoutRanges computes laid-out line geometry without materializing line text.
func LayoutRanges(measured Measured, opts LayoutOptions) RangeResult {
	lineHeight := opts.LineHeight
	if lineHeight <= 0 {
		lineHeight = 1
	}

	if len(measured.Tokens) == 0 {
		return RangeResult{
			Lines: []LineRange{{
				ByteStart: measured.ByteLen,
				ByteEnd:   measured.ByteLen,
				RuneStart: measured.RuneCount,
				RuneEnd:   measured.RuneCount,
			}},
			Metrics: Metrics{
				LineCount:    1,
				Height:       lineHeight,
				MaxLineWidth: 0,
				ByteLen:      measured.ByteLen,
				RuneCount:    measured.RuneCount,
			},
		}
	}

	result := RangeResult{
		Lines: make([]LineRange, 0, 8),
		Metrics: Metrics{
			ByteLen:   measured.ByteLen,
			RuneCount: measured.RuneCount,
		},
	}
	WalkLineRanges(measured, opts, func(line LineRange) bool {
		result.Lines = append(result.Lines, line)
		result.LineCount++
		if line.Width > result.MaxLineWidth {
			result.MaxLineWidth = line.Width
		}
		if line.Truncated {
			result.Truncated = true
		}
		return true
	})
	result.Height = float64(result.LineCount) * lineHeight
	return result
}

// WalkLines iterates until the callback returns false or the text is exhausted.
func WalkLines(measured Measured, opts LayoutOptions, fn func(Line) bool) {
	if fn == nil {
		return
	}
	if len(measured.Tokens) == 0 {
		fn(Line{
			ByteStart: measured.ByteLen,
			ByteEnd:   measured.ByteLen,
			RuneStart: measured.RuneCount,
			RuneEnd:   measured.RuneCount,
		})
		return
	}
	WalkLineRanges(measured, opts, func(line LineRange) bool {
		return fn(lineFromRange(measured, line))
	})
}

// WalkLineRanges iterates over laid-out line geometry without materializing line text.
func WalkLineRanges(measured Measured, opts LayoutOptions, fn func(LineRange) bool) {
	if fn == nil {
		return
	}
	if len(measured.Tokens) == 0 {
		fn(LineRange{
			ByteStart: measured.ByteLen,
			ByteEnd:   measured.ByteLen,
			RuneStart: measured.RuneCount,
			RuneEnd:   measured.RuneCount,
		})
		return
	}
	maxLines := opts.MaxLines
	lineCount := 0
	for start := 0; start < len(measured.Tokens); {
		line, next := LayoutNextLineRange(measured, start, opts)
		lineCount++
		if maxLines > 0 && lineCount == maxLines && hasMoreLineContent(measured, next) {
			line = clampLineRange(measured, line, opts)
			fn(line)
			return
		}
		if !fn(line) {
			return
		}
		if next <= start {
			next = start + 1
		}
		start = next
	}
	if measured.Tokens[len(measured.Tokens)-1].Kind == TokenNewline {
		if maxLines > 0 && lineCount >= maxLines {
			return
		}
		fn(emptyLineAtEndRange(measured))
	}
}

// LayoutNextLine returns one line plus the next token index to continue from.
func LayoutNextLine(measured Measured, start int, opts LayoutOptions) (Line, int) {
	line, next := LayoutNextLineRange(measured, start, opts)
	return lineFromRange(measured, line), next
}

// LayoutNextLineRange returns one line range plus the next token index to continue from.
func LayoutNextLineRange(measured Measured, start int, opts LayoutOptions) (LineRange, int) {
	tokens := measured.Tokens
	if start < 0 {
		start = 0
	}
	if start >= len(tokens) {
		return emptyLineAtEndRange(measured), len(tokens)
	}

	ws := normalizeWhiteSpace(measured.WhiteSpace)
	lineStart := normalizeWrappedLineStart(tokens, start, ws)
	if lineStart >= len(tokens) {
		return emptyLineAtEndRange(measured), len(tokens)
	}

	if tokens[lineStart].Kind == TokenNewline {
		return emptyLineAtIndexRange(tokens, lineStart, true), lineStart + 1
	}

	if ws == WhiteSpacePre {
		return layoutPreLineRange(measured, lineStart)
	}
	return layoutWrappedLineRange(measured, lineStart, ws, opts.MaxWidth)
}

func layoutPreLineRange(measured Measured, start int) (LineRange, int) {
	tokens := measured.Tokens
	for i := start; i < len(tokens); i++ {
		if tokens[i].Kind != TokenNewline {
			continue
		}
		line := buildLineRange(measured, start, i, true, false)
		return line, i + 1
	}
	line := buildLineRange(measured, start, len(tokens), false, false)
	return line, len(tokens)
}

func layoutWrappedLineRange(measured Measured, start int, ws WhiteSpace, maxWidth float64) (LineRange, int) {
	tokens := measured.Tokens
	lineWidth := 0.0
	lastBreak := -1
	lastBreakWidth := 0.0
	lastBreakSoft := false

	for i := start; i < len(tokens); i++ {
		token := tokens[i]
		if token.Kind == TokenNewline {
			return buildLineRange(measured, start, i, true, false), i + 1
		}

		tokenWidth := tokenProgressWidth(measured, lineWidth, token)
		fitAdvance := tokenFitAdvance(measured, lineWidth, token)
		paintAdvance := tokenPaintAdvance(measured, lineWidth, token, token.Kind == TokenSoftHyphen)

		if canBreakAfter(token.Kind) {
			lastBreak = i + 1
			lastBreakWidth = lineWidth + paintAdvance
			lastBreakSoft = token.Kind == TokenSoftHyphen
		}

		candidateWidth := lineWidth + tokenWidth
		if maxWidth > 0 && candidateWidth > maxWidth {
			if canBreakAfter(token.Kind) && lineWidth+fitAdvance <= maxWidth {
				line := buildLineRange(measured, start, i+1, false, token.Kind == TokenSoftHyphen)
				return line, normalizeWrappedLineNext(tokens, i+1, ws)
			}
			if lastBreak > start {
				line := buildLineRangeWithWidth(measured, start, lastBreak, false, lastBreakSoft, lastBreakWidth)
				return line, normalizeWrappedLineNext(tokens, lastBreak, ws)
			}
			if i > start && lineEndProhibited(tokens[i-1].Text) {
				line := buildLineRange(measured, start, i+1, false, false)
				return line, normalizeWrappedLineNext(tokens, i+1, ws)
			}
			if lineWidth > 0 && lineStartProhibited(token.Text) {
				line := buildLineRange(measured, start, i+1, false, false)
				return line, normalizeWrappedLineNext(tokens, i+1, ws)
			}
			if lineWidth == 0 {
				line := buildLineRange(measured, start, i+1, false, false)
				return line, normalizeWrappedLineNext(tokens, i+1, ws)
			}
			line := buildLineRange(measured, start, i, false, false)
			return line, normalizeWrappedLineNext(tokens, i, ws)
		}
		lineWidth = candidateWidth
	}

	return buildLineRange(measured, start, len(tokens), false, false), len(tokens)
}

func buildLineRange(measured Measured, start, end int, hardBreak, softBreak bool) LineRange {
	return buildLineRangeWithWidth(measured, start, end, hardBreak, softBreak, lineDisplayWidth(measured, start, end, softBreak))
}

func buildLineRangeWithWidth(measured Measured, start, end int, hardBreak, softBreak bool, width float64) LineRange {
	tokens := measured.Tokens
	line := LineRange{
		Start:     start,
		End:       end,
		Width:     width,
		HardBreak: hardBreak,
		SoftBreak: softBreak,
	}
	if start < end {
		line.ByteStart = tokens[start].ByteStart
		line.ByteEnd = tokens[end-1].ByteEnd
		line.RuneStart = tokens[start].RuneStart
		line.RuneEnd = tokens[end-1].RuneEnd
	}
	return line
}

func lineFromRange(measured Measured, line LineRange) Line {
	text := lineText(measured, line.Start, line.End, line.SoftBreak)
	if line.Ellipsis {
		text += "…"
	}
	return Line{
		Start:     line.Start,
		End:       line.End,
		ByteStart: line.ByteStart,
		ByteEnd:   line.ByteEnd,
		RuneStart: line.RuneStart,
		RuneEnd:   line.RuneEnd,
		Width:     line.Width,
		Text:      text,
		HardBreak: line.HardBreak,
		SoftBreak: line.SoftBreak,
		Truncated: line.Truncated,
		Ellipsis:  line.Ellipsis,
	}
}

func hasMoreLineContent(measured Measured, next int) bool {
	if next < len(measured.Tokens) {
		return true
	}
	return len(measured.Tokens) > 0 && measured.Tokens[len(measured.Tokens)-1].Kind == TokenNewline
}

func clampLineRange(measured Measured, line LineRange, opts LayoutOptions) LineRange {
	line.Truncated = true
	line.HardBreak = false
	line.SoftBreak = false

	if normalizeOverflow(opts.Overflow) != OverflowEllipsis {
		return line
	}

	ellipsisWidth := ellipsisAdvance(measured)
	if opts.MaxWidth <= 0 {
		line.Ellipsis = true
		line.Width += ellipsisWidth
		return line
	}

	allowedWidth := opts.MaxWidth - ellipsisWidth
	end := trimDisplayLineEnd(measured, line.Start, line.End)
	for end > line.Start && lineDisplayWidth(measured, line.Start, end, false) > allowedWidth {
		end--
		end = trimDisplayLineEnd(measured, line.Start, end)
	}

	if end <= line.Start {
		line.End = line.Start
		line.ByteEnd = line.ByteStart
		line.RuneEnd = line.RuneStart
		line.Width = minPositiveWidth(opts.MaxWidth, ellipsisWidth)
		line.Ellipsis = true
		return line
	}

	line.End = end
	line.ByteEnd = measured.Tokens[end-1].ByteEnd
	line.RuneEnd = measured.Tokens[end-1].RuneEnd
	line.Width = minPositiveWidth(opts.MaxWidth, lineDisplayWidth(measured, line.Start, end, false)+ellipsisWidth)
	line.Ellipsis = true
	return line
}

func trimDisplayLineEnd(measured Measured, start, end int) int {
	if normalizeWhiteSpace(measured.WhiteSpace) != WhiteSpaceNormal {
		return end
	}
	for end > start && measured.Tokens[end-1].Kind == TokenSpace {
		end--
	}
	return end
}

func minPositiveWidth(maxWidth, actual float64) float64 {
	if maxWidth > 0 && actual > maxWidth {
		return maxWidth
	}
	return actual
}

func emptyLineAtIndexRange(tokens []MeasuredToken, index int, hardBreak bool) LineRange {
	line := LineRange{
		Start:     index,
		End:       index,
		HardBreak: hardBreak,
	}
	if index >= 0 && index < len(tokens) {
		line.ByteStart = tokens[index].ByteStart
		line.ByteEnd = tokens[index].ByteStart
		line.RuneStart = tokens[index].RuneStart
		line.RuneEnd = tokens[index].RuneStart
	}
	return line
}

func emptyLineAtEndRange(measured Measured) LineRange {
	line := LineRange{
		Start: len(measured.Tokens),
		End:   len(measured.Tokens),
	}
	if len(measured.Tokens) == 0 {
		return line
	}
	line.ByteStart = measured.ByteLen
	line.ByteEnd = measured.ByteLen
	line.RuneStart = measured.RuneCount
	line.RuneEnd = measured.RuneCount
	return line
}

func lineText(measured Measured, start, end int, softBreak bool) string {
	if start >= end {
		if softBreak {
			return "-"
		}
		return ""
	}
	tokens := measured.Tokens
	textEnd := end
	if normalizeWhiteSpace(measured.WhiteSpace) == WhiteSpaceNormal {
		for textEnd > start && tokens[textEnd-1].Kind == TokenSpace {
			textEnd--
		}
	}
	var b strings.Builder
	for i := start; i < textEnd && i < len(tokens); i++ {
		switch tokens[i].Kind {
		case TokenNewline, TokenSoftHyphen, TokenBreak:
			continue
		default:
			b.WriteString(tokens[i].Text)
		}
	}
	if softBreak {
		b.WriteByte('-')
	}
	return b.String()
}

func lineDisplayWidth(measured Measured, start, end int, softBreak bool) float64 {
	if start >= end {
		return 0
	}
	progress := 0.0
	display := 0.0
	for i := start; i < end && i < len(measured.Tokens); i++ {
		token := measured.Tokens[i]
		before := progress
		progress += tokenProgressWidth(measured, progress, token)
		display = before + tokenPaintAdvance(measured, before, token, softBreak && i == end-1 && token.Kind == TokenSoftHyphen)
	}
	return display
}

func tokenProgressWidth(measured Measured, lineWidth float64, token MeasuredToken) float64 {
	switch token.Kind {
	case TokenTab:
		return tabAdvance(measured, lineWidth)
	case TokenSoftHyphen, TokenBreak, TokenNewline:
		return 0
	default:
		return token.Width
	}
}

func tokenFitAdvance(measured Measured, lineWidth float64, token MeasuredToken) float64 {
	switch token.Kind {
	case TokenSpace:
		if normalizeWhiteSpace(measured.WhiteSpace) == WhiteSpaceNormal {
			return 0
		}
		return token.Width
	case TokenTab:
		return 0
	case TokenSoftHyphen:
		return hyphenAdvance(measured)
	case TokenBreak, TokenNewline:
		return 0
	default:
		return token.Width
	}
}

func tokenPaintAdvance(measured Measured, lineWidth float64, token MeasuredToken, softBreak bool) float64 {
	switch token.Kind {
	case TokenSpace:
		if normalizeWhiteSpace(measured.WhiteSpace) == WhiteSpaceNormal {
			return 0
		}
		return token.Width
	case TokenTab:
		return tabAdvance(measured, lineWidth)
	case TokenSoftHyphen:
		if softBreak {
			return hyphenAdvance(measured)
		}
		return 0
	case TokenBreak, TokenNewline:
		return 0
	default:
		return token.Width
	}
}

func tabAdvance(measured Measured, lineWidth float64) float64 {
	tabSize := measured.TabSize
	if tabSize <= 0 {
		tabSize = 8
	}
	spaceWidth := measured.SpaceWidth
	if spaceWidth <= 0 {
		spaceWidth = 1
	}
	tabStop := float64(tabSize) * spaceWidth
	if tabStop <= 0 {
		return 0
	}
	remainder := mathMod(lineWidth, tabStop)
	if remainder == 0 {
		return tabStop
	}
	return tabStop - remainder
}

func hyphenAdvance(measured Measured) float64 {
	if measured.HyphenWidth > 0 {
		return measured.HyphenWidth
	}
	return 1
}

func ellipsisAdvance(measured Measured) float64 {
	if measured.EllipsisWidth > 0 {
		return measured.EllipsisWidth
	}
	return 1
}

func canBreakAfter(kind TokenKind) bool {
	switch kind {
	case TokenSpace, TokenTab, TokenSoftHyphen, TokenBreak:
		return true
	default:
		return false
	}
}

func lineStartProhibited(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !isLineStartProhibitedRune(r) {
			return false
		}
	}
	return true
}

func lineEndProhibited(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if !isLineEndProhibitedRune(r) {
			return false
		}
	}
	return true
}

func isLineStartProhibitedRune(r rune) bool {
	switch r {
	case '.', ',', '!', '?', ':', ';', ')', ']', '}', '%', '"', '”', '’', '»', '›', '…',
		'、', '。', '，', '．', '！', '？', '：', '；',
		'）', '】', '」', '』', '》', '〉', '〕', '〗', '〙', '〛',
		'ー', '々', 'ゝ', 'ゞ', 'ヽ', 'ヾ':
		return true
	default:
		return false
	}
}

func isLineEndProhibitedRune(r rune) bool {
	switch r {
	case '"', '“', '‘', '«', '‹', '(', '[', '{',
		'（', '【', '「', '『', '《', '〈', '〔', '〖', '〘', '〚':
		return true
	default:
		return false
	}
}

func normalizeWrappedLineStart(tokens []MeasuredToken, start int, ws WhiteSpace) int {
	for start < len(tokens) {
		switch tokens[start].Kind {
		case TokenBreak, TokenSoftHyphen:
			start++
		case TokenSpace:
			if ws == WhiteSpaceNormal {
				start++
				continue
			}
			return start
		default:
			return start
		}
	}
	return start
}

func normalizeWrappedLineNext(tokens []MeasuredToken, start int, ws WhiteSpace) int {
	for start < len(tokens) {
		switch tokens[start].Kind {
		case TokenBreak, TokenSoftHyphen:
			start++
		case TokenSpace:
			if ws == WhiteSpaceNormal {
				start++
				continue
			}
			return start
		default:
			return start
		}
	}
	return start
}

func mathMod(value, divisor float64) float64 {
	if divisor == 0 {
		return 0
	}
	remainder := value - float64(int(value/divisor))*divisor
	if remainder < 0 {
		remainder += divisor
	}
	if remainder < 1e-9 || divisor-remainder < 1e-9 {
		return 0
	}
	return remainder
}

func normalizeWhiteSpace(ws WhiteSpace) WhiteSpace {
	switch ws {
	case WhiteSpacePreWrap, WhiteSpacePre:
		return ws
	default:
		return WhiteSpaceNormal
	}
}

func normalizeOverflow(mode OverflowMode) OverflowMode {
	switch mode {
	case OverflowEllipsis:
		return OverflowEllipsis
	default:
		return OverflowClip
	}
}

func normalizeNewlines(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

func cacheKey(font, text string) string {
	return font + "\x00" + text
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}
