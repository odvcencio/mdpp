package mdpp

import (
	"strings"
	"testing"
)

// sourceOf returns the bytes a node's range covers in doc.Source.
func sourceOf(t *testing.T, doc *Document, n *Node) string {
	t.Helper()
	if !n.HasSourceRange() {
		t.Fatalf("%v %q has no source range", n.Type, n.Literal)
	}
	return string(doc.Source[n.Range.StartByte:n.Range.EndByte])
}

func TestTaskItemInQuoteHasItsRange(t *testing.T) {
	for _, src := range []string{"\n> - [X] A", "> - [X] A", "## Summary\n<!-- describe -->\n## Checks\n\n> - [X] A"} {
		doc := MustParse([]byte(src))
		items := doc.Root.Find(NodeTaskListItem)
		if len(items) != 1 {
			t.Fatalf("%q: %d task items", src, len(items))
		}
		got := sourceOf(t, doc, items[0])
		if !strings.HasPrefix(got, "- [X] A") {
			t.Errorf("%q: the task item covers %q, want it to start at its list marker", src, got)
		}
		if quotes := doc.Root.Find(NodeBlockquote); len(quotes) != 1 || !strings.HasPrefix(sourceOf(t, doc, quotes[0]), ">") {
			t.Errorf("%q: the block quote does not start at '>'", src)
		}
	}
}

// The alert lowering adds "> " to the lines after "> [!NOTE]". Ranges must
// refer to the text that was passed in.
func TestAlertLoweringKeepsOriginalRanges(t *testing.T) {
	cases := []struct {
		src      string
		heading  string
		trailing string
	}{
		{"> [!NOTE]\nx\n\n## Evidence\nObserved.", "## Evidence\n", "Observed.\n"},
		{"> [!NOTE]\nx\n## Evidence\nObserved.", "## Evidence\n", "Observed.\n"},
		{"---\ntitle: t\n---\n\n> [!WARNING]\nline one\nline two\n\n## Evidence\nObserved.\n", "## Evidence\n", "Observed.\n"},
		{"para\n\n> [!TIP]\nfirst\n\n> [!NOTE]\nsecond\n\n## Evidence\nObserved.\n", "## Evidence\n", "Observed.\n"},
	}
	for _, tc := range cases {
		doc := MustParse([]byte(tc.src))
		if !strings.HasPrefix(string(doc.Source), tc.src) {
			t.Errorf("%q: Document.Source is %q, which is not the input", tc.src, doc.Source)
		}
		headings := doc.Root.Find(NodeHeading)
		if len(headings) != 1 {
			t.Fatalf("%q: %d headings", tc.src, len(headings))
		}
		if got := sourceOf(t, doc, headings[0]); got != tc.heading {
			t.Errorf("%q: the heading covers %q, want %q", tc.src, got, tc.heading)
		}
		if l := headings[0].Range.StartLine; l != 1+strings.Count(tc.src[:strings.Index(tc.src, "## Evidence")], "\n") {
			t.Errorf("%q: the heading starts on line %d", tc.src, l)
		}
		var found bool
		doc.Root.Walk(func(n *Node) bool {
			if n.Type == NodeText && n.Literal == "Observed." {
				found = true
				if got := sourceOf(t, doc, n); got != "Observed." {
					t.Errorf("%q: the text covers %q", tc.src, got)
				}
			}
			return true
		})
		if !found {
			t.Errorf("%q: no text node 'Observed.'", tc.src)
		}
		if problems := checkRangeConsistency(doc, tc.src); len(problems) > 0 {
			t.Errorf("%q: %v", tc.src, problems)
		}
		for _, d := range doc.Diagnostics() {
			if d.Range.EndByte > len(doc.Source) {
				t.Errorf("%q: diagnostic %v lies outside the source", tc.src, d)
			}
		}
	}
}

// The text of an alert body has a range of its own although the lowering
// rewrote the lines it sits on.
func TestAlertBodyTextHasRange(t *testing.T) {
	src := "> [!NOTE] Title\n> first line\nsecond line\n"
	doc := MustParse([]byte(src))
	adm := doc.Root.Find(NodeAdmonition)
	if len(adm) != 1 {
		t.Fatalf("%d admonitions", len(adm))
	}
	got := map[string]string{}
	adm[0].Walk(func(n *Node) bool {
		if n.Type == NodeText {
			got[n.Literal] = sourceOf(t, doc, n)
		}
		return true
	})
	for _, want := range []string{"first line", "second line"} {
		if got[want] != want {
			t.Errorf("the text %q covers %q (all: %v)", want, got[want], got)
		}
	}
}

func TestDefinitionListHasRanges(t *testing.T) {
	src := "Term *one*\n: First meaning\n: Second meaning\n\nOther\n: x\n"
	doc := MustParse([]byte(src))
	lists := doc.Root.Find(NodeDefinitionList)
	if len(lists) != 1 {
		t.Fatalf("%d definition lists", len(lists))
	}
	if got := sourceOf(t, doc, lists[0]); got != src {
		t.Errorf("the list covers %q, want the whole input", got)
	}
	terms, descs := doc.Root.Find(NodeDefinitionTerm), doc.Root.Find(NodeDefinitionDesc)
	if len(terms) != 2 || len(descs) != 3 {
		t.Fatalf("%d terms, %d descriptions", len(terms), len(descs))
	}
	if got := sourceOf(t, doc, terms[0]); got != "Term *one*" {
		t.Errorf("term covers %q", got)
	}
	wantDescs := []string{"First meaning", "Second meaning", "x"}
	for i, d := range descs {
		if got := sourceOf(t, doc, d); got != wantDescs[i] {
			t.Errorf("description %d covers %q, want %q", i, got, wantDescs[i])
		}
	}
}

func TestQuoteHeadingAndFootnoteRanges(t *testing.T) {
	doc := MustParse([]byte("> [Heading *x*]\n> body\n"))
	strong := doc.Root.Find(NodeStrong)
	if len(strong) != 1 {
		t.Fatalf("%d strong nodes:\n%s", len(strong), fullNodeDump(doc.Root))
	}
	if got := sourceOf(t, doc, strong[0]); got != "[Heading *x*]" {
		t.Errorf("the quote heading covers %q", got)
	}
	doc = MustParse([]byte("text[^1]\n\n[^1]: the *note*\n"))
	for _, def := range doc.Root.Find(NodeFootnoteDef) {
		def.Walk(func(n *Node) bool {
			if n.Type == NodeText && n.Literal == "the " {
				if got := sourceOf(t, doc, n); got != "the " {
					t.Errorf("footnote text covers %q", got)
				}
			}
			return true
		})
	}
}

func TestInlineExtensionsKeepRangesAfterEscapes(t *testing.T) {
	src := "a \\* b $x$ c :smile: d x^2^ e\n"
	doc := MustParse([]byte(src))
	want := map[NodeType]string{NodeMathInline: "$x$", NodeEmoji: ":smile:", NodeSuperscript: "^2^"}
	seen := map[NodeType]bool{}
	doc.Root.Walk(func(n *Node) bool {
		if w, ok := want[n.Type]; ok {
			seen[n.Type] = true
			if got := sourceOf(t, doc, n); got != w {
				t.Errorf("%v covers %q, want %q", n.Type, got, w)
			}
		}
		return true
	})
	for typ := range want {
		if !seen[typ] {
			t.Errorf("no %v node in:\n%s", typ, fullNodeDump(doc.Root))
		}
	}
}

func TestComponentRangeCoversItsClosingTag(t *testing.T) {
	src := "Before.\n<H2 class=\"x\">Extra</H2>\n"
	doc := MustParse([]byte(src))
	comps := doc.Root.Find(NodeComponent)
	if len(comps) != 1 {
		t.Fatalf("%d components:\n%s", len(comps), fullNodeDump(doc.Root))
	}
	if got := sourceOf(t, doc, comps[0]); got != "<H2 class=\"x\">Extra</H2>" {
		t.Errorf("the component covers %q", got)
	}
}

// A synthesized node says so: the table of contents has a range (the
// directive), its generated list does not.
func TestSynthesizedNodesAreMarked(t *testing.T) {
	doc := MustParse([]byte("# T\n\n[[toc]]\n\n## A\n\n### B\n"))
	tocs := doc.Root.Find(NodeTableOfContents)
	if len(tocs) != 1 {
		t.Fatalf("%d tables of contents", len(tocs))
	}
	if got := sourceOf(t, doc, tocs[0]); got != "[[toc]]\n" {
		t.Errorf("the table of contents covers %q", got)
	}
	synthesized := 0
	tocs[0].Walk(func(n *Node) bool {
		if n == tocs[0] {
			return true
		}
		synthesized++
		if n.HasSourceRange() || n.Range != (Range{}) {
			t.Errorf("%v %q in the table of contents has the range %+v", n.Type, n.Literal, n.Range)
		}
		return true
	})
	if synthesized == 0 {
		t.Error("the table of contents has no generated nodes")
	}
	if (&Node{}).HasSourceRange() {
		t.Error("the zero node has a source range")
	}
	var nilNode *Node
	if nilNode.HasSourceRange() {
		t.Error("a nil node has a source range")
	}
}

// Ranges index Document.Source: the input with LF line ends and a final
// newline.
func TestSourceIsTheNormalizedInput(t *testing.T) {
	src := "# Title\r\n\r\n> [!NOTE]\r\nbody\r\n\r\n## Next"
	doc := MustParse([]byte(src))
	want := "# Title\n\n> [!NOTE]\nbody\n\n## Next\n"
	if string(doc.Source) != want {
		t.Fatalf("Source is %q, want %q", doc.Source, want)
	}
	if !doc.SourceHadCarriageReturns() {
		t.Error("SourceHadCarriageReturns is false")
	}
	for _, h := range doc.Root.Find(NodeHeading) {
		if got := sourceOf(t, doc, h); !strings.HasPrefix(got, "#") {
			t.Errorf("heading covers %q", got)
		}
	}
}
