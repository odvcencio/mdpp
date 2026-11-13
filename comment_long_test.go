package mdpp

import (
	"context"
	"strings"
	"testing"
)

func TestCommentLongAndDepth(t *testing.T) {
	for _, n := range []int{321, 2048, 16384} {
		text := "*" + strings.Repeat("x", n) + " <!--> [a][ref] b*"
		ctx := &parseCtx{budget: newParseBudget(context.Background(), len(text), ParseOptions{WorkBudget: 1})}
		src := []byte(text)
		unregister := registerSourcePositions(src)
		ns := parseInlineAt(text, src, 0, ctx)
		unregister()
		root := newNode(NodeDocument, ns...)
		if len(root.Find(NodeEmphasis)) != 1 || len(root.Find(NodeLink)) != 1 || len(root.Find(NodeHTMLInline)) != 1 {
			t.Errorf("length %d: missing nodes", n)
		}
		if ctx.budget.hit != "" || ctx.budget.workUsed != 0 || len(ctx.recoveryDiagnostics) != 0 {
			t.Errorf("length %d: budget=%+v diagnostics=%+v", n, ctx.budget, ctx.recoveryDiagnostics)
		}
		if len(ns) > 0 && (ns[0].Range.StartByte != 0 || ns[0].Range.EndByte != len(text)) {
			t.Errorf("range: %+v", ns[0].Range)
		}
	}
	text := strings.Repeat("*", 2*(maxInlineParseChunk/2+1)) + "<!-->" + strings.Repeat("*", 2*(maxInlineParseChunk/2+1))
	c, l := findInlineComments(text)
	nodes, ok := parseCommentDelimitersAt(text, nil, -1, c, l, nil)
	if !ok || len(newNode(NodeDocument, nodes...).Find(NodeStrong)) != maxInlineParseChunk/2+1 {
		t.Error("lost deep strong formatting")
	}
	ctx := &parseCtx{budget: newParseBudget(context.Background(), len(text), ParseOptions{WorkBudget: 1})}
	nodes, ok = parseCommentDelimitersAt(text, []byte(text), 0, c, l, ctx)
	if !ok || len(ctx.inlineStops) != 0 || ctx.budget.hit != "" || len(newNode(NodeDocument, nodes...).Find(NodeStrong)) != maxInlineParseChunk/2+1 {
		t.Errorf("delimiter depth spent parser budget: %#v", ctx)
	}
}
