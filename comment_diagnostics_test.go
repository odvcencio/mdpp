package mdpp

import (
	"context"
	"testing"
)

func TestCommentMalformedDiagnosticsAndBudget(t *testing.T) {
	cases := []struct {
		text   string
		work   int64
		ranges [][2]int
	}{
		{"*a <!--> <a href=\" b*", 360, [][2]int{{8, 20}}},
		{"*a <!--> [x]( b*", 0, [][2]int{{8, 16}}},
	}
	for _, c := range cases {
		ctx := &parseCtx{budget: newParseBudget(context.Background(), len(c.text), ParseOptions{WorkBudget: 100000})}
		parseInlineAt(c.text, []byte(c.text), 0, ctx)
		if ctx.budget.workUsed != c.work || ctx.budget.hit != "" {
			t.Errorf("%q: work=%d hit=%q", c.text, ctx.budget.workUsed, ctx.budget.hit)
		}
		if len(ctx.recoveryDiagnostics) != len(c.ranges) {
			t.Errorf("%q: diagnostics=%+v", c.text, ctx.recoveryDiagnostics)
			continue
		}
		for i, d := range ctx.recoveryDiagnostics {
			if d.Code != "MDPP-PARSE-006" || d.Range.StartByte != c.ranges[i][0] || d.Range.EndByte != c.ranges[i][1] {
				t.Errorf("%q: diagnostic=%+v", c.text, d)
			}
		}
	}
}
