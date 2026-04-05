package server

import (
	_ "embed"
	"net/http"

	"github.com/odvcencio/gosx"
)

//go:embed emoji_complete.js
var emojiCompleteRuntime string

//go:embed emoji_codes.json
var emojiCodesJSON []byte

// EmojiCompleteScript returns an inline script that enables emoji shortcode
// autocomplete on any textarea or input with the `data-gosx-emoji-complete`
// attribute. Type `:` followed by 2+ characters to trigger suggestions.
//
// Add to a page via ctx.AddHead(server.EmojiCompleteScript()).
func EmojiCompleteScript() gosx.Node {
	return gosx.RawHTML(`<script data-gosx-emoji-complete-runtime="true">` + emojiCompleteRuntime + `</script>`)
}

// EmojiCodesHandler serves the emoji shortcode lookup table as JSON.
// Mount at /_gosx/emoji-codes.json for the autocomplete runtime to fetch.
func EmojiCodesHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write(emojiCodesJSON)
	})
}
