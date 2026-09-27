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
