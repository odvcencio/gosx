package server

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/internal/emoji"
)

//go:embed emoji_complete.js
var emojiCompleteRuntime string

//go:embed emoji_codes.json
var emojiCodesJSON []byte

var (
	emojiCodesJSONOnce sync.Once
	emojiCodesJSONAll  []byte
	emojiCodesGzipOnce sync.Once
	emojiCodesGzip     []byte
)

func emojiCodesJSONBytes() []byte {
	emojiCodesJSONOnce.Do(func() {
		var rows [][]string
		if err := json.Unmarshal(emojiCodesJSON, &rows); err != nil {
			emojiCodesJSONAll = emojiCodesJSON
			return
		}

		table := make(map[string]string, len(rows)+len(emoji.SlackishAliases))
		for _, row := range rows {
			if len(row) >= 2 {
				table[row[0]] = row[1]
			}
		}
		emoji.ApplyAliases(table)

		rows = rows[:0]
		for name, value := range table {
			rows = append(rows, []string{name, value})
		}
		sort.Slice(rows, func(i, j int) bool {
			return rows[i][0] < rows[j][0]
		})

		data, err := json.Marshal(rows)
		if err != nil {
			emojiCodesJSONAll = emojiCodesJSON
			return
		}
		emojiCodesJSONAll = data
	})
	return emojiCodesJSONAll
}

func emojiCodesGzipped() []byte {
	emojiCodesGzipOnce.Do(func() {
		var buf bytes.Buffer
		w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		w.Write(emojiCodesJSONBytes())
		w.Close()
		emojiCodesGzip = buf.Bytes()
	})
	return emojiCodesGzip
}

// EmojiCompleteScript returns an inline script that enables emoji shortcode
// autocomplete on any textarea or input with the `data-gosx-emoji-complete`
// attribute. Type `:` followed by a shortcode character to trigger suggestions.
//
// Add to a page via ctx.AddHead(server.EmojiCompleteScript()).
func EmojiCompleteScript() gosx.Node {
	return gosx.RawHTML(`<script data-gosx-emoji-complete-runtime="true">` + emojiCompleteRuntime + `</script>`)
}

// EmojiCodesHandler serves the emoji shortcode lookup table as JSON.
// Serves pre-compressed gzip when the client supports it (~28KB instead of ~162KB).
func EmojiCodesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			w.Write(emojiCodesGzipped())
			return
		}
		w.Write(emojiCodesJSONBytes())
	})
}
