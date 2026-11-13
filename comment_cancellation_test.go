package mdpp

import (
	"context"
	"strings"
	"testing"
)

func TestCommentDelimiterCancellation(t *testing.T) {
	text := "*" + strings.Repeat("a", 16000) + "<!-->" + strings.Repeat("b", 16000) + "*"
	c, l := findInlineComments(text)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := &parseCtx{budget: newParseBudget(cancelled, len(text), ParseOptions{})}
	source := []byte("prefix " + text + " suffix")
	nodes, ok := parseCommentDelimitersAt(text, source, 7, c, l, ctx)
	if !ok || len(ctx.inlineStops) != 1 || ctx.budget.hit != "context cancellation" || len(nodes) != 1 || nodes[0].Literal != text {
		t.Fatalf("ok %v nodes=%+v timeout=%v hit=%s", ok, nodes, len(ctx.inlineStops) != 0, ctx.budget.hit)
	}
	if len(ctx.inlineStops) == 1 && (ctx.inlineStops[0].StartByte != 7 || ctx.inlineStops[0].EndByte != 7+len(text)) {
		t.Fatalf("incorrect stop range: %+v", ctx.inlineStops[0])
	}
}
