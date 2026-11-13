package mdpp
import (
 "testing"
 "context"
)
func TestCommentComplexControls(t *testing.T) {
 cases:=[]struct {src string; want []NodeType; comments int}{
 {"*a <!--> `c` [b](/u) <x> z*",[]NodeType{NodeEmphasis,NodeCodeSpan,NodeLink,NodeHTMLInline},2},
 {"[*a <!--> b*](/u)",[]NodeType{NodeLink,NodeEmphasis,NodeHTMLInline},1},
 {"*[a <!--> b](/u)*",[]NodeType{NodeLink,NodeEmphasis,NodeHTMLInline},1},
 {"*a <!--x--> `<!--hidden-->` <!--y--> b*",[]NodeType{NodeEmphasis,NodeCodeSpan},2},
 {"***a <!--x--> b***",[]NodeType{NodeEmphasis,NodeStrong},1},
 {"*a <!--x\ny--> b*",[]NodeType{NodeEmphasis},1},
 }
 for _, c:=range cases {t.Run(c.src,func(t *testing.T){ctx:=&parseCtx{budget:newParseBudget(context.Background(),len(c.src),ParseOptions{WorkBudget:100000})}; ns:=parseInlineAt(c.src,[]byte(c.src),0,ctx); root:=newNode(NodeDocument,ns...); for _, typ:=range c.want {if len(root.Find(typ))==0 {t.Errorf("missing %v: %s",typ,DumpTreeForSnapshot(root))}};if n:=len(root.Find(NodeHTMLInline));n!=c.comments {t.Errorf("html count %d != %d: %s",n,c.comments,DumpTreeForSnapshot(root))};var walk func(*Node);walk=func(n *Node){ if n.Type!=NodeDocument && (n.Range.StartByte<0||n.Range.EndByte>len(c.src)||n.Range.StartByte>=n.Range.EndByte) {t.Errorf("invalid %v range %+v",n.Type,n.Range)};for _, child:=range n.Children {if child.Range.StartByte<n.Range.StartByte || child.Range.EndByte>n.Range.EndByte {if n.Type!=NodeDocument {t.Errorf("child %v escapes %v",child.Range,n.Range)}};walk(child)}};walk(root);if ctx.budget.hit!="" || len(ctx.recoveryDiagnostics)>0 {t.Errorf("budget %s diagnostics %+v",ctx.budget.hit,ctx.recoveryDiagnostics)}})}
}
