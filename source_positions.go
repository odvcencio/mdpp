package mdpp

import (
	"sort"
	"sync"
	"sync/atomic"
	"unsafe"
)

type sourcePositionIndex struct {
	base      uintptr
	size      uintptr
	lineStart []int
}

type sourcePositionRegistry struct {
	mu     sync.RWMutex
	byBase map[uintptr][]*sourcePositionIndex
	bases  []uintptr
}

var activeSourcePositions = sourcePositionRegistry{byBase: make(map[uintptr][]*sourcePositionIndex)}

// registerSourcePositions builds one line-start table for a parse source and
// makes it available to sourceRange calls on that source and its subslices.
// The scope is removed at parse return so indexes never retain documents.
func registerSourcePositions(source []byte) func() {
	if len(source) == 0 {
		return func() {}
	}
	base := uintptr(unsafe.Pointer(unsafe.SliceData(source)))
	index := &sourcePositionIndex{
		base:      base,
		size:      uintptr(len(source)),
		lineStart: make([]int, 0, len(source)/80+1),
	}
	index.lineStart = append(index.lineStart, 0)
	for offset, b := range source {
		if b == '\n' {
			index.lineStart = append(index.lineStart, offset+1)
		}
	}

	activeSourcePositions.mu.Lock()
	if _, exists := activeSourcePositions.byBase[base]; !exists {
		at := sort.Search(len(activeSourcePositions.bases), func(i int) bool {
			return activeSourcePositions.bases[i] >= base
		})
		activeSourcePositions.bases = append(activeSourcePositions.bases, 0)
		copy(activeSourcePositions.bases[at+1:], activeSourcePositions.bases[at:])
		activeSourcePositions.bases[at] = base
	}
	activeSourcePositions.byBase[base] = append(activeSourcePositions.byBase[base], index)
	activeSourcePositions.mu.Unlock()

	var once atomic.Bool
	return func() {
		if !once.CompareAndSwap(false, true) {
			return
		}
		activeSourcePositions.mu.Lock()
		scopes := activeSourcePositions.byBase[base]
		for i := len(scopes) - 1; i >= 0; i-- {
			if scopes[i] == index {
				scopes = append(scopes[:i], scopes[i+1:]...)
				break
			}
		}
		if len(scopes) == 0 {
			delete(activeSourcePositions.byBase, base)
			at := sort.Search(len(activeSourcePositions.bases), func(i int) bool {
				return activeSourcePositions.bases[i] >= base
			})
			if at < len(activeSourcePositions.bases) && activeSourcePositions.bases[at] == base {
				activeSourcePositions.bases = append(activeSourcePositions.bases[:at], activeSourcePositions.bases[at+1:]...)
			}
		} else {
			activeSourcePositions.byBase[base] = scopes
		}
		activeSourcePositions.mu.Unlock()
	}
}

func indexedSourceLineCol(source []byte, offset int) (int, int, bool) {
	if len(source) == 0 {
		return 0, 0, false
	}
	start := uintptr(unsafe.Pointer(unsafe.SliceData(source)))
	end := start + uintptr(len(source))

	activeSourcePositions.mu.RLock()
	at := sort.Search(len(activeSourcePositions.bases), func(i int) bool {
		return activeSourcePositions.bases[i] > start
	}) - 1
	var best *sourcePositionIndex
	for i := at; i >= 0; i-- {
		base := activeSourcePositions.bases[i]
		if best != nil && start >= best.base {
			break
		}
		for _, candidate := range activeSourcePositions.byBase[base] {
			if start >= candidate.base && end <= candidate.base+candidate.size && (best == nil || candidate.size < best.size) {
				best = candidate
			}
		}
	}
	activeSourcePositions.mu.RUnlock()
	if best == nil {
		return 0, 0, false
	}

	localStart := int(start - best.base)
	absolute := localStart + offset
	lineAt := sort.Search(len(best.lineStart), func(i int) bool {
		return best.lineStart[i] > absolute
	}) - 1
	baseLine := sort.Search(len(best.lineStart), func(i int) bool {
		return best.lineStart[i] > localStart
	}) - 1
	if lineAt < 0 {
		lineAt = 0
	}
	if baseLine < 0 {
		baseLine = 0
	}
	line := lineAt - baseLine + 1
	column := offset + 1
	if lineAt > baseLine {
		column = absolute - best.lineStart[lineAt] + 1
	}
	return line, column, true
}

// remapLoweredRanges moves every range in doc from the lowered text that the
// grammar read back to original. The lowering only inserts two-byte "> "
// prefixes at the start of lines; inserts holds the offset in the lowered text
// of each one, in ascending order. An offset that falls inside an insertion
// maps to the point where the insertion was made. A node that has no range
// keeps none.
func remapLoweredRanges(doc *Document, inserts []int, original []byte) {
	if doc == nil || len(inserts) == 0 {
		return
	}
	const insertLen = 2
	lineStarts := []int{0}
	for off, b := range original {
		if b == '\n' {
			lineStarts = append(lineStarts, off+1)
		}
	}
	mapOffset := func(o int) int {
		k := sort.Search(len(inserts), func(i int) bool { return inserts[i] >= o })
		removed := k * insertLen
		if k > 0 && inserts[k-1]+insertLen > o {
			removed -= inserts[k-1] + insertLen - o
		}
		o -= removed
		if o < 0 {
			o = 0
		}
		if o > len(original) {
			o = len(original)
		}
		return o
	}
	lineCol := func(o int) (int, int) {
		line := sort.Search(len(lineStarts), func(i int) bool { return lineStarts[i] > o })
		return line, o - lineStarts[line-1] + 1
	}
	mapRange := func(r Range) Range {
		if r.StartLine == 0 {
			return r
		}
		start, end := mapOffset(r.StartByte), mapOffset(r.EndByte)
		if end < start {
			end = start
		}
		r.StartByte, r.EndByte = start, end
		r.StartLine, r.StartCol = lineCol(start)
		r.EndLine, r.EndCol = lineCol(end)
		return r
	}
	seen := map[*Node]bool{}
	var walk func(n *Node)
	walk = func(n *Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		n.Range = mapRange(n.Range)
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	for i := range doc.diagnostics {
		doc.diagnostics[i].Range = mapRange(doc.diagnostics[i].Range)
	}
	doc.Source = original
}
