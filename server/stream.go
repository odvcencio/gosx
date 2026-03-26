package server

import (
	"fmt"
	"net/http"
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
		fmt.Fprint(w, html)
		return
	}

	prefix, suffix, marked := splitStreamTail(html)
	w.WriteHeader(status)
	if marked {
		fmt.Fprint(w, prefix)
	} else {
		fmt.Fprint(w, html)
	}

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}

	streamDeferredChunks(w, res.Deferred, flusher)

	if marked {
		fmt.Fprint(w, suffix)
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
		block := block
		go func() {
			chunks <- deferredChunk{
				slotID: block.id,
				html:   resolveDeferredHTML(block),
			}
		}()
	}

	for range blocks {
		chunk := <-chunks
		fmt.Fprint(w, renderDeferredChunk(chunk.slotID, chunk.html))
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
	return fmt.Sprintf(
		`<template id=%q data-gosx-stream-template>%s</template><script>(function(){var slot=document.getElementById(%q);var tpl=document.getElementById(%q);if(!slot||!tpl){return;}slot.replaceWith(tpl.content.cloneNode(true));tpl.remove();})();</script>`,
		templateID,
		html,
		slotID,
		templateID,
	)
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
