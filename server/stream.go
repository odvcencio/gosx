package server

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"m31labs.dev/gosx"
)

const streamTailMarker = "<!--gosx-stream-tail-->"

// HTMLResponse describes an HTML page response, optionally including deferred
// fragments that should be streamed into place after the initial shell flushes.
type HTMLResponse struct {
	Status   int
	Headers  http.Header
	Node     gosx.Node
	Deferred *DeferredRegistry

	// Request, Cache and Revalidator let WriteHTML derive the ETag from the
	// rendered body.
	//
	// WHY they live here: the validator must describe the bytes the client
	// receives, and only WriteHTML holds those bytes. Callers used to apply the
	// cache headers before the render, so effectiveETag could only hash the
	// request. Two different bodies then shared one validator, and a
	// conditional request with a stale tag won a 304 with an empty body.
	//
	// Leave all three zero to skip the cache handling.
	Request     *http.Request
	Cache       *CacheState
	Revalidator *Revalidator

	// CacheDigestExclude lists per-request strings that the body embeds but
	// that are not part of the resource representation, such as the
	// Content-Security-Policy script nonce. WriteHTML removes them before it
	// hashes the body. Without that step the digest changes on every request
	// and no conditional request can ever match.
	//
	// WriteHTML always removes the request ID, which it reads from Request.
	CacheDigestExclude []string

	// Nonce is the per-request Content-Security-Policy script nonce. WriteHTML
	// attaches it to the inline script in every streamed chunk, so ctx.Defer and
	// ctx.Suspense still work under a nonce-based policy.
	Nonce string
}

// WriteHTML writes an HTML response and, when possible, streams deferred
// fragments after the initial shell has been flushed.
//
// It renders the node before it touches the response headers, so the ETag can
// describe the body. A conditional request that matches the validator returns
// 304 and no body. That costs one render, which is the price of an honest
// content-derived validator: the server cannot know the body is unchanged
// without producing it.
func WriteHTML(w http.ResponseWriter, res HTMLResponse) {
	status := res.Status
	if status == 0 {
		status = http.StatusOK
	}

	html := gosx.RenderHTML(res.Node)
	streaming := res.Deferred != nil && res.Deferred.HasDeferred()

	if res.Cache.SharedCacheable() {
		// A shared cache stores one body and replays it to every client, so the
		// response must carry no per-request value. The body already dropped the
		// request ID and the nonce; align the policy header with it.
		applySharedCacheSecurityHeaders(w, res.Request)
		res.Nonce = ""
	}

	// Run before copyHeaders, which snapshots res.Headers, and before the
	// Content-Type set, which a 304 must not carry.
	if res.Cache.shouldApply() {
		// A streamed response has no complete body yet, so report no body and
		// keep the request-derived validator. See ApplyCacheHeadersForBody.
		var body []byte
		if !streaming {
			body = res.cacheDigestBody(html)
		}
		if ApplyCacheHeadersForBody(res.Request, res.Headers, status, res.Cache, res.Revalidator, body) {
			WriteNotModified(w, res.Headers)
			return
		}
	}

	copyHeaders(w.Header(), res.Headers)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if !streaming {
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

	streamDeferredChunks(w, res.Deferred, flusher, res.Nonce)

	if marked {
		io.WriteString(w, suffix)
	}
}

// cacheDigestBody returns the bytes that describe the resource representation.
//
// WHY it strips values: a GoSX document embeds the per-request ID in its
// document contract script, and it can embed a per-request
// Content-Security-Policy nonce on every script tag. Both change on every
// request, so a digest over the raw body would never repeat and no conditional
// request could ever match. Neither value describes the resource: a shared cache
// that replayed one client's request ID or nonce to another client would be
// wrong. Remove them, then hash what is left.
func (res HTMLResponse) cacheDigestBody(html string) []byte {
	if id := RequestID(res.Request); id != "" {
		html = strings.ReplaceAll(html, id, "")
	}
	for _, value := range res.CacheDigestExclude {
		if value == "" {
			continue
		}
		html = strings.ReplaceAll(html, value, "")
	}
	return []byte(html)
}

func splitStreamTail(html string) (string, string, bool) {
	before, after, ok := strings.Cut(html, streamTailMarker)
	if !ok {
		return html, "", false
	}
	return before, after, true
}

func streamDeferredChunks(w http.ResponseWriter, registry *DeferredRegistry, flusher http.Flusher, nonce string) {
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
		io.WriteString(w, renderDeferredChunk(chunk.slotID, chunk.html, nonce))
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

func renderDeferredChunk(slotID, html, nonce string) string {
	templateID := slotID + "-content"
	// The chunk carries the same nonce the policy header names. Without it a
	// nonce-based Content-Security-Policy blocks the script and the placeholder
	// never resolves.
	scriptNonce := nonceAttr(nonce)
	// Pre-size to roughly the static template (~225 bytes) plus body html.
	var b strings.Builder
	b.Grow(256 + len(html) + len(scriptNonce) + 4*len(templateID) + 2*len(slotID))
	b.WriteString(`<template id=`)
	b.WriteString(strconv.Quote(templateID))
	b.WriteString(` data-gosx-stream-template data-gosx-stream-target=`)
	b.WriteString(strconv.Quote(slotID))
	b.WriteString(`>`)
	b.WriteString(html)
	b.WriteString(`</template><script`)
	b.WriteString(scriptNonce)
	b.WriteString(`>(function(){var slot=document.getElementById(`)
	b.WriteString(strconv.Quote(slotID))
	b.WriteString(`);var tpl=document.getElementById(`)
	b.WriteString(strconv.Quote(templateID))
	b.WriteString(`);if(!slot||!tpl){return;}var content=tpl.content.cloneNode(true);var replaced=false;if(window.__gosx&&window.__gosx.dom&&typeof window.__gosx.dom.replaceFragment==="function"){replaced=window.__gosx.dom.replaceFragment(slot,content)===true;}if(!replaced){slot.replaceWith(content);}tpl.remove();})();</script>`)
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
