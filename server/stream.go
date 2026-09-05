package server

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"m31labs.dev/gosx"
)

const streamTailMarker = "<!--gosx-stream-tail-->"

// A permit remains held until resolution/rendering returns, even when the
// originating request has ended. Noncooperative legacy callbacks therefore
// cannot escape accounting by causing clients to abandon and retry requests.
var deferredAdmission = make(chan struct{}, 128)

// Internal renders (currently ISR refresh) must not mistake a 200 shell or a
// completed error fallback for a healthy artifact. Only the response owner
// accesses this state; resolver workers never receive or update it directly.
type streamOutcomeKey struct{}
type streamOutcome struct{ err error }

func recordStreamFailure(ctx context.Context, err error) {
	if outcome, _ := ctx.Value(streamOutcomeKey{}).(*streamOutcome); outcome != nil && outcome.err == nil {
		outcome.err = err
	}
}

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
// Non-streamed responses render before headers so automatic ETags describe
// the body. Streams have no complete representation at header time and receive
// no automatic ETag; explicit SetETag/SetLastModified versions remain owned by
// the caller. A matching explicit conditional request or HEAD launches no
// deferred callbacks. StreamOptions bounds callback admission and lifetime;
// custom writers must cooperate with cancellation/deadlines to unblock output.
func WriteHTML(w http.ResponseWriter, res HTMLResponse) {
	ctx := context.Background()
	if res.Request != nil {
		ctx = res.Request.Context()
	}
	if ctx.Err() != nil {
		recordStreamFailure(ctx, ctx.Err())
		return
	}
	status := res.Status
	if status == 0 {
		status = http.StatusOK
	}

	renderContext := gosx.RenderContext{RequestTarget: documentContextPath(res.Request)}
	html := gosx.RenderHTMLWithContext(res.Node, renderContext)
	streaming := res.Deferred != nil && res.Deferred.HasDeferred()
	stripNonce := ""

	if res.Cache.SharedCacheable() {
		// Handler and layout nodes can capture the request nonce before they mark
		// the response shared-cacheable. Remove that exact rendered attribute
		// from the completed document before hashing or writing it; the remaining
		// inline scripts are governed by the nonce-free, fail-closed shared CSP.
		html = stripResponseNonceAttribute(html, res.Nonce)
		// A shared cache stores one body and replays it to every client. Align the
		// policy header with the now nonce-free body.
		applySharedCacheSecurityHeaders(w, res.Request)
		stripNonce = res.Nonce
		res.Nonce = ""
	}

	// Run before copyHeaders, which snapshots res.Headers, and before the
	// Content-Type set, which a 304 must not carry.
	if res.Cache.shouldApply() {
		// A streamed response has no complete representation yet. A nil body
		// suppresses automatic ETags; explicit resource versions remain valid.
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
	if res.Request != nil && res.Request.Method == http.MethodHead {
		w.WriteHeader(status)
		return
	}

	if !streaming {
		w.WriteHeader(status)
		io.WriteString(w, html)
		return
	}

	opts, _ := res.Deferred.stream.normalized() // SetStreamOptions validates writes.
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	stream := deferredStream{
		renderContext: renderContext,
		ctx:           ctx, writer: w, controller: http.NewResponseController(w), options: opts,
		requestID: RequestID(res.Request), nonce: res.Nonce, stripNonce: stripNonce,
	}
	// Real net/http writers support write deadlines. Canceling a stream then
	// interrupts blocked output without moving response writes to a goroutine
	// that could outlive ServeHTTP. Arbitrary custom writers must provide the
	// same cooperative contract; Go cannot preempt their blocked Write method.
	interruptDone := make(chan struct{})
	stopInterrupt := context.AfterFunc(ctx, func() {
		defer close(interruptDone)
		_ = stream.controller.SetWriteDeadline(time.Now())
	})
	defer func() {
		if !stopInterrupt() {
			<-interruptDone
		}
	}()
	if ctx.Err() != nil {
		stream.report("", "cancel", ctx.Err())
		return
	}
	prefix, suffix, marked := splitStreamTail(html)
	w.WriteHeader(status)
	if !marked {
		prefix = html
	}
	if err := stream.write(prefix); err != nil {
		cancel()
		stream.report("", "write", err)
		return
	}
	if err := stream.flush(); err != nil {
		cancel()
		stream.report("", "flush", err)
		return
	}

	if err := stream.run(res.Deferred.snapshot()); err != nil {
		cancel()
		stream.report("", "stream", err)
		return
	}
	if marked {
		if err := stream.write(suffix); err != nil {
			cancel()
			stream.report("", "write-tail", err)
			return
		}
		if err := stream.flush(); err != nil {
			cancel()
			stream.report("", "flush-tail", err)
		}
	}
}

func stripResponseNonceAttribute(markup, nonce string) string {
	if nonce == "" {
		return markup
	}
	nonceAttr := gosx.RenderAttrs(gosx.Attrs(gosx.Attr("nonce", nonce)))
	return strings.ReplaceAll(markup, nonceAttr, "")
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

type deferredStream struct {
	renderContext gosx.RenderContext
	ctx           context.Context
	writer        http.ResponseWriter
	controller    *http.ResponseController
	options       StreamOptions
	requestID     string
	nonce         string
	stripNonce    string
}

func (s *deferredStream) write(markup string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	n, err := io.WriteString(s.writer, markup)
	if err == nil && n != len(markup) {
		err = io.ErrShortWrite
	}
	return err
}

func (s *deferredStream) flush() error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	err := s.controller.Flush()
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}

func (s *deferredStream) report(slotID, operation string, err error) {
	if err == nil {
		return
	}
	recordStreamFailure(s.ctx, err)
	event := StreamError{RequestID: s.requestID, SlotID: slotID, Operation: operation, Err: err}
	if s.options.OnError == nil {
		log.Printf("[gosx] stream request=%q slot=%q operation=%q error=%q", event.RequestID, slotID, operation, err)
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("[gosx] stream observer panic request=%q slot=%q error=%q", s.requestID, slotID, recovered)
		}
	}()
	s.options.OnError(event)
}

type deferredChunk struct {
	slotID string
	html   string
	err    error
	panic  bool
}

func (s *deferredStream) run(blocks []deferredBlock) error {
	chunks := make(chan deferredChunk, s.options.Concurrency)
	next, active := 0, 0
	for next < len(blocks) || active > 0 {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		var admission chan struct{}
		if next < len(blocks) && active < s.options.Concurrency {
			admission = deferredAdmission
		}
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case admission <- struct{}{}:
			if err := s.ctx.Err(); err != nil {
				<-deferredAdmission
				return err
			}
			block := blocks[next]
			ctx := s.ctx
			next++
			active++
			// No goroutine exists for queued work: admission occurs in the
			// response owner. Workers never receive a ResponseWriter.
			go func() {
				defer func() { <-deferredAdmission }()
				chunk := resolveDeferredHTMLWithContext(ctx, block, s.renderContext)
				select {
				case chunks <- chunk:
				case <-ctx.Done():
				}
			}()
		case chunk := <-chunks:
			active--
			if err := s.ctx.Err(); err != nil {
				return err
			}
			operation := "resolve"
			if chunk.panic {
				operation = "panic"
			}
			s.report(chunk.slotID, operation, chunk.err)
			markup := stripResponseNonceAttribute(chunk.html, s.stripNonce)
			if err := s.write(renderDeferredChunk(chunk.slotID, markup, s.nonce)); err != nil {
				return err
			}
			if err := s.flush(); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveDeferredHTML(ctx context.Context, block deferredBlock) deferredChunk {
	return resolveDeferredHTMLWithContext(ctx, block, gosx.RenderContext{})
}

func resolveDeferredHTMLWithContext(ctx context.Context, block deferredBlock, renderContext gosx.RenderContext) (chunk deferredChunk) {
	chunk.slotID = block.id
	defer func() {
		if recovered := recover(); recovered != nil {
			chunk.err = panicError(recovered)
			chunk.panic = true
			// SafeMessage applies to returned errors, never to panics.
			chunk.html = gosx.RenderHTMLWithContext(defaultDeferredError(nil), renderContext)
		}
	}()
	if err := ctx.Err(); err != nil {
		chunk.err = err
		return
	}
	node, err := block.resolve(ctx)
	chunk.err = err
	if err != nil {
		node = defaultDeferredError(err)
	}
	chunk.html = gosx.RenderHTMLWithContext(node, renderContext)
	return
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
	var safe interface{ SafeMessage() string }
	if errors.As(err, &safe) {
		if public := safe.SafeMessage(); public != "" {
			message = public
		}
	}
	return gosx.El("div",
		gosx.Attrs(
			gosx.Attr("data-gosx-stream-error", "true"),
		),
		gosx.Text(message),
	)
}
