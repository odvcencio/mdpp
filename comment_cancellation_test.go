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
	nodes, ok := parseCommentDelimitersAt(text, []byte(text), 0, c, l, ctx)
	if !ok || !ctx.inlineTimeoutOccurred || ctx.budget.hit != "context cancellation" || len(nodes) != 1 || nodes[0].Literal != text {
		t.Fatalf("ok %v nodes=%+v timeout=%v hit=%s", ok, nodes, ctx.inlineTimeoutOccurred, ctx.budget.hit)
	}
}
