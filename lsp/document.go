package lsp

import (
	"errors"
	"sort"
	"sync"
	"unicode/utf8"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/mdpp"
)

type DocumentStore struct {
	mu   sync.RWMutex
	docs map[DocumentURI]*OpenDocument
}

type OpenDocument struct {
	mu       sync.RWMutex
	URI      DocumentURI
	Version  int32
	Source   []byte
	Document *mdpp.Document
	Index    *LineIndex
	// parser carries retained tree-sitter state plus the AST subtree cache
	// across incremental parses. Always non-nil for documents produced by
	// Open / OpenAsync / ApplyChanges; cached reuse of paragraph and
	// container-chunk subtrees is what keeps single-character edit
	// latency low on large documents.
	parser *mdpp.Parser
	// tree mirrors parser's retained tree for legacy paths that still hand
	// a *gotreesitter.Tree directly to mdpp.ParseIncremental. Nil when the
	// last parse used a fallback or no parse has completed yet.
	tree *gotreesitter.Tree
	// parsing is true while an async parse is in flight for this document.
	parsing bool
	// ready is closed once the initial parse has completed. A nil channel
	// means the document was opened synchronously (parse already done).
	ready chan struct{}
}

type LineIndex struct {
	source     []byte
	lineStarts []int
}

func NewDocumentStore() *DocumentStore {
	return &DocumentStore{docs: map[DocumentURI]*OpenDocument{}}
}

func (s *DocumentStore) Open(item TextDocumentItem) *OpenDocument {
	source := []byte(item.Text)
	parser := mdpp.NewParser()
	doc, _ := parser.Parse(source)
	open := &OpenDocument{
		URI:      item.URI,
		Version:  item.Version,
		Source:   source,
		Document: doc,
		Index:    NewLineIndex(source),
		parser:   parser,
	}
	s.mu.Lock()
	if prev, ok := s.docs[item.URI]; ok {
		prev.releaseTree()
	}
	s.docs[item.URI] = open
	s.mu.Unlock()
	return open
}

// OpenAsync inserts an OpenDocument immediately with a placeholder document and
// spawns a goroutine to perform the full parse. onReady, if non-nil, is called
// from the parse goroutine after the document has been updated with the parsed
// result. Callers may call Wait on the returned *OpenDocument to block until
// the initial parse has completed.
func (s *DocumentStore) OpenAsync(item TextDocumentItem, onReady func(*OpenDocument)) *OpenDocument {
	source := []byte(item.Text)
	parser := mdpp.NewParser()
	open := &OpenDocument{
		URI:     item.URI,
		Version: item.Version,
		Source:  source,
		// Placeholder document: empty root, but a real source so that
		// features/index lookups don't NPE while the real parse runs.
		Document: &mdpp.Document{Root: &mdpp.Node{Type: mdpp.NodeDocument}, Source: source},
		Index:    NewLineIndex(source),
		parser:   parser,
		parsing:  true,
		ready:    make(chan struct{}),
	}
	s.mu.Lock()
	if prev, ok := s.docs[item.URI]; ok {
		prev.releaseTree()
	}
	s.docs[item.URI] = open
	s.mu.Unlock()

	initialVersion := item.Version
	go func() {
		parsed, _ := parser.Parse(source)
		open.mu.Lock()
		// Guard against a didChange that ran while we were parsing: only
		// install the result if the document is still on the version the
		// async parse was started for.
		if open.Version == initialVersion {
			open.Document = parsed
		}
		open.parsing = false
		ch := open.ready
		open.mu.Unlock()
		if ch != nil {
			close(ch)
		}
		if onReady != nil {
			onReady(open)
		}
	}()
	return open
}

// Wait blocks until the initial async parse (if any) has completed.
func (d *OpenDocument) Wait() {
	d.mu.RLock()
	ch := d.ready
	d.mu.RUnlock()
	if ch != nil {
		<-ch
	}
}

// Parsing reports whether an async parse is currently in flight.
func (d *OpenDocument) Parsing() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.parsing
}

func (d *OpenDocument) releaseTree() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tree != nil {
		d.tree.Release()
		d.tree = nil
	}
	if d.parser != nil {
		d.parser.Close()
		d.parser = nil
	}
}

func (s *DocumentStore) Get(uri DocumentURI) (*OpenDocument, bool) {
	s.mu.RLock()
	doc, ok := s.docs[uri]
	s.mu.RUnlock()
	return doc, ok
}

func (s *DocumentStore) Close(uri DocumentURI) {
	s.mu.Lock()
	if doc, ok := s.docs[uri]; ok {
		doc.releaseTree()
	}
	delete(s.docs, uri)
	s.mu.Unlock()
}

func (d *OpenDocument) ApplyChanges(version int32, changes []TextDocumentContentChangeEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	source := append([]byte(nil), d.Source...)
	index := NewLineIndex(source)

	// Track the accumulated incremental edit when every change carries a
	// range. A single whole-document change (nil Range) forces a full parse.
	incremental := d.parser != nil && !d.parsing && len(changes) > 0
	type pendingEdit struct {
		startByte, oldEndByte, newEndByte int
		startRow, startCol                uint32
		oldEndRow, oldEndCol              uint32
		newEndRow, newEndCol              uint32
	}
	var edits []pendingEdit

	for _, change := range changes {
		if change.Range == nil {
			source = []byte(change.Text)
			index = NewLineIndex(source)
			incremental = false
			edits = nil
			continue
		}
		start, ok := index.PositionToOffset(change.Range.Start)
		if !ok {
			return errors.New("change range start is outside the document")
		}
		end, ok := index.PositionToOffset(change.Range.End)
		if !ok {
			return errors.New("change range end is outside the document")
		}
		if end < start {
			return errors.New("change range end is before start")
		}

		if incremental {
			// Capture byte-accurate edit coordinates in the pre-edit source.
			startRow, startCol := byteOffsetToRowCol(source, start)
			oldEndRow, oldEndCol := byteOffsetToRowCol(source, end)
			newText := []byte(change.Text)
			newEndByte := start + len(newText)
			// Compute new-end row/col by walking the replacement bytes from
			// the start point.
			newEndRow := startRow
			newEndCol := startCol
			for _, b := range newText {
				if b == '\n' {
					newEndRow++
					newEndCol = 0
				} else {
					newEndCol++
				}
			}
			edits = append(edits, pendingEdit{
				startByte:  start,
				oldEndByte: end,
				newEndByte: newEndByte,
				startRow:   startRow,
				startCol:   startCol,
				oldEndRow:  oldEndRow,
				oldEndCol:  oldEndCol,
				newEndRow:  newEndRow,
				newEndCol:  newEndCol,
			})
		}

		next := make([]byte, 0, len(source)-(end-start)+len(change.Text))
		next = append(next, source[:start]...)
		next = append(next, change.Text...)
		next = append(next, source[end:]...)
		source = next
		index = NewLineIndex(source)
	}

	d.Version = version
	d.Source = source
	d.Index = index

	if incremental && len(edits) > 0 && d.parser != nil {
		// Use the retained Parser: it handles the primary-path tree
		// internally and reuses cached paragraph/container-chunk subtrees
		// even when the outer path is a fallback (e.g. container directive
		// documents, where tree-sitter incremental doesn't apply but chunk
		// caching still delivers the needed speedup).
		var edit gotreesitter.InputEdit
		for i, e := range edits {
			ie := gotreesitter.InputEdit{
				StartByte:   uint32(e.startByte),
				OldEndByte:  uint32(e.oldEndByte),
				NewEndByte:  uint32(e.newEndByte),
				StartPoint:  gotreesitter.Point{Row: e.startRow, Column: e.startCol},
				OldEndPoint: gotreesitter.Point{Row: e.oldEndRow, Column: e.oldEndCol},
				NewEndPoint: gotreesitter.Point{Row: e.newEndRow, Column: e.newEndCol},
			}
			if i == 0 {
				edit = ie
			} else {
				d.parser.ApplyEdit(ie)
			}
		}
		doc, err := d.parser.ParseIncremental(source, edit)
		if err == nil && doc != nil {
			d.Document = doc
			return nil
		}
	}

	// Full parse path.
	if d.parser == nil {
		d.parser = mdpp.NewParser()
	}
	doc, _ := d.parser.Parse(source)
	d.Document = doc
	return nil
}

// byteOffsetToRowCol returns the (row, column-in-bytes) for the given byte
// offset in source. Column counts bytes, not runes or UTF-16 code units;
// tree-sitter uses byte-based points.
func byteOffsetToRowCol(source []byte, offset int) (uint32, uint32) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	var row, col uint32
	for i := 0; i < offset; i++ {
		if source[i] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	return row, col
}

func (d *OpenDocument) Snapshot() (*mdpp.Document, []byte, *LineIndex, int32) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	src := append([]byte(nil), d.Source...)
	return d.Document, src, d.Index, d.Version
}

// SnapshotReady blocks until the initial parse is complete (if any), then
// returns a Snapshot. Feature handlers that need a real parsed document
// (hover, completion, definition, formatting, semantic tokens, etc.) should
// use this so they don't see the placeholder document for a just-opened file.
func (d *OpenDocument) SnapshotReady() (*mdpp.Document, []byte, *LineIndex, int32) {
	d.Wait()
	return d.Snapshot()
}

func parseDocument(source []byte) *mdpp.Document {
	doc, err := mdpp.Parse(source)
	if err != nil {
		return &mdpp.Document{Root: &mdpp.Node{Type: mdpp.NodeDocument}, Source: source}
	}
	return doc
}

// parseDocumentWithTree parses source and returns the resulting document plus
// the retained tree-sitter Tree (nil when a non-primary parse path was used).
// The caller owns the returned Tree and must eventually Release() it (or feed
// it back to mdpp.ParseIncremental, which assumes ownership).
func parseDocumentWithTree(source []byte) (*mdpp.Document, *gotreesitter.Tree) {
	doc, tree, err := mdpp.ParseWithTree(source)
	if err != nil || doc == nil {
		if tree != nil {
			tree.Release()
		}
		return &mdpp.Document{Root: &mdpp.Node{Type: mdpp.NodeDocument}, Source: source}, nil
	}
	return doc, tree
}

func NewLineIndex(source []byte) *LineIndex {
	starts := []int{0}
	for i, b := range source {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &LineIndex{
		source:     append([]byte(nil), source...),
		lineStarts: starts,
	}
}

func (i *LineIndex) PositionToOffset(pos Position) (int, bool) {
	if i == nil || int(pos.Line) >= len(i.lineStarts) {
		return len(i.source), false
	}
	start := i.lineStarts[pos.Line]
	end := i.lineContentEnd(int(pos.Line))
	if end < start {
		end = start
	}
	rel, ok := utf16ColumnToByte(i.source[start:end], int(pos.Character))
	if !ok {
		return end, false
	}
	return start + rel, true
}

func (i *LineIndex) OffsetToPosition(offset int) Position {
	if i == nil {
		return Position{}
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(i.source) {
		offset = len(i.source)
	}
	line := i.lineForOffset(offset)
	start := i.lineStarts[line]
	col := byteColumnToUTF16(i.source[start:offset])
	return Position{Line: uint32(line), Character: uint32(col)}
}

func (i *LineIndex) RangeToLSP(r mdpp.Range) Range {
	start := r.StartByte
	end := r.EndByte
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if start > len(i.source) {
		start = len(i.source)
	}
	if end > len(i.source) {
		end = len(i.source)
	}
	return Range{
		Start: i.OffsetToPosition(start),
		End:   i.OffsetToPosition(end),
	}
}

func (i *LineIndex) LinePrefix(pos Position) (string, bool) {
	offset, ok := i.PositionToOffset(pos)
	if !ok || int(pos.Line) >= len(i.lineStarts) {
		return "", false
	}
	start := i.lineStarts[pos.Line]
	return string(i.source[start:offset]), true
}

func (i *LineIndex) UTF16Length(start, end int) uint32 {
	if i == nil {
		return 0
	}
	if start < 0 {
		start = 0
	}
	if end > len(i.source) {
		end = len(i.source)
	}
	if end < start {
		end = start
	}
	return uint32(byteColumnToUTF16(i.source[start:end]))
}

func (i *LineIndex) LineContentEndForOffset(offset int) int {
	if i == nil {
		return 0
	}
	line := i.lineForOffset(offset)
	return i.lineContentEnd(line)
}

func (i *LineIndex) NextLineStartAfterOffset(offset int) int {
	if i == nil {
		return 0
	}
	line := i.lineForOffset(offset)
	if line+1 >= len(i.lineStarts) {
		return len(i.source)
	}
	return i.lineStarts[line+1]
}

func (i *LineIndex) lineForOffset(offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(i.source) {
		offset = len(i.source)
	}
	line := sort.Search(len(i.lineStarts), func(n int) bool {
		return i.lineStarts[n] > offset
	}) - 1
	if line < 0 {
		return 0
	}
	return line
}

func (i *LineIndex) lineContentEnd(line int) int {
	if line < 0 {
		return 0
	}
	if line >= len(i.lineStarts) {
		return len(i.source)
	}
	end := len(i.source)
	if line+1 < len(i.lineStarts) {
		end = i.lineStarts[line+1]
	}
	if end > 0 && end <= len(i.source) && i.source[end-1] == '\n' {
		end--
	}
	if end > 0 && end <= len(i.source) && i.source[end-1] == '\r' {
		end--
	}
	return end
}

func utf16ColumnToByte(line []byte, want int) (int, bool) {
	if want <= 0 {
		return 0, true
	}
	col := 0
	for off := 0; off < len(line); {
		r, size := utf8.DecodeRune(line[off:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		nextCol := col + 1
		if r > 0xffff {
			nextCol++
		}
		if nextCol > want {
			return off, true
		}
		off += size
		col = nextCol
		if col == want {
			return off, true
		}
	}
	if col == want {
		return len(line), true
	}
	return len(line), false
}

func byteColumnToUTF16(line []byte) int {
	col := 0
	for off := 0; off < len(line); {
		r, size := utf8.DecodeRune(line[off:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		off += size
		col++
		if r > 0xffff {
			col++
		}
	}
	return col
}
