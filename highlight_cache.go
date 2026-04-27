//go:build !js || !wasm

package mdpp

import (
	"container/list"
	"hash/fnv"
	"sync"
)

// highlightCacheCap bounds the number of distinct (language, source) → HTML
// results held in the process-wide highlight cache. Preview workloads render
// the same fence repeatedly as the author edits text outside it; caching the
// rendered HTML lets those redraws skip the parse entirely.
const highlightCacheCap = 256

// hlCacheEntry stores one cached highlight result. ok mirrors the second
// return of highlightCodeImpl so callers can distinguish "we highlighted"
// from "we already tried and failed for this language".
type hlCacheEntry struct {
	key  uint64
	html string
	ok   bool
}

// hlCache is a small LRU keyed by fnv64a(language || 0 || source).
// It is safe for concurrent use.
type hlCache struct {
	mu    sync.Mutex
	cap   int
	items map[uint64]*list.Element
	order *list.List
}

func newHLCache(cap int) *hlCache {
	return &hlCache{
		cap:   cap,
		items: make(map[uint64]*list.Element, cap),
		order: list.New(),
	}
}

func (c *hlCache) get(key uint64) (hlCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.items[key]
	if !ok {
		return hlCacheEntry{}, false
	}
	c.order.MoveToFront(elem)
	return elem.Value.(hlCacheEntry), true
}

func (c *hlCache) put(entry hlCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[entry.key]; ok {
		elem.Value = entry
		c.order.MoveToFront(elem)
		return
	}
	elem := c.order.PushFront(entry)
	c.items[entry.key] = elem
	for c.order.Len() > c.cap {
		tail := c.order.Back()
		if tail == nil {
			break
		}
		c.order.Remove(tail)
		delete(c.items, tail.Value.(hlCacheEntry).key)
	}
}

// highlightHTMLCache is the package-global cache used by highlightCodeImpl.
var highlightHTMLCache = newHLCache(highlightCacheCap)

// hlCacheKey derives the cache key for a (language, source) pair.
// The 0 byte separator prevents "goX" + "Y" colliding with "go" + "XY".
func hlCacheKey(language, source string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(language))
	h.Write([]byte{0})
	h.Write([]byte(source))
	return h.Sum64()
}
