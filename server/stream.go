package server

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/odvcencio/gosx"
)

const streamTailMarker = "<!--gosx-stream-tail-->"

// HTMLResponse describes an HTML page response, optionally including deferred
// fragments that should be streamed into place after the initial shell flushes.
type HTMLResponse struct {
	Status   int
	Headers  http.Header
	Node     gosx.Node
	Deferred *DeferredRegistry
}

// WriteHTML writes an HTML response and, when possible, streams deferred
// fragments after the initial shell has been flushed.
func WriteHTML(w http.ResponseWriter, res HTMLResponse) {
	copyHeaders(w.Header(), res.Headers)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	status := res.Status
	if status == 0 {
		status = http.StatusOK
	}

	html := gosx.RenderHTML(res.Node)
	if res.Deferred == nil || !res.Deferred.HasDeferred() {
		w.WriteHeader(status)
		io.WriteString(w, html)
		return
	}

	prefix, suffix, marked := splitStreamTail(html)
	w.WriteHeader(status)
	if marked {
		io.WriteString(w, prefix)
	} else {
		io.WriteString(w, html)
	}

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	streamDeferredChunks(w, res.Deferred, flusher)

	if marked {
		io.WriteString(w, suffix)
	}
}

func splitStreamTail(html string) (string, string, bool) {
	before, after, ok := strings.Cut(html, streamTailMarker)
	if !ok {
		return html, "", false
	}
	return before, after, true
}

func streamDeferredChunks(w http.ResponseWriter, registry *DeferredRegistry, flusher http.Flusher) {
	blocks := registry.snapshot()
	if len(blocks) == 0 {
		return
	}

	type deferredChunk struct {
		slotID string
		html   string
	}

	chunks := make(chan deferredChunk, len(blocks))
	for _, block := range blocks {
		go func() {
			chunks <- deferredChunk{
				slotID: block.id,
				html:   resolveDeferredHTML(block),
			}
		}()
	}

	for range blocks {
		chunk := <-chunks
		io.WriteString(w, renderDeferredChunk(chunk.slotID, chunk.html))
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func resolveDeferredHTML(block deferredBlock) string {
	if block.resolve == nil {
		return ""
	}

	var node gosx.Node
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				node = defaultDeferredError(panicError(recovered))
			}
		}()

		resolved, err := block.resolve()
		if err != nil {
			node = defaultDeferredError(err)
			return
		}
		node = resolved
	}()

	return gosx.RenderHTML(node)
}

func renderDeferredChunk(slotID, html string) string {
	templateID := slotID + "-content"
	// Pre-size to roughly the static template (~225 bytes) plus body html.
	var b strings.Builder
	b.Grow(256 + len(html) + 4*len(templateID) + 2*len(slotID))
	b.WriteString(`<template id=`)
	b.WriteString(strconv.Quote(templateID))
	b.WriteString(` data-gosx-stream-template>`)
	b.WriteString(html)
	b.WriteString(`</template><script>(function(){var slot=document.getElementById(`)
	b.WriteString(strconv.Quote(slotID))
	b.WriteString(`);var tpl=document.getElementById(`)
	b.WriteString(strconv.Quote(templateID))
	b.WriteString(`);if(!slot||!tpl){return;}slot.replaceWith(tpl.content.cloneNode(true));tpl.remove();})();</script>`)
	return b.String()
}

func defaultDeferredError(err error) gosx.Node {
	message := "The server could not finish this section."
	if err != nil && err.Error() != "" {
		message = err.Error()
	}
	return gosx.El("div",
		gosx.Attrs(
			gosx.Attr("data-gosx-stream-error", "true"),
		),
		gosx.Text(message),
	)
}
