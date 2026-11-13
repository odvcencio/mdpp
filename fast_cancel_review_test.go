package mdpp

import (
	"context"
	"strings"
	"testing"
)

func TestReviewFastEligibilityCancellation(t *testing.T) {
	cases := []struct {
		name string
		run  func(*parseCtx)
	}{
		{"line", func(c *parseCtx) { fastChunkBytesAcceptedContext([]byte("a\u00a0"+strings.Repeat("x", 100000)), c) }},
		{"left whitespace", func(c *parseCtx) {
			src := []byte("x" + strings.Repeat(" ", 100000) + "\u00a0x")
			line := fastLineBounds{start: 0, end: len(src), first: 0}
			fastStrayRuneIsTextContext(src, &line, len(src)-3, 2, c)
		}},
		{"right whitespace", func(c *parseCtx) {
			src := []byte("x\u00a0" + strings.Repeat(" ", 100000) + "x")
			line := fastLineBounds{start: 0, end: len(src), first: 0}
			fastStrayRuneIsTextContext(src, &line, 1, 2, c)
		}},
		{"label", func(c *parseCtx) {
			src := []byte("[" + strings.Repeat("x", 100000) + "]")
			fastLineBoundsAtContext(src, 0, c)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cancel := &scanCancelContext{}
			ctx := &parseCtx{budget: newParseBudget(cancel, 100000, ParseOptions{})}
			tc.run(ctx)
			if ctx.budget.hit != "context cancellation" {
				t.Fatalf("hit=%q checks=%d", ctx.budget.hit, cancel.calls)
			}
		})
	}
	_ = context.Canceled
}
