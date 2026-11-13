package mdpp
import "testing"
func TestCommentDelimiterControls(t *testing.T) {
 cases:=[]struct{s string; types []NodeType}{
 {"*a <!--> b* ~z~",[]NodeType{NodeEmphasis,NodeSubscript}},
 {"*a <!--> b* `<x>`",[]NodeType{NodeEmphasis,NodeCodeSpan}},
 {"*a <!--> [link](/u) b*",[]NodeType{NodeEmphasis,NodeLink}},
 {"[prefix *a <!--> b*](/u)",[]NodeType{NodeEmphasis,NodeLink}},
 {"<x> *a <!--> b*",[]NodeType{NodeEmphasis,NodeHTMLInline}},
 }
 for _, tc:=range cases {t.Run(tc.s,func(t *testing.T){nodes:=parseInlineAt(tc.s,[]byte(tc.s),0,nil); root:=newNode(NodeDocument,nodes...);for _, typ:=range tc.types {if len(root.Find(typ))==0 {t.Errorf("missing %v: %s",typ,DumpTreeForSnapshot(root))}}})}
}
