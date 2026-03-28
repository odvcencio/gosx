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

// TokenKind identifies the layout role of a prepared token.
type TokenKind string

const (
	TokenWord    TokenKind = "word"
	TokenSpace   TokenKind = "space"
	TokenNewline TokenKind = "newline"
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
	Source     string          `json:"source"`
	ByteLen    int             `json:"byteLen"`
	RuneCount  int             `json:"runeCount"`
	WhiteSpace WhiteSpace      `json:"whiteSpace"`
	Font       string          `json:"font,omitempty"`
	Tokens     []MeasuredToken `json:"tokens"`
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
}

// Result contains the full line layout for a measured text block.
type Result struct {
	Lines        []Line  `json:"lines"`
	LineCount    int     `json:"lineCount"`
	Height       float64 `json:"height"`
	MaxLineWidth float64 `json:"maxLineWidth"`
	ByteLen      int     `json:"byteLen"`
	RuneCount    int     `json:"runeCount"`
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
		tabSize = 4
	}

	text = normalizeNewlines(text)
	tokens := make([]Token, 0, len(text)/2+1)
	var word strings.Builder
	var spaces strings.Builder
	wordByteStart, wordByteEnd := -1, 0
	wordRuneStart, wordRuneEnd := -1, 0
	spaceByteStart, spaceByteEnd := -1, 0
	spaceRuneStart, spaceRuneEnd := -1, 0

	flushWord := func() {
		if word.Len() == 0 {
			return
		}
		tokens = append(tokens, Token{
			Kind:      TokenWord,
			Text:      word.String(),
			ByteStart: wordByteStart,
			ByteEnd:   wordByteEnd,
			RuneStart: wordRuneStart,
			RuneEnd:   wordRuneEnd,
		})
		word.Reset()
		wordByteStart, wordByteEnd = -1, 0
		wordRuneStart, wordRuneEnd = -1, 0
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
			if spaceByteStart < 0 {
				spaceByteStart = byteStart
				spaceRuneStart = runeStart
			}
			spaceByteEnd = byteEnd
			spaceRuneEnd = runeEnd
			spaces.WriteString(strings.Repeat(" ", tabSize))
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
			wordByteEnd = byteEnd
			wordRuneEnd = runeEnd
			if isCJK(r) {
				flushWord()
				tokens = append(tokens, Token{
					Kind:      TokenWord,
					Text:      string(r),
					ByteStart: byteStart,
					ByteEnd:   byteEnd,
					RuneStart: runeStart,
					RuneEnd:   runeEnd,
				})
				wordByteStart, wordByteEnd = -1, 0
				wordRuneStart, wordRuneEnd = -1, 0
				runeIndex++
				continue
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
		Font:       font,
		Tokens:     make([]MeasuredToken, len(expanded)),
	}

	for i, token := range expanded {
		measured.Tokens[i].Token = token
		if token.Kind == TokenNewline {
			continue
		}
		texts = append(texts, token.Text)
		indexes = append(indexes, i)
	}

	widths, err := measurer.MeasureBatch(font, texts)
	if err != nil {
		return Measured{}, err
	}
	if len(widths) != len(texts) {
		return Measured{}, fmt.Errorf("textlayout: measurer returned %d widths for %d texts", len(widths), len(texts))
	}

	for i, width := range widths {
		measured.Tokens[indexes[i]].Width = width
	}

	return measured, nil
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
	if token.Kind == TokenNewline || token.Text == "" {
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
			LineCount:    1,
			Height:       lineHeight,
			MaxLineWidth: 0,
			ByteLen:      measured.ByteLen,
			RuneCount:    measured.RuneCount,
		}
	}

	lines := make([]Line, 0, 8)
	for start := 0; start < len(measured.Tokens); {
		line, next := LayoutNextLine(measured, start, opts)
		lines = append(lines, line)
		if next <= start {
			next = start + 1
		}
		start = next
	}

	if measured.Tokens[len(measured.Tokens)-1].Kind == TokenNewline {
		lines = append(lines, emptyLineAtEnd(measured))
	}

	var maxLineWidth float64
	for _, line := range lines {
		if line.Width > maxLineWidth {
			maxLineWidth = line.Width
		}
	}

	return Result{
		Lines:        lines,
		LineCount:    len(lines),
		Height:       float64(len(lines)) * lineHeight,
		MaxLineWidth: maxLineWidth,
		ByteLen:      measured.ByteLen,
		RuneCount:    measured.RuneCount,
	}
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
	for start := 0; start < len(measured.Tokens); {
		line, next := LayoutNextLine(measured, start, opts)
		if !fn(line) {
			return
		}
		if next <= start {
			next = start + 1
		}
		start = next
	}
	if measured.Tokens[len(measured.Tokens)-1].Kind == TokenNewline {
		fn(emptyLineAtEnd(measured))
	}
}

// LayoutNextLine returns one line plus the next token index to continue from.
func LayoutNextLine(measured Measured, start int, opts LayoutOptions) (Line, int) {
	tokens := measured.Tokens
	if start < 0 {
		start = 0
	}
	if start >= len(tokens) {
		return emptyLineAtEnd(measured), len(tokens)
	}

	ws := normalizeWhiteSpace(measured.WhiteSpace)
	lineStart := start
	if ws == WhiteSpaceNormal {
		for lineStart < len(tokens) && tokens[lineStart].Kind == TokenSpace {
			lineStart++
		}
		if lineStart >= len(tokens) {
			return emptyLineAtEnd(measured), len(tokens)
		}
	}

	if tokens[lineStart].Kind == TokenNewline {
		return emptyLineAtIndex(tokens, lineStart, true), lineStart + 1
	}

	if ws == WhiteSpacePre {
		return layoutPreLine(tokens, lineStart)
	}
	return layoutWrappedLine(tokens, lineStart, ws, opts.MaxWidth)
}

func layoutPreLine(tokens []MeasuredToken, start int) (Line, int) {
	for i := start; i < len(tokens); i++ {
		if tokens[i].Kind != TokenNewline {
			continue
		}
		return buildLine(tokens, start, i, true), i + 1
	}
	return buildLine(tokens, start, len(tokens), false), len(tokens)
}

func layoutWrappedLine(tokens []MeasuredToken, start int, ws WhiteSpace, maxWidth float64) (Line, int) {
	lineWidth := 0.0
	lastBreak := -1

	for i := start; i < len(tokens); i++ {
		token := tokens[i]
		if token.Kind == TokenNewline {
			return buildLine(tokens, start, i, true), i + 1
		}

		breakAt := lastBreak
		if token.Kind == TokenSpace {
			if ws == WhiteSpaceNormal {
				breakAt = i
			} else {
				breakAt = i + 1
			}
		}

		candidateWidth := lineWidth + token.Width
		if maxWidth > 0 && lineWidth == 0 && candidateWidth > maxWidth {
			return buildLine(tokens, start, i+1, false), i + 1
		}
		if maxWidth > 0 && lineWidth > 0 && candidateWidth > maxWidth {
			if breakAt > start {
				next := breakAt
				if ws == WhiteSpaceNormal {
					for next < len(tokens) && tokens[next].Kind == TokenSpace {
						next++
					}
				}
				return buildLine(tokens, start, breakAt, false), next
			}
			return buildLine(tokens, start, i, false), i
		}

		lineWidth = candidateWidth
		lastBreak = breakAt
	}

	return buildLine(tokens, start, len(tokens), false), len(tokens)
}

func buildLine(tokens []MeasuredToken, start, end int, hardBreak bool) Line {
	line := Line{
		Start:     start,
		End:       end,
		Width:     tokensWidth(tokens, start, end),
		Text:      tokensText(tokens, start, end),
		HardBreak: hardBreak,
	}
	if start < end {
		line.ByteStart = tokens[start].ByteStart
		line.ByteEnd = tokens[end-1].ByteEnd
		line.RuneStart = tokens[start].RuneStart
		line.RuneEnd = tokens[end-1].RuneEnd
	}
	return line
}

func emptyLineAtIndex(tokens []MeasuredToken, index int, hardBreak bool) Line {
	line := Line{
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

func emptyLineAtEnd(measured Measured) Line {
	line := Line{
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

func tokensWidth(tokens []MeasuredToken, start, end int) float64 {
	width := 0.0
	for i := start; i < end && i < len(tokens); i++ {
		width += tokens[i].Width
	}
	return width
}

func tokensText(tokens []MeasuredToken, start, end int) string {
	if start >= end {
		return ""
	}
	var b strings.Builder
	for i := start; i < end && i < len(tokens); i++ {
		b.WriteString(tokens[i].Text)
	}
	return b.String()
}

func normalizeWhiteSpace(ws WhiteSpace) WhiteSpace {
	switch ws {
	case WhiteSpacePreWrap, WhiteSpacePre:
		return ws
	default:
		return WhiteSpaceNormal
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
