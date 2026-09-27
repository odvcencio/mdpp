package fmt

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/mdpp"
)

// segmentedTableFixtureBoundary mirrors mdpp's byte-sized segmented-parser
// boundary. Keep this literal local to the regression: the threshold is
// intentionally package-private, while the test needs exact 2047/2048-byte
// inputs to prove which exported parser path handled the table.
const segmentedTableFixtureBoundary = 2048

func TestSegmentedTableFormatIdempotence(t *testing.T) {
	tests := []struct {
		name string
		size int
		fast bool
	}{
		{name: "below segmented boundary", size: segmentedTableFixtureBoundary - 1, fast: false},
		{name: "segmented fast path", size: segmentedTableFixtureBoundary, fast: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := segmentedTableSource(tt.size)
			if len(src) != tt.size {
				t.Fatalf("fixture length = %d, want %d", len(src), tt.size)
			}

			// ParseWithTree exposes whether the primary tree-sitter parser ran:
			// a nil tree means a fallback parser produced the document. This
			// fixture has only headings, a simple table, and plain text, so the
			// 2048-byte nil-tree result together with the 2047-byte primary-tree
			// result is stable evidence of the segmented fast-path boundary.
			doc, tree, err := mdpp.ParseWithTree(src)
			usedPrimaryTree := tree != nil
			if tree != nil {
				tree.Release()
			}
			if err != nil {
				t.Fatalf("ParseWithTree: %v", err)
			}
			if usedPrimaryTree == tt.fast {
				t.Fatalf("primary tree = %t, want %t", usedPrimaryTree, !tt.fast)
			}
			assertSegmentedTableShape(t, src, doc)

			once, err := Format(src)
			if err != nil {
				t.Fatalf("Format: %v", err)
			}
			if len(once) > len(src)*2 {
				t.Fatalf("first Format pass grew document from %d to %d bytes", len(src), len(once))
			}
			for _, marker := range []string{"segmented-left", "segmented-right", "row-left", "row-right"} {
				if got := bytes.Count(once, []byte(marker)); got != 1 {
					t.Fatalf("formatted table marker %q occurs %d times, want once", marker, got)
				}
			}

			twice, err := Format(once)
			if err != nil {
				t.Fatalf("Format twice: %v", err)
			}
			if !bytes.Equal(once, twice) {
				t.Fatalf("Format is not idempotent:\nfirst:\n%s\nsecond:\n%s", once, twice)
			}

			formattedDoc, err := mdpp.Parse(once)
			if err != nil {
				t.Fatalf("Parse formatted source: %v", err)
			}
			assertSegmentedTableShape(t, once, formattedDoc)
		})
	}
}

func segmentedTableSource(size int) []byte {
	const prefix = "# H0\n\n| segmented-left | segmented-right |\n|---|---|\n| row-left | row-right |\n\n"
	const suffix = "\n# H1\n\n# H2\n"
	padding := size - len(prefix) - len(suffix)
	if padding < 0 {
		panic("segmented table fixture size is smaller than its fixed source")
	}
	return []byte(prefix + strings.Repeat("x", padding) + suffix)
}

func assertSegmentedTableShape(t *testing.T, source []byte, doc *mdpp.Document) {
	t.Helper()
	if doc == nil || doc.AST() == nil {
		t.Fatal("Parse returned no document AST")
	}
	tables := doc.AST().Find(mdpp.NodeTable)
	if len(tables) != 1 {
		t.Fatalf("table count = %d, want 1", len(tables))
	}
	if headings := doc.AST().Find(mdpp.NodeHeading); len(headings) != 3 {
		t.Fatalf("top-level fixture heading count = %d, want 3", len(headings))
	}
	table := tables[0]
	wantRows := [][]string{
		{"segmented-left", "segmented-right"},
		{"row-left", "row-right"},
	}
	if len(table.Children) != len(wantRows) {
		t.Fatalf("table row count = %d, want %d", len(table.Children), len(wantRows))
	}
	for rowIndex, row := range table.Children {
		if row == nil {
			t.Fatalf("row %d is nil", rowIndex)
		}
		if row.Type != mdpp.NodeTableRow {
			t.Fatalf("row %d has type %v, want table row", rowIndex, row.Type)
		}
		if len(row.Children) != len(wantRows[rowIndex]) {
			t.Fatalf("row %d cell count = %d, want %d", rowIndex, len(row.Children), len(wantRows[rowIndex]))
		}
		if row.Range.StartLine == 0 || row.Range.StartLine != row.Range.EndLine {
			t.Fatalf("row %d range has invalid line span: %+v", rowIndex, row.Range)
		}
		if row.Range.StartByte < 0 || row.Range.StartByte >= row.Range.EndByte || row.Range.EndByte > len(source) {
			t.Fatalf("row %d range is outside source: %+v (source length %d)", rowIndex, row.Range, len(source))
		}
		previousEnd := row.Range.StartByte
		for cellIndex, cell := range row.Children {
			if cell == nil {
				t.Fatalf("row %d cell %d is nil", rowIndex, cellIndex)
			}
			if cell.Type != mdpp.NodeTableCell {
				t.Fatalf("row %d cell %d has type %v, want table cell", rowIndex, cellIndex, cell.Type)
			}
			if cell.Range.StartLine == 0 || cell.Range.StartLine != cell.Range.EndLine {
				t.Fatalf("row %d cell %d range has invalid line span: %+v", rowIndex, cellIndex, cell.Range)
			}
			if cell.Range.StartByte < row.Range.StartByte || cell.Range.StartByte < previousEnd || cell.Range.StartByte >= cell.Range.EndByte || cell.Range.EndByte > row.Range.EndByte {
				t.Fatalf("row %d cell %d range does not advance within row: row=%+v cell=%+v", rowIndex, cellIndex, row.Range, cell.Range)
			}
			got := strings.TrimSpace(string(source[cell.Range.StartByte:cell.Range.EndByte]))
			if got != wantRows[rowIndex][cellIndex] {
				t.Fatalf("row %d cell %d text = %q, want %q (range=%+v)", rowIndex, cellIndex, got, wantRows[rowIndex][cellIndex], cell.Range)
			}
			previousEnd = cell.Range.EndByte
		}
	}
}
