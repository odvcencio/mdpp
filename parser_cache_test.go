package mdpp

import (
	"reflect"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// TestParserIncrementalReusesCachedParagraphs exercises the paragraph and
// container-chunk cache paths: a parser that has parsed a document once
// should parse a single-character-inserted variant with nonzero cache hits,
// and the produced document should be structurally equivalent to a full
// fresh parse of the edited source.
func TestParserIncrementalReusesCachedParagraphs(t *testing.T) {
	src := []byte(strings.Join([]string{
		"# Heading one",
		"",
		"Paragraph one stays the same forever.",
		"",
		"Paragraph two has content.",
		"",
		"Paragraph three has even more content here.",
		"",
		"## Heading two",
		"",
		"Final paragraph with tail text.",
		"",
	}, "\n"))

	p := NewParser()
	defer p.Close()

	doc1, err := p.Parse(src)
	if err != nil {
		t.Fatalf("initial parse: %v", err)
	}
	if doc1 == nil || doc1.Root == nil {
		t.Fatal("initial parse produced no document")
	}

	// After cold parse, hits should be zero (nothing to reuse), misses > 0.
	hits, misses := p.LastStats()
	if hits != 0 {
		t.Fatalf("expected 0 hits on cold parse, got %d", hits)
	}
	if misses == 0 {
		t.Fatalf("expected >0 misses on cold parse, got %d", misses)
	}

	// Insert a single 'z' into "Paragraph two" → exactly one paragraph
	// should change; the others (plus headings) should be cache hits.
	needle := []byte("Paragraph two")
	idx := strings.Index(string(src), string(needle))
	if idx < 0 {
		t.Fatal("needle not found")
	}
	insertAt := idx + len(needle)
	next := make([]byte, 0, len(src)+1)
	next = append(next, src[:insertAt]...)
	next = append(next, 'z')
	next = append(next, src[insertAt:]...)

	var row, col uint32
	for i := 0; i < insertAt; i++ {
		if src[i] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	edit := gotreesitter.InputEdit{
		StartByte:   uint32(insertAt),
		OldEndByte:  uint32(insertAt),
		NewEndByte:  uint32(insertAt + 1),
		StartPoint:  gotreesitter.Point{Row: row, Column: col},
		OldEndPoint: gotreesitter.Point{Row: row, Column: col},
		NewEndPoint: gotreesitter.Point{Row: row, Column: col + 1},
	}

	doc2, err := p.ParseIncremental(next, edit)
	if err != nil {
		t.Fatalf("incremental parse: %v", err)
	}
	if doc2 == nil || doc2.Root == nil {
		t.Fatal("incremental parse produced no document")
	}

	hits, misses = p.LastStats()
	if hits == 0 {
		t.Fatalf("expected >0 cache hits on incremental reparse, got %d (misses=%d)", hits, misses)
	}

	// Compare against a fresh full parse of the edited source.
	fresh := MustParse(next)
	if fresh == nil {
		t.Fatal("fresh parse failed")
	}
	if got, want := countParagraphs(doc2.Root), countParagraphs(fresh.Root); got != want {
		t.Fatalf("paragraph count mismatch: incremental=%d fresh=%d", got, want)
	}
	if got, want := countHeadings(doc2.Root), countHeadings(fresh.Root); got != want {
		t.Fatalf("heading count mismatch: incremental=%d fresh=%d", got, want)
	}
	if !reflect.DeepEqual(doc2.Root, fresh.Root) {
		t.Fatal("incremental cached AST ranges differ from fresh parse")
	}

	// Edited paragraph should contain the newly-inserted 'z'.
	if !strings.Contains(doc2.Root.Text(), "Paragraph twoz") {
		t.Fatalf("edited paragraph did not include inserted byte; full text = %q", doc2.Root.Text())
	}
}

func TestParserSegmentedCachePreservesRangesAcrossInsertedLine(t *testing.T) {
	source, sites := makeMarkdownEditorBenchmarkSource(8 << 10)
	insertAt := sites[len(sites)/3]
	next := make([]byte, 0, len(source)+1)
	next = append(next, source[:insertAt]...)
	next = append(next, '\n')
	next = append(next, source[insertAt:]...)

	start := markdownEditorPointForOffset(source, insertAt)
	edit := gotreesitter.InputEdit{
		StartByte:   uint32(insertAt),
		OldEndByte:  uint32(insertAt),
		NewEndByte:  uint32(insertAt + 1),
		StartPoint:  start,
		OldEndPoint: start,
		NewEndPoint: gotreesitter.Point{Row: start.Row + 1, Column: 0},
	}

	parser := NewParser()
	defer parser.Close()
	if _, err := parser.Parse(source); err != nil {
		t.Fatalf("initial parse: %v", err)
	}
	incremental, err := parser.ParseIncremental(next, edit)
	if err != nil {
		t.Fatalf("incremental parse: %v", err)
	}
	hits, _ := parser.LastStats()
	if hits == 0 {
		t.Fatal("expected segmented cache hits after inserted line")
	}
	fresh := MustParse(next)
	if !reflect.DeepEqual(incremental.Root, fresh.Root) {
		t.Fatal("segmented cached AST ranges differ from fresh parse after inserted line")
	}
}

// TestParserContainerChunkCache verifies that a container-directive document
// (which forces the fallback path where tree-sitter incremental parse is
// bypassed) still benefits from chunk caching across ParseIncremental calls.
func TestParserContainerChunkCache(t *testing.T) {
	src := []byte(strings.Join([]string{
		"# Outer heading",
		"",
		"First outer paragraph that never changes.",
		"",
		":::tip \"Pay attention\"",
		"Body paragraph inside a container.",
		":::",
		"",
		"Third outer paragraph with more words.",
		"",
	}, "\n"))

	p := NewParser()
	defer p.Close()

	if _, err := p.Parse(src); err != nil {
		t.Fatalf("initial parse: %v", err)
	}

	// Edit inside the first outer paragraph, not the container body.
	needle := []byte("First outer paragraph")
	idx := strings.Index(string(src), string(needle))
	insertAt := idx + len(needle)
	next := make([]byte, 0, len(src)+1)
	next = append(next, src[:insertAt]...)
	next = append(next, 'Q')
	next = append(next, src[insertAt:]...)
	var row, col uint32
	for i := 0; i < insertAt; i++ {
		if src[i] == '\n' {
			row++
			col = 0
		} else {
			col++
		}
	}
	edit := gotreesitter.InputEdit{
		StartByte:   uint32(insertAt),
		OldEndByte:  uint32(insertAt),
		NewEndByte:  uint32(insertAt + 1),
		StartPoint:  gotreesitter.Point{Row: row, Column: col},
		OldEndPoint: gotreesitter.Point{Row: row, Column: col},
		NewEndPoint: gotreesitter.Point{Row: row, Column: col + 1},
	}

	doc2, err := p.ParseIncremental(next, edit)
	if err != nil {
		t.Fatalf("incremental: %v", err)
	}
	hits, _ := p.LastStats()
	if hits == 0 {
		t.Fatalf("expected cache hits for container chunk path, got 0")
	}

	// Output should still contain the container body, un-reparsed.
	if !strings.Contains(doc2.Root.Text(), "Body paragraph inside a container") {
		t.Fatalf("container body missing from incremental reparse")
	}
}

func countParagraphs(n *Node) int {
	count := 0
	n.Walk(func(cur *Node) bool {
		if cur.Type == NodeParagraph {
			count++
		}
		return true
	})
	return count
}

func countHeadings(n *Node) int {
	count := 0
	n.Walk(func(cur *Node) bool {
		if cur.Type == NodeHeading {
			count++
		}
		return true
	})
	return count
}
