package mdpp

import (
	"strings"
	"testing"
)

// TestSplitFastTableCellSpansCarriesTrueOffsets guards the 2026-08-09
// corruption: fast-path table cells shared the whole row's range, so a
// range-based consumer (the fmt table rewriter) turned every cell into
// the full row and multiplied tables by their column count per pass.
func TestSplitFastTableCellSpansCarriesTrueOffsets(t *testing.T) {
	line := "| Piece | Location | Behavior |"
	spans := splitFastTableCellSpans(line)
	if len(spans) != 3 {
		t.Fatalf("spans = %d, want 3", len(spans))
	}
	wantTexts := []string{"Piece", "Location", "Behavior"}
	prevEnd := -1
	for i, span := range spans {
		if got := strings.TrimSpace(line[span.start:span.end]); got != wantTexts[i] {
			t.Fatalf("span %d = %q, want %q", i, got, wantTexts[i])
		}
		if span.start <= prevEnd {
			t.Fatalf("span %d start %d does not advance past %d", i, span.start, prevEnd)
		}
		prevEnd = span.end
	}

	indented := "  | a | b |"
	spans = splitFastTableCellSpans(indented)
	if len(spans) != 2 || strings.TrimSpace(indented[spans[0].start:spans[0].end]) != "a" || strings.TrimSpace(indented[spans[1].start:spans[1].end]) != "b" {
		t.Fatalf("indented spans = %+v", spans)
	}
}
