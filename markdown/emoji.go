package markdown

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/odvcencio/gosx/internal/emoji"
)

var emojiPattern = regexp.MustCompile(`:([a-z0-9_+-]+):`)

// processEmojiShortcodes replaces :shortcode: with emoji nodes.
func processEmojiShortcodes(root *Node) {
	table := loadEmojiTable()
	processInlinePattern(root, emojiPattern, func(match []string) *Node {
		if len(match) < 2 {
			return textNode(match[0])
		}
		code := match[1]
		char, ok := table[code]
		if !ok {
			return textNode(match[0])
		}
		return &Node{
			Type:    NodeEmoji,
			Literal: char,
			Attrs:   map[string]string{"code": code},
		}
	})
}

var (
	emojiTableOnce sync.Once
	emojiTable     map[string]string
)

func loadEmojiTable() map[string]string {
	emojiTableOnce.Do(func() {
		raw, err := base64.StdEncoding.DecodeString(emojiCompressed)
		if err != nil {
			emojiTable = map[string]string{}
			return
		}
		reader, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			emojiTable = map[string]string{}
			return
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			emojiTable = map[string]string{}
			return
		}
		lines := strings.Split(string(data), "\n")
		emojiTable = make(map[string]string, len(lines))
		for _, line := range lines {
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) == 2 {
				emojiTable[parts[0]] = parts[1]
			}
		}
		emoji.ApplyAliases(emojiTable)
	})
	return emojiTable
}

// emojiCompressed is a gzip+base64 encoded TSV of shortcode→emoji mappings.
// Generated from GitHub gemoji + Unicode Emoji 16.0 standard.
// 3824 shortcodes covering all fully-qualified emoji.
// Compressed: 27771 bytes gzipped from 139379 bytes.
const emojiCompressed = "" +
	"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" +
	"69T9f+MbMwNzIAIA"
