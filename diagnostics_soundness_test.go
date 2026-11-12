package mdpp

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// The diagnostics of a parse must say, for every part of the source that was
// kept as text or guessed, that its tree is not the structural reading. These
// tests check that on every path that returns a document.

// covers reports whether the ranges together cover bytes start..end.
func covers(ranges []Range, start, end int) bool {
	sorted := append([]Range(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StartByte < sorted[j].StartByte })
	at := start
	for _, r := range sorted {
		if r.StartByte <= at && r.EndByte > at {
			at = r.EndByte
		}
	}
	return at >= end
}

func diagnosticSummary(doc *Document) string {
	var parts []string
	for _, d := range doc.Diagnostics() {
		parts = append(parts, fmt.Sprintf("%s[%d,%d)", d.Code, d.Range.StartByte, d.Range.EndByte))
	}
	return strings.Join(parts, " ")
}

// When the inline fallback returns a whole span as text, the whole span is
// uncertain, not only the place where the grammar reported its error.
func TestInlineFallbackUncertaintyCoversTheWholeSpan(t *testing.T) {
	cases := []struct {
		src        string
		start, end int // the span that was returned as text
	}{
		{"~~x~~ <<a http://a\n", 0, 18}, // the strikethrough is lost
		{"*x* ~~<<a http://a\n", 0, 18},
		{"a *b* <<a http://a\n", 0, 18},
	}
	for _, tc := range cases {
		doc := MustParse([]byte(tc.src))
		if doc.StructureCertified() {
			t.Errorf("%q: certified, but the span was returned as text:\n%s", tc.src, fullNodeDump(doc.Root))
			continue
		}
		if !covers(doc.UncertifiedRanges(), tc.start, tc.end) {
			t.Errorf("%q: uncertified ranges %s do not cover %d..%d", tc.src, diagnosticSummary(doc), tc.start, tc.end)
		}
	}
}

// The fast block path, chunk assembly and chunk-cache hits report the same
// diagnostics as the full parser, and a document that discarded structure is
// never certified.
func TestDiagnosticsSurviveEveryReturnPath(t *testing.T) {
	filler := strings.Repeat("Plain words fill the section so that the document is long enough to be cut into chunks.\n", 12)
	segmented := "# One\n\n" + filler + "\n# Two\n\n*x* <a http://a\n\n" + filler + "\n# Three\n\n" + filler
	cases := []struct {
		name, src  string
		start, end int
	}{
		{"fast block path", "***\n*x* <a http://a\n", 4, 19},
		{"fast block path, link", "***\n[a](<b\n", 4, 10},
		{"fast path attempt that is given up", "***\n*x* <a http://a\n\n- [x]\ta\n", 4, 19},
		{"after an HTML block", "<!-- -->\n [a](<b\n", 9, 16},
		{"segmented document", segmented, strings.Index(segmented, "*x* <a"), strings.Index(segmented, "*x* <a") + len("*x* <a http://a")},
		{"chunk before a container", "*x* <a http://a\n\n:::note\nbody\n:::\n", 0, 15},
		{"chunk after a container", ":::note\nbody\n:::\n\n*x* <a http://a\n", 18, 33},
		{"container body", ":::note\n*x* <a http://a\n:::\n", 8, 23},
	}
	for _, tc := range cases {
		fastBlockChunkDisabled = true
		full := MustParse([]byte(tc.src))
		fastBlockChunkDisabled = false
		doc := MustParse([]byte(tc.src))
		if doc.StructureCertified() {
			t.Errorf("%s: certified:\n%s", tc.name, fullNodeDump(doc.Root))
			continue
		}
		if !covers(doc.UncertifiedRanges(), tc.start, tc.end) {
			t.Errorf("%s: uncertified ranges %s do not cover %d..%d", tc.name, diagnosticSummary(doc), tc.start, tc.end)
		}
		if got, want := diagnosticSummary(doc), diagnosticSummary(full); got != want {
			t.Errorf("%s: diagnostics differ between the fast and the full path:\n fast: %s\n full: %s", tc.name, got, want)
		}

		// The same input through one Parser twice: the second parse may use
		// cached chunks and must report the same.
		p := NewParser()
		first, err := p.Parse([]byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		second, err := p.Parse([]byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}
		if diagnosticSummary(first) != diagnosticSummary(second) || second.StructureCertified() {
			t.Errorf("%s: Parser reports\n first:  %s\n second: %s (certified %v)", tc.name, diagnosticSummary(first), diagnosticSummary(second), second.StructureCertified())
		}
		if !covers(second.UncertifiedRanges(), tc.start, tc.end) {
			t.Errorf("%s: second parse: ranges %s do not cover %d..%d", tc.name, diagnosticSummary(second), tc.start, tc.end)
		}
		p.Close()
	}
}

// A chunk whose parse recorded a diagnostic is not cached; one without is.
func TestChunkWithDiagnosticsIsNotCached(t *testing.T) {
	p := NewParser()
	defer p.Close()
	src := []byte("<!-- -->\n [a](<b\n")
	for i := 0; i < 3; i++ {
		doc, err := p.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if doc.StructureCertified() || len(doc.Diagnostics()) != 1 {
			t.Fatalf("parse %d: diagnostics %s", i, diagnosticSummary(doc))
		}
	}
}

// The inline stops of a document are reported at most maxStopDiagnostics times,
// and what is over the limit is folded into one range that covers it.
func TestManyInlineStopsKeepTheirCoverage(t *testing.T) {
	src := []byte(strings.Repeat("x", 65) + "\n")
	var stops []Range
	for i := 0; i < 65; i++ {
		stops = append(stops, sourceRange(src, i, i+1))
	}
	ranges := inlineStopRanges(stops, src)
	if len(ranges) > maxStopDiagnostics {
		t.Errorf("%d ranges, want at most %d", len(ranges), maxStopDiagnostics)
	}
	if !covers(ranges, 0, 65) {
		t.Errorf("the ranges do not cover every stop: %+v", ranges)
	}
	// Through the context, onto a document.
	ctx := &parseCtx{}
	for i := 0; i < 65; i++ {
		ctx.noteInlineStop(src, i, "x")
	}
	doc := &Document{Source: src}
	ctx.collect(doc, src, diagMark{})
	if len(doc.diagnostics) > maxStopDiagnostics || !covers(doc.UncertifiedRanges(), 0, 65) {
		t.Errorf("document diagnostics: %d, covering 0..65: %v", len(doc.diagnostics), covers(doc.UncertifiedRanges(), 0, 65))
	}
	if len(ctx.inlineStops) != 0 {
		t.Errorf("%d stops left in the context", len(ctx.inlineStops))
	}
}
