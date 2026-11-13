package mdpp

import (
	"strings"
	"testing"
)

// The input of the review that found a task list kept as paragraph text with
// no way for the caller to tell.
func reviewComplexityInput() string {
	return "## Summary\n<!-- describe -->\n## Checks\n- [X] " + strings.Repeat("a*b_", 5000)
}

func TestComplexityFallbackDiagnosticCoversTheBlock(t *testing.T) {
	src := reviewComplexityInput()
	doc, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var found []Diagnostic
	for _, d := range doc.Diagnostics() {
		if d.Code == DiagComplexityLimit {
			found = append(found, d)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one %s diagnostic, got %+v\n%s", DiagComplexityLimit, doc.Diagnostics(), fullNodeDump(doc.Root))
	}
	d := found[0]
	block := strings.Index(src, "- [X] ")
	if d.Range.StartByte > block || d.Range.EndByte < len(src) {
		t.Errorf("the diagnostic covers bytes %d-%d, want the block %d-%d", d.Range.StartByte, d.Range.EndByte, block, len(src))
	}
	if d.Range.StartLine != 4 || d.Range.StartByte < strings.Index(src, "## Checks") {
		t.Errorf("the diagnostic starts at %+v, want line 4 (the block, not the headings before it)", d.Range)
	}
	if !d.StructureUncertain() || doc.StructureCertified() {
		t.Error("the fallback must make the structure uncertain")
	}
	ranges := doc.UncertifiedRanges()
	if len(ranges) != 1 || ranges[0] != d.Range {
		t.Errorf("UncertifiedRanges = %+v", ranges)
	}
	// The tree has no task item, as the review saw; the diagnostic is what says why.
	if len(doc.Root.Find(NodeTaskListItem)) != 0 {
		t.Error("unexpected task item")
	}
	// The headings before the block are certified: no diagnostic overlaps them.
	for _, h := range doc.Root.Find(NodeHeading) {
		if h.Range.StartByte < d.Range.EndByte && d.Range.StartByte < h.Range.EndByte {
			t.Errorf("heading %q [%d-%d] overlaps the uncertified range", h.Text(), h.Range.StartByte, h.Range.EndByte)
		}
	}
	// A clean document is certified.
	clean := MustParse([]byte("# T\n\n- [x] a\n"))
	if !clean.StructureCertified() || len(clean.UncertifiedRanges()) != 0 {
		t.Errorf("a clean document is uncertified: %+v", clean.Diagnostics())
	}
}

// Every fallback leaves a diagnostic with a range inside the source.
func TestEveryFallbackLeavesADiagnosticWithARange(t *testing.T) {
	deep := strings.Repeat(":::a\n", maxContainerDepth+3) + "x\n" + strings.Repeat(":::\n", maxContainerDepth+3)
	cases := []struct {
		name string
		src  string
		opts ParseOptions
		code string
	}{
		{"complexity limit", reviewComplexityInput(), ParseOptions{}, DiagComplexityLimit},
		{"work budget", "# T\n\n- a\n- b\n", ParseOptions{WorkBudget: 1}, DiagBudgetExceeded},
		{"unclosed container", "intro\n\n:::note\ninside\n\nafter\n", ParseOptions{}, DiagContainerAutoClosed},
		{"recursion depth", deep, ParseOptions{}, DiagDepthExceeded},
		{"syntax error kept as text", "[a](<b\n\n[c]: <d\n", ParseOptions{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := ParseWithOptions([]byte(tc.src), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if tc.code == "" {
				return // exercised for panics only
			}
			var d *Diagnostic
			for _, x := range doc.Diagnostics() {
				if x.Code == tc.code {
					x := x
					d = &x
					break
				}
			}
			if d == nil {
				t.Fatalf("no %s diagnostic: %+v", tc.code, doc.Diagnostics())
			}
			r := d.Range
			if r.StartLine == 0 || r.EndByte <= r.StartByte || r.EndByte > len(doc.Source) {
				t.Errorf("%s diagnostic has the range %+v (source %d bytes)", tc.code, r, len(doc.Source))
			}
			if !d.StructureUncertain() {
				t.Errorf("%s does not mark the structure uncertain", tc.code)
			}
		})
	}
}

// A container with no closing fence is closed at the end of the document; the
// diagnostic covers the guessed extent, not only the opening line.
func TestUnclosedContainerDiagnosticCoversItsBody(t *testing.T) {
	src := "intro\n\n:::note\ninside\n\nafter\n"
	doc := MustParse([]byte(src))
	for _, d := range doc.Diagnostics() {
		if d.Code != DiagContainerAutoClosed {
			continue
		}
		open := strings.Index(src, ":::note")
		if d.Range.StartByte != open || d.Range.EndByte != len(src) {
			t.Errorf("range %d-%d, want %d-%d", d.Range.StartByte, d.Range.EndByte, open, len(src))
		}
		return
	}
	t.Fatalf("no %s diagnostic: %+v", DiagContainerAutoClosed, doc.Diagnostics())
}

// The error branches that used to return an empty document with no diagnostic.
func TestNoTreeFallbackKeepsTheSourceAndSaysSo(t *testing.T) {
	doc := noTreeDocument([]byte("# T\n\ntext"))
	if len(doc.diagnostics) != 1 || doc.diagnostics[0].Code != DiagNoTree || doc.diagnostics[0].Range.EndByte == 0 {
		t.Fatalf("diagnostics: %+v", doc.diagnostics)
	}
	if got := doc.Root.Text(); !strings.Contains(got, "text") {
		t.Errorf("the source is lost: %q", got)
	}
	if doc.StructureCertified() {
		t.Error("certified")
	}
}

// An inline span that runs out of budget is reported with its own range.
func TestInlineBudgetDiagnosticHasTheSpan(t *testing.T) {
	ctx := &parseCtx{}
	src := []byte("ab *cd* ef\n")
	ctx.noteInlineStop(src, 3, "*cd*")
	if len(ctx.inlineStops) != 1 || ctx.inlineStops[0].StartByte != 3 || ctx.inlineStops[0].EndByte != 7 {
		t.Fatalf("stops: %+v", ctx.inlineStops)
	}
}
