package mdpp

import (
	"fmt"
	"testing"
)

func TestFastComparatorMatchesDumpAndDetectsMutations(t *testing.T) {
	cases := []string{"[^1]: a\n", "[^1]: a\r\nb\r\n", "# a\n\n- x\n- y\n", "|a|b|\n|-|-|\n|1|2|\n", "<!-- c -->\n\n> a\n> b\n", "x &amp; y\n", ""}
	inputs := differentialInputs()
	for i := 0; i < len(inputs); i += 997 {
		cases = append(cases, inputs[i])
	}
	for _, noRanges := range []bool{false, true} {
		dumpWithoutRanges = noRanges
		for _, src := range cases {
			a := parseTreeWithFastPath(src, true)
			b := parseTreeWithFastPath(src, false)
			dumpSource = []byte(src)
			direct := sameFastPathTrees(src)
			dumpSource = nil
			wantPublic := parseWithFastPath(src, true) == parseWithFastPath(src, false)
			if direct != wantPublic {
				t.Fatalf("public verdict mismatch noRanges=%v src=%q", noRanges, src)
			}
			dumpSource = []byte(src)
			if a == nil || b == nil {
				continue
			}
			want := fullNodeDump(a) == fullNodeDump(b)
			got := sameDifferentialNode(a, b, []byte(src), []byte(src), noRanges)
			if got != want {
				t.Fatalf("different verdict noRanges=%v src=%q", noRanges, src)
			}
		}
	}
	dumpSource = nil
	dumpWithoutRanges = false
	base := func() *Node {
		return &Node{Type: NodeParagraph, Literal: "p", Attrs: map[string]string{"a": "b"}, Range: Range{StartByte: 1, EndByte: 3, StartLine: 1, StartCol: 2, EndLine: 1, EndCol: 4}, Children: []*Node{{Type: NodeText, Literal: "x", Range: Range{StartByte: 1, EndByte: 2, StartLine: 1, StartCol: 2, EndLine: 1, EndCol: 3}}}}
	}
	mutations := []struct {
		name string
		f    func(*Node)
	}{{"type", func(n *Node) { n.Type = NodeText }}, {"literal", func(n *Node) { n.Literal = "q" }}, {"attr-value", func(n *Node) { n.Attrs["a"] = "c" }}, {"attr-key", func(n *Node) { delete(n.Attrs, "a"); n.Attrs["b"] = "b" }}, {"range", func(n *Node) { n.Range.EndCol++ }}, {"child-literal", func(n *Node) { n.Children[0].Literal = "z" }}, {"child-count", func(n *Node) { n.Children = nil }}}
	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			a, b := base(), base()
			m.f(b)
			if fullNodeDump(a) == fullNodeDump(b) {
				t.Fatal("negative control did not change dump")
			}
			if sameDifferentialNode(a, b, nil, nil, false) {
				t.Fatal("mutation missed")
			}
		})
	}
}

func BenchmarkFastDifferentialComparison(b *testing.B) {
	src := "***\n\n# heading\n\n- a [link](https://example.org)\n- b\n\n|a|b|\n|-|-|\n|1|2|\n\n[^1]: note\n"
	a := parseTreeWithFastPath(src, true)
	c := parseTreeWithFastPath(src, false)
	source := []byte(src)
	if a == nil || c == nil {
		b.Fatal("parse")
	}
	b.Run("dump", func(b *testing.B) {
		dumpSource = source
		defer func() { dumpSource = nil }()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = fullNodeDump(a) == fullNodeDump(c)
		}
	})
	b.Run("structural", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = sameDifferentialNode(a, c, source, source, false)
		}
	})
}

func TestFastComparatorFootnoteRangeNormalization(t *testing.T) {
	for _, src := range []string{"a\n", "a\r\n"} {
		t.Run(fmt.Sprintf("bytes_%d", len(src)), func(t *testing.T) {
			a := &Node{Type: NodeFootnoteDef, Literal: "a", Range: Range{StartByte: 0, StartLine: 1, StartCol: 1, EndByte: 1, EndLine: 1, EndCol: 2}}
			b := &Node{Type: NodeFootnoteDef, Literal: "a", Range: Range{StartByte: 0, StartLine: 1, StartCol: 1, EndByte: len(src), EndLine: 2, EndCol: 1}}
			dumpSource = []byte(src)
			defer func() { dumpSource = nil }()
			if fullNodeDump(a) != fullNodeDump(b) || !sameDifferentialNode(a, b, []byte(src), []byte(src), false) {
				t.Fatal("trailing newline should normalize")
			}
			b.Range.StartCol++
			if fullNodeDump(a) == fullNodeDump(b) || sameDifferentialNode(a, b, []byte(src), []byte(src), false) {
				t.Fatal("start position mismatch not detected")
			}
			if !sameDifferentialNode(a, b, []byte(src), []byte(src), true) {
				t.Fatal("no-ranges comparison included positions")
			}
		})
	}
}

func TestFastComparatorPreservesAmbiguousDump(t *testing.T) {
	a := &Node{Type: NodeText, Attrs: map[string]string{"a": "x", "b": "y"}}
	b := &Node{Type: NodeText, Attrs: map[string]string{`a="x" b`: "y"}}
	if fullNodeDump(a) != fullNodeDump(b) {
		t.Fatal("fixture is not dump-equivalent")
	}
	if sameDifferentialNode(a, b, nil, nil, false) {
		t.Fatal("fixture is structurally distinct")
	}
	if !sameDifferentialDump(a, b, nil, nil, false) {
		t.Fatal("did not preserve dump verdict")
	}
}

func TestFastComparatorFootnoteOneSourceNil(t *testing.T) {
	a := &Node{Type: NodeFootnoteDef, Literal: "a", Range: Range{StartLine: 1, StartCol: 1, EndByte: 2, EndLine: 2, EndCol: 1}}
	b := &Node{Type: NodeFootnoteDef, Literal: "a", Range: a.Range}
	if sameDifferentialDump(a, b, []byte("a\n"), nil, false) {
		t.Fatal("source-specific normalization was lost")
	}
}

func TestSharedFastDocumentMatchesSeparateParse(t *testing.T) {
	inputs := differentialInputs()
	for _, noRanges := range []bool{false, true} {
		dumpWithoutRanges = noRanges
		for i := 0; i < len(inputs); i += 997 {
			src := inputs[i]
			doc, err := Parse([]byte(src))
			if err != nil || doc == nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			dumpSource = doc.Source
			fromRange := fullNodeDump(doc.Root)
			dumpSource = nil
			fromFast := parseWithFastPath(src, true)
			if fromRange != fromFast {
				t.Fatalf("default and fast parses differ for %q", src)
			}
			expected := fromFast == parseWithFastPath(src, false)
			if sameFullPathAsDocument(src, doc, noRanges) != expected {
				t.Fatalf("shared comparison differs for %q", src)
			}
		}
	}
	dumpWithoutRanges = false
	src := "# heading\n"
	doc, err := Parse([]byte(src))
	if err != nil || doc == nil || len(doc.Root.Children) == 0 {
		t.Fatalf("fixture %v", err)
	}
	doc.Root.Children[0].Literal += "mutation"
	if sameFullPathAsDocument(src, doc, false) {
		t.Fatal("shared comparison missed mutation")
	}
}

func TestFastComparatorNilChildrenPreserveDumpPanic(t *testing.T) {
	a := &Node{Type: NodeDocument, Children: []*Node{nil}}
	b := &Node{Type: NodeDocument, Children: []*Node{nil}}
	if sameDifferentialNode(nil, nil, nil, nil, false) {
		t.Fatal("nil nodes must decline the structural shortcut")
	}
	if sameDifferentialNode(a, b, nil, nil, false) {
		t.Fatal("nil children must decline the structural shortcut")
	}
	panics := func(f func()) (yes bool) { defer func() { yes = recover() != nil }(); f(); return }
	if !panics(func() { _ = fullNodeDump(a) }) {
		t.Fatal("original dump unexpectedly accepted a nil child")
	}
	if !panics(func() { _ = sameDifferentialDump(a, b, nil, nil, false) }) {
		t.Fatal("comparison hid the original nil-child panic")
	}
}
