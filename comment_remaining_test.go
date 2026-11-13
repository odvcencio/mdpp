package mdpp
import ("strings"; "testing")
func TestCommentRemainingControls(t *testing.T) {
 cases:=[]string{
  "*a <!--> b!*",
  "*a <!--> \\* b*",
  "*a <!--> [link][id] b*",
  "*"+strings.Repeat("a",321)+" <!--> b*",
 }
 for _, s:=range cases {t.Run(s[:min(25,len(s))],func(t *testing.T){ns:=parseInlineAt(s,[]byte(s),0,nil);root:=newNode(NodeDocument,ns...);if len(root.Find(NodeEmphasis))!=1 {t.Errorf("missing emphasis: %s",DumpTreeForSnapshot(root))}})}
}
