package mdpp
import "testing"
func TestReviewCommentEdgeFormatting(t *testing.T) {
 for _, src := range []string{"*a <!--> b*\n", "*a <!---> b*\n", "*a <!-- x -- y --> b*\n", "*a <!-- x > y --> b*\n", "*[a <!--> b](/u)*\n", "*a <!-- `x` --> b*\n"} {
  t.Run(src, func(t *testing.T) { d:=MustParse([]byte(src)); if len(d.Root.Find(NodeEmphasis)) != 1 || len(d.Root.Find(NodeHTMLInline)) != 1 { t.Errorf("%s",DumpTreeForSnapshot(d.Root)) } })
 }
}
