package mdpp

import (
	"hash/fnv"
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// Parser holds per-document state that survives across ParseIncremental
// calls, enabling reuse of expensive work (paragraph inline reparse,
// container body chunk parses) for unchanged regions.
//
// A Parser is safe for sequential use by a single caller; concurrent Parse
// calls on the same Parser are not supported. Typical usage is to retain
// one Parser per open document in an LSP-like context.
type Parser struct {
	mu sync.Mutex

	// prevTree is the tree-sitter parse tree retained from the most recent
	// primary-path parse. It is edited in-place (via InputEdit) and handed
	// to tree-sitter for incremental reparse. Nil when no primary parse has
	// completed (or the last parse took a fallback path).
	prevTree *gotreesitter.Tree

	// cache holds reusable AST subtrees keyed by (node type, content).
	cache *parseCache

	// lastHits and lastMisses record the cache effectiveness of the most
	// recent Parse / ParseIncremental call.
	lastHits, lastMisses int
}

// NewParser returns a fresh Parser with an empty cache and no retained tree.
func NewParser() *Parser {
	return &Parser{cache: newParseCache()}
}

// LastStats returns (cacheHits, cacheMisses) recorded by the most recent
// Parse or ParseIncremental call. Useful for observability and tests.
func (p *Parser) LastStats() (int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastHits, p.lastMisses
}

// Parse runs a full parse, replacing any retained incremental state.
func (p *Parser) Parse(source []byte) (*Document, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Release any prior tree.
	if p.prevTree != nil {
		p.prevTree.Release()
		p.prevTree = nil
	}
	var doc *Document
	var tree *gotreesitter.Tree
	func() {
		defer func() {
			if r := recover(); r != nil {
				src := normalizeLineEndings(append([]byte(nil), source...))
				root := &Node{
					Type:     NodeDocument,
					Range:    sourceRange(src, 0, len(src)),
					Children: []*Node{textNodeRange(string(src), sourceRange(src, 0, len(src)))},
				}
				doc = &Document{
					Root:   root,
					Source: src,
					diagnostics: []Diagnostic{{
						Code:     "MDPP-PARSE-000",
						Severity: SeverityError,
						Message:  "parser recovered from panic",
						Range:    sourceRange(src, 0, len(src)),
					}},
				}
				tree = nil
			}
		}()
		ctx := &parseCtx{cache: p.cache}
		doc, tree = parseDocumentRetainTreeCtx(source, nil, ctx)
		p.lastHits, p.lastMisses = ctx.stats()
	}()
	p.prevTree = tree
	return doc, nil
}

// ParseIncremental applies edit to the retained tree (if any) and runs an
// incremental parse, reusing cached AST subtrees for unchanged content. If
// no tree is retained (first parse or prior fallback), it runs a full parse.
func (p *Parser) ParseIncremental(source []byte, edit gotreesitter.InputEdit) (*Document, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var doc *Document
	var tree *gotreesitter.Tree
	prev := p.prevTree
	p.prevTree = nil
	func() {
		defer func() {
			if r := recover(); r != nil {
				src := normalizeLineEndings(append([]byte(nil), source...))
				root := &Node{
					Type:     NodeDocument,
					Range:    sourceRange(src, 0, len(src)),
					Children: []*Node{textNodeRange(string(src), sourceRange(src, 0, len(src)))},
				}
				doc = &Document{
					Root:   root,
					Source: src,
					diagnostics: []Diagnostic{{
						Code:     "MDPP-PARSE-000",
						Severity: SeverityError,
						Message:  "parser recovered from panic",
						Range:    sourceRange(src, 0, len(src)),
					}},
				}
				if prev != nil {
					prev.Release()
					prev = nil
				}
				tree = nil
			}
		}()
		if prev != nil {
			prev.Edit(edit)
		}
		ctx := &parseCtx{cache: p.cache}
		doc, tree = parseDocumentRetainTreeCtx(source, prev, ctx)
		p.lastHits, p.lastMisses = ctx.stats()
	}()
	p.prevTree = tree
	return doc, nil
}

// ApplyEdit applies an InputEdit to the retained tree without reparsing. Use
// this when multiple edits must be applied before a single incremental parse.
// ParseIncremental already applies the passed edit internally, so use this
// only for secondary edits that precede a ParseIncremental call.
func (p *Parser) ApplyEdit(edit gotreesitter.InputEdit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prevTree != nil {
		p.prevTree.Edit(edit)
	}
}

// Close releases any retained tree-sitter state.
func (p *Parser) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prevTree != nil {
		p.prevTree.Release()
		p.prevTree = nil
	}
	if p.cache != nil {
		p.cache.reset()
	}
}

// parseCtx is the per-parse-invocation context threaded through the parse
// pipeline. It carries the cache and tracks which cache entries have been
// consumed during the current parse so stale entries can be evicted
// afterwards.
type parseCtx struct {
	cache *parseCache
	// suppressLooseRecovery prevents recursive loose-block reparsing when
	// a recovery parse itself produces loose fallback text.
	suppressLooseRecovery bool
	// hits counts successful cache reuses for observability / tests.
	hits int
	// misses counts expensive parses that were not reused.
	misses int
	// seen records cache keys touched during the current parse. Keys not
	// present here at the end of the parse represent stale paragraphs /
	// chunks no longer in the document and are evicted to bound memory.
	seen map[cacheKey]struct{}
}

func (c *parseCtx) recordSeen(k cacheKey) {
	if c == nil {
		return
	}
	if c.seen == nil {
		c.seen = make(map[cacheKey]struct{})
	}
	c.seen[k] = struct{}{}
}

// Stats returns (hits, misses) recorded on this context.
func (c *parseCtx) stats() (int, int) {
	if c == nil {
		return 0, 0
	}
	return c.hits, c.misses
}

// cacheKey identifies a reusable cached AST node by its role and content.
// role encodes the node category (paragraph / heading / container-chunk /
// body-doc) so a text that could plausibly parse as more than one role
// (e.g. "# foo" as a heading vs paragraph) cannot alias.
type cacheKey struct {
	role uint8
	hash uint64
	// content length disambiguates hash collisions (cheap check).
	size uint32
}

const (
	roleParagraph uint8 = 1 + iota
	roleHeading
	roleChunk
	roleBodyDoc
)

// parseCache is a concurrency-safe hash-keyed cache of reusable AST subtrees.
// Values are stored with their original byte offsets so reusers can shift
// ranges by a delta when reinstalling the subtree in a new document.
type parseCache struct {
	mu      sync.Mutex
	entries map[cacheKey]*cacheEntry
}

type cacheEntry struct {
	// root is the cached AST subtree (for paragraph/heading role) or a
	// synthetic document root whose children are the cached block list
	// (for chunk/body-doc role).
	root *Node
	// children is set for chunk/body-doc entries: the flat child list that
	// should be emitted into the parent.
	children []*Node
	// baseStart is the byte offset in the source that produced this entry
	// where the cached subtree was originally rooted. Reusers compute
	// delta = newStart - baseStart and shift ranges accordingly.
	baseStart int
}

func newParseCache() *parseCache {
	return &parseCache{entries: make(map[cacheKey]*cacheEntry)}
}

func (c *parseCache) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries = make(map[cacheKey]*cacheEntry)
	c.mu.Unlock()
}

func (c *parseCache) get(k cacheKey) (*cacheEntry, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	e, ok := c.entries[k]
	c.mu.Unlock()
	return e, ok
}

func (c *parseCache) put(k cacheKey, e *cacheEntry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.entries[k] = e
	c.mu.Unlock()
}

// pruneNotIn removes entries whose keys are not in keep. Called at the end
// of a parse to bound memory to live content.
func (c *parseCache) pruneNotIn(keep map[cacheKey]struct{}) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) == 0 {
		return
	}
	// Hard cap: if the keep set is empty and the cache is large, just clear.
	if len(keep) == 0 && len(c.entries) > 1024 {
		c.entries = make(map[cacheKey]*cacheEntry)
		return
	}
	for k := range c.entries {
		if _, ok := keep[k]; !ok {
			delete(c.entries, k)
		}
	}
}

func hashContent(role uint8, content []byte) cacheKey {
	h := fnv.New64a()
	h.Write([]byte{role})
	h.Write(content)
	return cacheKey{role: role, hash: h.Sum64(), size: uint32(len(content))}
}

func hashString(role uint8, s string) cacheKey {
	h := fnv.New64a()
	h.Write([]byte{role})
	h.Write([]byte(s))
	return cacheKey{role: role, hash: h.Sum64(), size: uint32(len(s))}
}

// cloneAndShift returns a deep copy of root whose byte ranges have been
// shifted by delta bytes and whose line/col are recomputed from source.
// It leaves Literal, Attrs (shallow copy), and Type intact.
func cloneAndShift(root *Node, delta int, source []byte) *Node {
	if root == nil {
		return nil
	}
	n := &Node{
		Type:    root.Type,
		Literal: root.Literal,
	}
	if root.Attrs != nil {
		n.Attrs = make(map[string]string, len(root.Attrs))
		for k, v := range root.Attrs {
			n.Attrs[k] = v
		}
	}
	n.Range = shiftRange(root.Range, delta, source)
	if len(root.Children) > 0 {
		n.Children = make([]*Node, len(root.Children))
		for i, c := range root.Children {
			n.Children[i] = cloneAndShift(c, delta, source)
		}
	}
	return n
}

// cloneForCache returns a deep copy of n suitable for storing in the parse
// cache. The _ argument is the absolute start byte at the time of parse;
// callers retain it so they can later compute a shift delta on reuse.
func cloneForCache(n *Node, _ int) *Node {
	return cloneAndShift(n, 0, nil)
}

func shiftRange(r Range, delta int, source []byte) Range {
	if r.StartLine == 0 && r.StartByte == 0 && r.EndByte == 0 {
		return r
	}
	newStart := r.StartByte + delta
	newEnd := r.EndByte + delta
	if newStart < 0 {
		newStart = 0
	}
	if newEnd < newStart {
		newEnd = newStart
	}
	if source != nil {
		return sourceRange(source, newStart, newEnd)
	}
	// Without source we can only shift bytes; lines/cols become stale but
	// callers who lack source should re-stamp later via applyTreeRange.
	return Range{
		StartByte: newStart,
		EndByte:   newEnd,
		StartLine: r.StartLine,
		StartCol:  r.StartCol,
		EndLine:   r.EndLine,
		EndCol:    r.EndCol,
	}
}
