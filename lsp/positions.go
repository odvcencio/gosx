package lsp

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type byteRange struct {
	start int
	end   int
}

func byteOffsetForSourcePoint(source string, line, byteCharacter int) int {
	if line <= 0 && byteCharacter <= 0 {
		return 0
	}
	lineStart, lineEnd := lineByteBounds(source, line)
	offset := lineStart + clampZeroBased(byteCharacter)
	if offset > lineEnd {
		return lineEnd
	}
	return offset
}

func byteOffsetForProtocolPosition(source string, pos Position) int {
	lineStart, lineEnd := lineByteBounds(source, clampZeroBased(pos.Line))
	want := clampZeroBased(pos.Character)
	units := 0
	for offset := lineStart; offset < lineEnd; {
		r, size := utf8.DecodeRuneInString(source[offset:lineEnd])
		if size <= 0 {
			size = 1
		}
		width := utf16.RuneLen(r)
		if units+width > want {
			return offset
		}
		if units+width == want {
			return offset + size
		}
		units += width
		offset += size
	}
	return lineEnd
}

func protocolPositionForByteOffset(source string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	line := 0
	lineStart := 0
	for i := 0; i < offset; i++ {
		if source[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	lineEnd := lineEndFromStart(source, lineStart)
	if lineEnd > 0 && lineEnd <= len(source) && source[lineEnd-1] == '\r' {
		lineEnd--
	}
	if offset > lineEnd {
		offset = lineEnd
	}
	character := 0
	for i := lineStart; i < offset; {
		r, size := utf8.DecodeRuneInString(source[i:lineEnd])
		if size <= 0 || i+size > offset {
			break
		}
		character += utf16.RuneLen(r)
		i += size
	}
	return Position{Line: line, Character: character}
}

func protocolRangeForByteRange(source string, r byteRange) Range {
	if r.end < r.start {
		r.end = r.start
	}
	return Range{
		Start: protocolPositionForByteOffset(source, r.start),
		End:   protocolPositionForByteOffset(source, r.end),
	}
}

func lineByteBounds(source string, line int) (int, int) {
	if line <= 0 {
		end := lineEndFromStart(source, 0)
		return 0, trimCRLineEnd(source, end)
	}
	currentLine := 0
	lineStart := 0
	for i := 0; i < len(source); i++ {
		if source[i] != '\n' {
			continue
		}
		currentLine++
		lineStart = i + 1
		if currentLine == line {
			end := lineEndFromStart(source, lineStart)
			return lineStart, trimCRLineEnd(source, end)
		}
	}
	if currentLine == line {
		end := lineEndFromStart(source, lineStart)
		return lineStart, trimCRLineEnd(source, end)
	}
	return len(source), len(source)
}

func lineEndFromStart(source string, start int) int {
	if start < 0 {
		start = 0
	}
	if start > len(source) {
		return len(source)
	}
	if next := strings.IndexByte(source[start:], '\n'); next >= 0 {
		return start + next
	}
	return len(source)
}

func trimCRLineEnd(source string, end int) int {
	if end > 0 && end <= len(source) && source[end-1] == '\r' {
		return end - 1
	}
	return end
}
