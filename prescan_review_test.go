package mdpp

import (
	"strings"
	"testing"
)

func TestReviewCommentScanLinearInvalidation(t *testing.T) {
	for _, n := range []int{100, 1000, 10000} {
		text := strings.Repeat("![", n) + strings.Repeat("[a](b)", n) + strings.Repeat("]", n) + "<!--x-->"
		scanner := &commentScan{text: text}
		comments, links := findInlineCommentsScan(scanner)
		if len(comments) != 1 || len(links) != n {
			t.Fatalf("n=%d comments=%d links=%d", n, len(comments), len(links))
		}
		if scanner.steps > len(text) {
			t.Fatalf("n=%d steps=%d bytes=%d", n, scanner.steps, len(text))
		}
	}
}
func TestReviewCommentPrescanCancellation(t *testing.T) {
	for name, text := range map[string]string{
		"raw html":  "<a title=\"" + strings.Repeat("x", 100000) + "\"><!--x-->",
		"backticks": strings.Repeat("`", 100000) + "<!--x-->",
		"comment":   "<!--" + strings.Repeat("x", 100000) + "-->",
		"link":      "[a](" + strings.Repeat("x", 100000) + ")<!--x-->",
		"spaces":    "[a](" + strings.Repeat(" ", 100000) + ")<!--x-->",
		"title":     "[a](x \"" + strings.Repeat("x", 100000) + "\")<!--x-->",
	} {
		t.Run(name, func(t *testing.T) {
			c := &scanCancelContext{}
			ctx := &parseCtx{budget: newParseBudget(c, len(text), ParseOptions{})}
			scanner := &commentScan{text: text, ctx: ctx}
			findInlineCommentsScan(scanner)
			if !scanner.cancelled || ctx.budget.hit != "context cancellation" {
				t.Fatalf("cancelled=%v hit=%q checks=%d", scanner.cancelled, ctx.budget.hit, c.calls)
			}
		})
	}
}
func TestReviewCommentRegexReaderParity(t *testing.T) {
	for _, text := range []string{"<a>" + strings.Repeat("x", 5000) + "\xff", "<x attr=\"" + strings.Repeat("\xff", 5000) + "\">", "<a>", "<a title=\"x > y\">rest", "<mailto:x@example.com>", "<foo@example.com>", "</tag>", "<x a='猫'>", "<a a='broken>", "<x", "<http://a>", "<x attr='" + strings.Repeat("a", 9000) + "'>", "<a>" + strings.Repeat("x", 9000)} {
		ctx := &parseCtx{budget: newParseBudget(nil, len(text), ParseOptions{})}
		want := (&commentScan{text: text}).otherHTMLEnd(0)
		got := (&commentScan{text: text, ctx: ctx}).otherHTMLEnd(0)
		if got != want {
			t.Errorf("text=%q reader=%d direct=%d", text, got, want)
		}
	}
}

func TestReviewCommentPrecheckCancellation(t *testing.T) {
	text := strings.Repeat("x", 100000)
	c := &scanCancelContext{}
	ctx := &parseCtx{budget: newParseBudget(c, len(text), ParseOptions{})}
	nodes := parseInlineAt(text, []byte(text), 0, ctx)
	if len(ctx.inlineStops) == 0 || ctx.budget.hit != "context cancellation" || len(nodes) != 1 || nodes[0].Literal != text {
		t.Fatalf("timeout=%v hit=%q nodes=%d checks=%d", len(ctx.inlineStops) != 0, ctx.budget.hit, len(nodes), c.calls)
	}
}
