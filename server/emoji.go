package server

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"net/http"
	"strings"
	"sync"

	"github.com/odvcencio/gosx"
)

//go:embed emoji_complete.js
var emojiCompleteRuntime string

//go:embed emoji_codes.json
var emojiCodesJSON []byte

var (
	emojiCodesGzipOnce sync.Once
	emojiCodesGzip     []byte
)

func emojiCodesGzipped() []byte {
	emojiCodesGzipOnce.Do(func() {
		var buf bytes.Buffer
		w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		w.Write(emojiCodesJSON)
		w.Close()
		emojiCodesGzip = buf.Bytes()
	})
	return emojiCodesGzip
}

// EmojiCompleteScript returns an inline script that enables emoji shortcode
// autocomplete on any textarea or input with the `data-gosx-emoji-complete`
// attribute. Type `:` followed by 2+ characters to trigger suggestions.
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
		w.Write(emojiCodesJSON)
	})
}
