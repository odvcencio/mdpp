package mdpp
import (
 "context"
 "strings"
 "testing"
)
func TestCommentDelimiterBudgetAndRanges(t *testing.T) {
 for _, text := range []string{"*a <!--> b*", "**a <!---> b**", "~~a <!-- x -- y --> b~~", "*a <!-- x\ny --> b*"} {
  src := []byte("prefix "+text); ctx:= &parseCtx{budget:newParseBudget(context.Background(),len(src),ParseOptions{WorkBudget:1})}; nodes:=parseInlineAt(text,src,7,ctx)
  if ctx.budget.hit != "" || ctx.budget.workUsed != 0 || len(ctx.recoveryDiagnostics) != 0 {t.Errorf("%q: budget %+v diagnostics %+v",text,ctx.budget,ctx.recoveryDiagnostics)}
  if len(nodes)!=1 || (nodes[0].Type!=NodeEmphasis && nodes[0].Type!=NodeStrong && nodes[0].Type!=NodeStrikethrough) {t.Errorf("%q: nodes %+v", text,nodes); continue}
  if nodes[0].Range.StartByte!=7 || nodes[0].Range.EndByte!=len(src) {t.Errorf("%q: range %+v",text,nodes[0].Range)}
  comments:=nodes[0].Find(NodeHTMLInline); if len(comments)!=1 {t.Errorf("%q: comments %d", text,len(comments));continue}; n:=comments[0]; if string(src[n.Range.StartByte:n.Range.EndByte])!=n.Literal || !strings.HasPrefix(n.Literal,"<!--") {t.Errorf("%q: comment %+v",text,n)}
 }
}
