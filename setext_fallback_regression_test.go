package mdpp

import (
	"context"
	"strings"
	"testing"
	"time"
)

type scanCancelContext struct{ calls int }

func (*scanCancelContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*scanCancelContext) Done() <-chan struct{}       { return nil }
func (c *scanCancelContext) Err() error {
	c.calls++
	if c.calls >= 4 {
		return context.Canceled
	}
	return nil
}
func (*scanCancelContext) Value(any) any { return nil }

func TestSetextFallbackPreservesContext(t *testing.T) {
	ctx := &parseCtx{recoveryDiagnostics: []Diagnostic{{Code: "sentinel"}}}
	doc, ok := parseFastBlockChunk([]byte("T [x](\nU\n===\n"), ctx)
	if ok || doc != nil {
		t.Fatalf("expected full-parser fallback, got %v %v", doc, ok)
	}
	if len(ctx.recoveryDiagnostics) != 1 || ctx.recoveryDiagnostics[0].Code != "sentinel" {
		t.Fatalf("discarded paragraph leaked diagnostics: %+v", ctx.recoveryDiagnostics)
	}
}
func TestSetextFallbackPreservesWorkBudget(t *testing.T) {
	src := []byte("***\n\nT a\\*b\nU\n===\n")
	doc, err := ParseWithOptions(src, ParseOptions{WorkBudget: 1170})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	doc.Root.Walk(func(n *Node) bool {
		if n.Type == NodeHeading {
			found = true
		}
		return true
	})
	if !found {
		t.Fatalf("heading lost at 1170 work units: diagnostics=%+v tree=%s", doc.Diagnostics(), DumpTreeForSnapshot(doc.Root))
	}
	for _, d := range doc.Diagnostics() {
		if d.Code == "MDPP-PARSE-005" {
			t.Fatalf("discarded paragraph exhausted work budget: %+v", d)
		}
	}
}

func TestSetextFallbackDoesNotLeakEarlierBlockWork(t *testing.T) {
	cases := []string{
		"# Heading [x](\n\nT\nU\n===\n",
		"T\nU\n===\n[^n]: [x](\n",
	}
	for _, src := range cases {
		ctx := &parseCtx{recoveryDiagnostics: []Diagnostic{{Code: "sentinel"}}}
		doc, ok := parseFastBlockChunk([]byte(src), ctx)
		if ok || doc != nil {
			t.Fatalf("%q: expected fallback", src)
		}
		if len(ctx.recoveryDiagnostics) != 1 || ctx.recoveryDiagnostics[0].Code != "sentinel" {
			t.Errorf("%q: discarded work leaked diagnostics: %+v", src, ctx.recoveryDiagnostics)
		}
	}
}

func TestSetextPreflightKeepsFencedCodeFast(t *testing.T) {
	src := []byte("```go\nT\nU\n===\n```\n")
	doc, ok := parseFastBlockChunk(src, &parseCtx{})
	if !ok || doc == nil || len(doc.Root.Children) != 1 || doc.Root.Children[0].Type != NodeCodeBlock {
		t.Fatalf("fenced block unexpectedly declined: ok=%t doc=%+v", ok, doc)
	}
}

func TestSetextPreflightKeepsTableFast(t *testing.T) {
	src := []byte("| a | b |\n| --- | --- |\n| c | d |\n| e | f |\n===\n")
	doc, ok := parseFastBlockChunk(src, &parseCtx{})
	if !ok || doc == nil || len(doc.Root.Children) < 1 || doc.Root.Children[0].Type != NodeTable {
		t.Fatalf("table unexpectedly declined: ok=%t doc=%+v", ok, doc)
	}
}

func TestReviewLongLineBudgetPreserved(t *testing.T) {
	for _, n := range []int{65537, 70000} {
		src := []byte(strings.Repeat("x", n) + "\n")
		b := newParseBudget(context.Background(), len(src), ParseOptions{WorkBudget: int64(30 * len(src))})
		b.reserveParserWork(len(src))
		ctx := &parseCtx{budget: b}
		var ds []Diagnostic
		children := appendParsedSegmentCtx(nil, &ds, map[string]linkRefDef{}, src, blockChunk{start: 0, end: len(src), startLine: 1, startCol: 1}, ctx)
		if b.hit != "" || len(children) != 1 || children[0].Type != NodeParagraph {
			t.Errorf("length=%d hit=%q nodes=%d", n, b.hit, len(children))
		}
	}
}

func TestReviewConstructionCancellation(t *testing.T) {
	for _, kind := range []string{"fence", "table", "list", "paragraph"} {
		t.Run(kind, func(t *testing.T) {
			c := &scanCancelContext{}
			b := newParseBudget(c, 10000, ParseOptions{})
			ctx := &parseCtx{budget: b}
			switch kind {
			case "fence":
				_, _, _ = fastFenceNode([]byte("```\nx\ny\n```\n"), sourceLines([]byte("```\nx\ny\n```\n")), 0, ctx)
			case "table":
				_, _ = fastTableNode([]byte("| a | b |\n|---|---|\n| x | y |\n"), sourceLines([]byte("| a | b |\n|---|---|\n| x | y |\n")), 0, ctx)
			case "list":
				_, _ = fastListNode([]byte("- x\n- y\n- z\n- w\n"), sourceLines([]byte("- x\n- y\n- z\n- w\n")), 0, ctx)
			case "paragraph":
				c.calls = -20
				src := []byte(strings.Repeat("word\n", 1000))
				_, _ = parseFastBlockChunk(src, ctx)
			}
			if b.hit != "context cancellation" {
				t.Errorf("hit=%q calls=%d", b.hit, c.calls)
			}
		})
	}
}

func TestReviewTrimSpaceEquivalence(t *testing.T) {
	for _, s := range []string{strings.Repeat(" ", 5000) + "x" + strings.Repeat("\t", 5000), strings.Repeat("\u2003", 2000) + "x" + strings.Repeat("\u00a0", 3000), strings.Repeat(" ", 5000) + string([]byte{0xff}) + strings.Repeat(" ", 5000), strings.Repeat("\n", 6000)} {
		if got, want := fastTrimSpace(nil, s), strings.TrimSpace(s); got != want {
			t.Fatalf("trim mismatch %q vs %q", got, want)
		}
	}
}

func TestReviewCancellationDuringWhitespace(t *testing.T) {
	c := &scanCancelContext{}
	b := newParseBudget(c, 100000, ParseOptions{})
	_ = fastTrimSpace(&parseCtx{budget: b}, strings.Repeat(" ", 100000))
	if b.hit != "context cancellation" {
		t.Fatalf("hit=%q calls=%d", b.hit, c.calls)
	}
}

func TestReviewThematicParagraphInterruption(t *testing.T) {
	for _, marker := range []string{"***", "___", "* * *"} {
		src := []byte("paragraph\n" + marker + "\nnext\n")
		d, ok := parseFastBlockChunk(src, &parseCtx{})
		if !ok || len(d.Root.Children) != 3 || d.Root.Children[0].Type != NodeParagraph || d.Root.Children[1].Type != NodeThematicBreak || d.Root.Children[2].Type != NodeParagraph {
			t.Errorf("marker=%q ok=%v doc=%+v", marker, ok, d)
		}
	}
	if _, ok := parseFastBlockChunk([]byte("paragraph\n---\n"), &parseCtx{}); ok {
		t.Fatal("setext underline should fall back")
	}
}

func TestReviewQuoteBoundaries(t *testing.T) {
	cases := []struct {
		source   string
		accepted bool
	}{{"> q\n\n", true}, {"> q\n\n\n", true}, {"> q\n\ntext\n", true}, {"> q\n\n# h\n", true}, {"> q\n\n> r\n", false}, {"> q\n \n", false}, {"> q\n\n- item\n", true}, {"> q\r\n\r\n", true}}
	for _, tc := range cases {
		_, ok := parseFastBlockChunk([]byte(tc.source), &parseCtx{})
		if ok != tc.accepted {
			t.Errorf("%q: accepted=%v, want %v", tc.source, ok, tc.accepted)
		}
	}
}
