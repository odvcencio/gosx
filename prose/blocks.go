package prose

import (
	"strconv"
	"strings"
)

// Kind identifies the broad top-level Markdown block shape. It is intentionally
// renderer-agnostic so an application can use it to drive incremental previews,
// streaming responses, or source-aware annotations.
type Kind string

const (
	KindParagraph Kind = "paragraph"
	KindHeading   Kind = "heading"
	KindList      Kind = "list"
	KindQuote     Kind = "quote"
	KindFence     Kind = "fence"
	KindTable     Kind = "table"
	KindThematic  Kind = "thematic_break"
	KindDirective Kind = "directive"
	KindUnknown   Kind = "unknown"
)

// Block is a top-level source range. Key is stable for the block's position in
// one stream and can be passed to the browser block reconciler. Start and End
// are byte offsets into the original source, with End exclusive. The final
// block is marked incomplete because it is the block most likely to change
// while a stream is arriving.
type Block struct {
	Key        string
	Kind       Kind
	Source     string
	Start      int
	End        int
	Incomplete bool
}

// Split divides Markdown++ source into stable top-level blocks. It is a
// conservative splitter: blank lines end ordinary blocks, fenced blocks stay
// intact, and headings/thematic breaks begin their own block even when the
// source omits a blank line.
func Split(source string) []Block {
	if source == "" {
		return nil
	}

	type line struct {
		start int
		end   int
		text  string
	}

	lines := make([]line, 0, strings.Count(source, "\n")+1)
	for offset := 0; offset < len(source); {
		end := strings.IndexByte(source[offset:], '\n')
		if end < 0 {
			lines = append(lines, line{start: offset, end: len(source), text: source[offset:]})
			break
		}
		end += offset + 1
		lines = append(lines, line{start: offset, end: end, text: source[offset:end]})
		offset = end
	}

	blocks := make([]Block, 0, len(lines))
	blockStart := -1
	blockKind := KindUnknown
	var fenceChar byte
	var fenceLength int
	inFence := false

	finish := func(end int) {
		if blockStart < 0 || end <= blockStart {
			return
		}
		blocks = append(blocks, Block{
			Key:    strconv.Itoa(len(blocks)),
			Kind:   blockKind,
			Source: source[blockStart:end],
			Start:  blockStart,
			End:    end,
		})
		blockStart = -1
		blockKind = KindUnknown
	}

	for _, current := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(current.text, "\n"))
		if inFence {
			if isFenceClose(trimmed, fenceChar, fenceLength) {
				inFence = false
				finish(current.end)
			}
			continue
		}

		if trimmed == "" {
			finish(current.end)
			continue
		}

		if fence, ok := fenceInfo(trimmed); ok {
			finish(current.start)
			blockStart = current.start
			blockKind = KindFence
			fenceChar = fence.char
			fenceLength = fence.length
			inFence = true
			continue
		}

		kind := classify(trimmed)
		if blockStart < 0 {
			blockStart = current.start
			blockKind = kind
		} else if kind == KindHeading || kind == KindThematic || kind == KindDirective {
			finish(current.start)
			blockStart = current.start
			blockKind = kind
		}

		if kind == KindHeading || kind == KindThematic {
			finish(current.end)
		}
	}
	finish(len(source))

	if len(blocks) > 0 {
		blocks[len(blocks)-1].Incomplete = true
	}
	return blocks
}

type fence struct {
	char   byte
	length int
}

func fenceInfo(line string) (fence, bool) {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return fence{}, false
	}
	char := line[0]
	length := 0
	for length < len(line) && line[length] == char {
		length++
	}
	if length < 3 {
		return fence{}, false
	}
	return fence{char: char, length: length}, true
}

func isFenceClose(line string, char byte, length int) bool {
	if len(line) < length || line[0] != char {
		return false
	}
	count := 0
	for count < len(line) && line[count] == char {
		count++
	}
	return count >= length && strings.TrimSpace(line[count:]) == ""
}

func classify(line string) Kind {
	switch {
	case strings.HasPrefix(line, "#") && strings.TrimLeft(line, "#") != "":
		return KindHeading
	case strings.HasPrefix(line, ">"):
		return KindQuote
	case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") || isOrderedItem(line):
		return KindList
	case strings.HasPrefix(line, "|"):
		return KindTable
	case strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]"),
		strings.HasPrefix(line, "::"):
		return KindDirective
	case isThematicBreak(line):
		return KindThematic
	default:
		return KindParagraph
	}
}

func isOrderedItem(line string) bool {
	digit := false
	for _, r := range line {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case digit && (r == '.' || r == ')'):
			return strings.HasPrefix(line[strings.IndexRune(line, r)+1:], " ")
		default:
			return false
		}
	}
	return false
}

func isThematicBreak(line string) bool {
	compact := strings.NewReplacer(" ", "", "\t", "").Replace(line)
	if len(compact) < 3 {
		return false
	}
	for _, candidate := range []string{"---", "***", "___"} {
		if strings.Trim(compact, candidate) == "" && strings.HasPrefix(compact, candidate[:1]) {
			return true
		}
	}
	return false
}
