package server

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"mime"
	"net/http"
	"strings"
	"sync"
)

const maxCachedHTMLBody = 1 << 20

type compressedBodyKey struct {
	etag, encoding string
	length         int
}
type compressedBodyEntry struct {
	key    compressedBodyKey
	digest [32]byte
	body   []byte
	size   int
}
type compressedBodyCache struct {
	mu                          sync.Mutex
	entries                     map[compressedBodyKey]*list.Element
	order                       list.List
	bytes, maxBytes, maxEntries int
}

func newCompressedBodyCache(size, entries int) *compressedBodyCache {
	return &compressedBodyCache{entries: make(map[compressedBodyKey]*list.Element), maxBytes: size, maxEntries: entries}
}
func (c *compressedBodyCache) get(key compressedBodyKey, digest [32]byte) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil {
		value := entry.Value.(compressedBodyEntry)
		if value.digest == digest {
			c.order.MoveToFront(entry)
			return value.body, true
		}
	}
	return nil, false
}
func (c *compressedBodyCache) put(key compressedBodyKey, digest [32]byte, body []byte) {
	if len(body)+len(key.etag)+256 > c.maxBytes || c.maxEntries <= 0 {
		return
	}
	body = bytes.Clone(body)
	key.etag = strings.Clone(key.etag)
	size := cap(body) + len(key.etag) + 256
	if size > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	remove := func(entry *list.Element) {
		value := entry.Value.(compressedBodyEntry)
		c.bytes -= value.size
		delete(c.entries, value.key)
		c.order.Remove(entry)
	}
	if entry := c.entries[key]; entry != nil {
		remove(entry)
	}
	for c.order.Len() >= c.maxEntries || c.bytes+size > c.maxBytes {
		remove(c.order.Back())
	}
	c.entries[key] = c.order.PushFront(compressedBodyEntry{key, digest, body, size})
	c.bytes += size
}

func shareableCompressedHTML(h http.Header) bool {
	media, _, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil || media != "text/html" {
		return false
	}
	etag := h.Get("ETag")
	if len(etag) < 2 || etag[0] != '"' || etag[len(etag)-1] != '"' || len(h.Values("Set-Cookie")) > 0 || h.Get("Content-Encoding") != "" || h.Get("Content-Range") != "" || !strings.HasPrefix(strings.ToLower(h.Get("Content-Type")), "text/html") {
		return false
	}
	for _, name := range []string{"Content-Security-Policy", "Content-Security-Policy-Report-Only"} {
		if strings.Contains(strings.ToLower(h.Get(name)), "nonce-") {
			return false
		}
	}
	shared := false
	for _, header := range h.Values("Cache-Control") {
		for _, part := range strings.Split(header, ",") {
			name, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(part)), "=")
			switch name {
			case "private", "no-store":
				return false
			case "public", "s-maxage":
				shared = true
			}
		}
	}
	return shared
}

func hasNonceAttribute(body []byte) bool {
	for i := 0; i+5 < len(body); i++ {
		if body[i] != 'n' && body[i] != 'N' {
			continue
		}
		if i > 0 && body[i-1] != ' ' && body[i-1] != '\t' && body[i-1] != '\n' && body[i-1] != '\r' && body[i-1] != '\f' {
			continue
		}
		if !bytes.EqualFold(body[i:i+5], []byte("nonce")) {
			continue
		}
		j := i + 5
		for j < len(body) && (body[j] == ' ' || body[j] == '\t' || body[j] == '\n' || body[j] == '\r' || body[j] == '\f') {
			j++
		}
		if j < len(body) && body[j] == '=' {
			return true
		}
	}
	return false
}

// Finish sees the complete body. Flush and oversized bodies use the normal
// streaming encoder and never enter this cache.
func (w *compressionWriter) finishShareable() bool {
	if w.started || !w.cacheCandidate || len(w.buffer) < 1024 || hasNonceAttribute(w.buffer) || len(w.Header().Values("Set-Cookie")) > 0 {
		return false
	}
	key := compressedBodyKey{w.headers.Get("ETag"), w.encoding, len(w.buffer)}
	digest := sha256.Sum256(w.buffer)
	if body, ok := w.bodyCache.get(key, digest); ok {
		w.cacheReplay = body
		_ = w.start(true)
		return true
	}
	var encoded bytes.Buffer
	w.cacheCapture = &encoded
	if err := w.start(true); err != nil {
		w.cacheCapture = nil
		_ = w.closeCompressor()
		return true
	}
	err := w.closeCompressor()
	w.cacheCapture = nil
	if err == nil {
		w.bodyCache.put(key, digest, encoded.Bytes())
	}
	return true
}

func disableCompressedBodyCache(writer http.ResponseWriter) {
	for writer != nil {
		if cacheWriter, ok := writer.(interface{ disableBodyCache() }); ok {
			cacheWriter.disableBodyCache()
			return
		}
		wrapper, ok := writer.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return
		}
		writer = wrapper.Unwrap()
	}
}
func (w *compressionWriter) disableBodyCache() { w.shareRequest = false; w.cacheCandidate = false }
