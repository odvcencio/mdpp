package mdpp

import (
	"reflect"
	"testing"
)

func TestCommentReferenceASTCompatibility(t *testing.T) {
	for _, suffix := range []string{"[x]", "[x *y*][id]", "[x *y*](/u)"} {
		before := suffix
		after := "*a <!--> " + suffix + " b*"
		a := MustParse([]byte(before)).Root.Find(NodeLink)
		b := MustParse([]byte(after)).Root.Find(NodeLink)
		if len(a) != 1 || len(b) != 1 {
			t.Errorf("%q: links %d, %d", suffix, len(a), len(b))
			continue
		}
		if _, resolved := a[0].Attrs["href"]; !resolved {
			delete(a[0].Attrs, "raw")
			if len(b[0].Children) < 2 || b[0].Children[0].Literal != "[" || b[0].Children[len(b[0].Children)-1].Literal[0] != ']' {
				t.Errorf("%q unresolved brackets not materialized", suffix)
			}
			for _, child := range []*Node{b[0].Children[0], b[0].Children[len(b[0].Children)-1]} {
				if child.Range.StartByte < 0 || child.Range.EndByte > len(after) || after[child.Range.StartByte:child.Range.EndByte] != child.Literal {
					t.Errorf("%q bracket range %+v literal %q", suffix, child.Range, child.Literal)
				}
			}
		}
		if b[0].referenceSuffix != nil {
			t.Errorf("%q has transient state after parse", suffix)
		}
		if !reflect.DeepEqual(a[0].Attrs, b[0].Attrs) {
			t.Errorf("%q: attributes %v != %v", suffix, a[0].Attrs, b[0].Attrs)
		}
		if b[0].Range.StartByte-a[0].Range.StartByte != 9 || b[0].Range.EndByte-a[0].Range.EndByte != 9 {
			t.Errorf("%q: ranges %+v %+v", suffix, a[0].Range, b[0].Range)
		}
	}
}
