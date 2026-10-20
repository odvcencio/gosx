//go:build !js || !wasm

package highlight

import (
	"container/list"
	"crypto/sha256"
	"sync"
)

// Cache only rendered strings: parser trees and source buffers remain request-local.
var renderedHTML = newHTMLCache(512, 8<<20)

type htmlCacheKey struct {
	language string
	digest   [32]byte
}

type htmlCacheEntry struct {
	key    htmlCacheKey
	output string
}

type htmlCache struct {
	mu                          sync.Mutex
	entries                     map[htmlCacheKey]*list.Element
	order                       *list.List
	bytes, maxEntries, maxBytes int
}

func newHTMLCache(entries, bytes int) *htmlCache {
	return &htmlCache{entries: make(map[htmlCacheKey]*list.Element), order: list.New(), maxEntries: entries, maxBytes: bytes}
}

func (c *htmlCache) get(key htmlCacheKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil {
		c.order.MoveToFront(entry)
		return entry.Value.(htmlCacheEntry).output, true
	}
	return "", false
}

func (c *htmlCache) put(key htmlCacheKey, output string) {
	// Include a fixed allowance for the key, list element and map entry.
	size := len(output) + 128
	if size > c.maxBytes || c.maxEntries <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry := c.entries[key]; entry != nil {
		c.order.MoveToFront(entry)
		return
	}
	for c.order.Len() >= c.maxEntries || c.bytes+size > c.maxBytes {
		entry := c.order.Back()
		value := entry.Value.(htmlCacheEntry)
		c.bytes -= len(value.output) + 128
		delete(c.entries, value.key)
		c.order.Remove(entry)
	}
	c.entries[key] = c.order.PushFront(htmlCacheEntry{key, output})
	c.bytes += size
}

func cachedHTML(lang, source string) string {
	if source == "" {
		return ""
	}
	if len(source) > renderedHTML.maxBytes {
		return renderHTML(lang, source)
	}
	key := htmlCacheKey{lang, sha256.Sum256([]byte(source))}
	if output, ok := renderedHTML.get(key); ok {
		return output
	}
	output := renderHTML(lang, source)
	renderedHTML.put(key, output)
	return output
}
