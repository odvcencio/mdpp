package lsp

import (
	"errors"
	"sort"
	"sync"
	"unicode/utf8"

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
}

type LineIndex struct {
	source     []byte
	lineStarts []int
}

func NewDocumentStore() *DocumentStore {
	return &DocumentStore{docs: map[DocumentURI]*OpenDocument{}}
}

func (s *DocumentStore) Open(item TextDocumentItem) *OpenDocument {
	doc := parseDocument([]byte(item.Text))
	open := &OpenDocument{
		URI:      item.URI,
		Version:  item.Version,
		Source:   []byte(item.Text),
		Document: doc,
		Index:    NewLineIndex([]byte(item.Text)),
	}
	s.mu.Lock()
	s.docs[item.URI] = open
	s.mu.Unlock()
	return open
}

func (s *DocumentStore) Get(uri DocumentURI) (*OpenDocument, bool) {
	s.mu.RLock()
	doc, ok := s.docs[uri]
	s.mu.RUnlock()
	return doc, ok
}

func (s *DocumentStore) Close(uri DocumentURI) {
	s.mu.Lock()
	delete(s.docs, uri)
	s.mu.Unlock()
}

func (d *OpenDocument) ApplyChanges(version int32, changes []TextDocumentContentChangeEvent) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	source := append([]byte(nil), d.Source...)
	index := NewLineIndex(source)
	for _, change := range changes {
		if change.Range == nil {
			source = []byte(change.Text)
			index = NewLineIndex(source)
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
	d.Document = parseDocument(source)
	return nil
}

func (d *OpenDocument) Snapshot() (*mdpp.Document, []byte, *LineIndex, int32) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	src := append([]byte(nil), d.Source...)
	return d.Document, src, d.Index, d.Version
}

func parseDocument(source []byte) *mdpp.Document {
	doc, err := mdpp.Parse(source)
	if err != nil {
		return &mdpp.Document{Root: &mdpp.Node{Type: mdpp.NodeDocument}, Source: source}
	}
	return doc
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
