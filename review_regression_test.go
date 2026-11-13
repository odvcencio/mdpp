package mdpp

import "testing"

// Regressions found by the second review of the fast-path pull request.

// A list that starts at 0 keeps start="0" on every path (CommonMark 5.2). The
// full parser dropped the zero because "no numeric marker" and "zero" were
// both an empty string.
func TestOrderedListStartingAtZeroKeepsItsStart(t *testing.T) {
	for _, src := range []string{"***\n0. a\n* b\n", "0. a\n* b\n", "0. a\n", "***\n\n0. a\n1. b\n"} {
		for _, fast := range []bool{true, false} {
			fastBlockChunkDisabled = !fast
			doc := MustParse([]byte(src))
			fastBlockChunkDisabled = false
			lists := doc.Root.Find(NodeList)
			if len(lists) == 0 || lists[0].Attr("ordered") != "true" || lists[0].Attr("start") != "0" {
				t.Errorf("%q (fast=%v): the first list is %+v, want an ordered list with start=0\n%s", src, fast, lists[0].Attrs, fullNodeDump(doc.Root))
			}
		}
	}
	doc := MustParse([]byte("***\n0. a\n* b\n"))
	lists := doc.Root.Find(NodeList)
	if len(lists) != 2 || lists[1].Attr("ordered") == "true" {
		t.Errorf("want an ordered list then a bullet list:\n%s", fullNodeDump(doc.Root))
	}
	if orderedListStart("2.") != "2" || orderedListStart("0.") != "0" || orderedListStart("-") != "" {
		t.Errorf("orderedListStart: 2. -> %q, 0. -> %q, - -> %q", orderedListStart("2."), orderedListStart("0."), orderedListStart("-"))
	}
}

// An unindented ATX heading ends a table for certain (GFM 4.10), so the fast
// path reads the table and the heading; the delimiter-looking row under the
// first body row is a body row.
func TestTableEndsAtATXHeading(t *testing.T) {
	src := "***\n|h|\n|-|\n|h|\n|-|\n# h\n"
	doc := MustParse([]byte(src))
	tables := doc.Root.Find(NodeTable)
	if len(tables) != 1 || len(tables[0].Children) != 3 {
		t.Fatalf("want one table with a header row and two body rows:\n%s", fullNodeDump(doc.Root))
	}
	kids := doc.Root.Children
	if kids[len(kids)-1].Type != NodeHeading {
		t.Errorf("the last block is %v, want a heading", kids[len(kids)-1].Type)
	}
	if _, ok := parseFastBlockChunk([]byte("|h|\n|-|\n|h|\n#x\n"), nil); ok {
		t.Error("'#x' is not a heading, so the next line may be a table row; the chunk must be rejected")
	}
}

// A cell of spaces keeps the span between its pipes.
func TestWhitespaceOnlyTableCellKeepsItsSpan(t *testing.T) {
	src := "***\n|h|\n|-|\n| |\n"
	for _, fast := range []bool{true, false} {
		fastBlockChunkDisabled = !fast
		doc := MustParse([]byte(src))
		fastBlockChunkDisabled = false
		cells := doc.Root.Find(NodeTableCell)
		if len(cells) != 2 {
			t.Fatalf("fast=%v: %d cells", fast, len(cells))
		}
		r := cells[1].Range
		if r.StartByte != 13 || r.EndByte != 14 || r.StartLine != 4 || r.StartCol != 2 || r.EndLine != 4 || r.EndCol != 3 {
			t.Errorf("fast=%v: the space cell has the range %+v, want bytes 13-14 at 4:2-4:3", fast, r)
		}
	}
}

// A list item whose text starts a nested block is not paragraph text.
func TestListItemWithNestedBlockIsNotSimple(t *testing.T) {
	for _, item := range []string{"-", "1) a", "#", "## ", "[a]: /u", "1.", "* b"} {
		src := "***\n- " + item + "\n"
		if _, ok := parseFastBlockChunk([]byte(src[4:]), nil); ok {
			t.Errorf("the fast path accepted %q", src[4:])
		}
		if fast, full := parseWithFastPath(src, true), parseWithFastPath(src, false); fast != full {
			t.Errorf("%q: fast and full readings differ\nfast:\n%s\nfull:\n%s", src, fast, full)
		}
	}
	for _, src := range []string{"- -\n", "- 1) a\n", "- #\n", "- [a]: /u\n"} {
		if _, ok := parseFastBlockChunk([]byte(src), nil); ok {
			t.Errorf("the fast path accepted %q", src)
		}
	}
}

// A closing fence has no text after its marker run.
func TestClosingFenceHasNoTrailingText(t *testing.T) {
	for _, tc := range []struct{ src, literal string }{
		{"***\n~~~\n~~~x\n~~~\n~~~\n", "~~~x\n"},
		{"***\n```\n```x\n```\n", "```x\n"},
		{"***\n~~~\na\n~~~~ \t\n", "a\n"},
	} {
		doc := MustParse([]byte(tc.src))
		blocks := doc.Root.Find(NodeCodeBlock)
		if len(blocks) == 0 || blocks[0].Literal != tc.literal {
			t.Errorf("%q: first code block is %q, want %q\n%s", tc.src, blocks[0].Literal, tc.literal, fullNodeDump(doc.Root))
		}
		if fast, full := parseWithFastPath(tc.src, true), parseWithFastPath(tc.src, false); fast != full {
			t.Errorf("%q: fast and full readings differ\nfast:\n%s\nfull:\n%s", tc.src, fast, full)
		}
	}
}

// Explicit expected trees, written from the specification and not from the
// output of either path, for three classes in which the full path once read a
// document wrongly. Both paths must produce exactly this tree. The
// differential test waives nothing.
func TestSpecificationTreesOnBothPaths(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"breaks after a break are breaks, not front matter (CommonMark 4.1)", "***\n\n***\n\n---\n\n---\n", `Document "" [0-19 1:1-8:1]
 ThematicBreak "" [0-4 1:1-2:1]
 ThematicBreak "" [5-9 3:1-4:1]
 ThematicBreak "" [10-14 5:1-6:1]
 ThematicBreak "" [15-19 7:1-8:1]
`},
		{"a paragraph holding only a link stays a paragraph (CommonMark 4.8)", "***\n\n[link](\\(foo\\))\n", `Document "" [0-21 1:1-4:1]
 ThematicBreak "" [0-4 1:1-2:1]
 Paragraph "" [5-21 3:1-4:1]
  Link href="(foo)" "" [5-20 3:1-3:16]
   Text "link" [6-10 3:2-3:6]
`},
		{"a header-like row and a delimiter row under a table are body rows (GFM 4.10)", "***\n|h|\n|-|\n|h|\n|-|\n# h\n", `Document "" [0-24 1:1-7:1]
 ThematicBreak "" [0-4 1:1-2:1]
 Table align="" "" [4-20 2:1-6:1]
  TableRow "" [4-7 2:1-2:4]
   TableCell "" [5-6 2:2-2:3]
    Text "h" [5-6 2:2-2:3]
  TableRow "" [12-15 4:1-4:4]
   TableCell "" [13-14 4:2-4:3]
    Text "h" [13-14 4:2-4:3]
  TableRow "" [16-19 5:1-5:4]
   TableCell "" [17-18 5:2-5:3]
    Text "-" [17-18 5:2-5:3]
 Heading level="1" "" [20-24 6:1-7:1]
  Text "h" [22-23 6:3-6:4]
`},
	}
	for _, tc := range cases {
		for _, fast := range []bool{true, false} {
			if got := parseWithFastPath(tc.src, fast); got != tc.want {
				t.Errorf("%s: %q (fast path %v)\ngot:\n%swant:\n%s", tc.name, tc.src, fast, got, tc.want)
			}
		}
	}
}

// Quote stripping rewrites indentation tabs as spaces. The block that starts in
// the expanded indentation must start at the tab that made it, and a node
// that would start or end inside the spaces of one tab has no range.
func TestQuoteWithExpandedTabsKeepsExactRanges(t *testing.T) {
	cases := []struct {
		src                        string
		blockStart, blockEnd       int
		startLine, startCol, eLine int
		eCol                       int
	}{
		{">\t\tx\n", 1, 5, 1, 2, 2, 1},
		{"***\n>\t\tx\n", 5, 9, 2, 2, 3, 1},
		{"> \t\tx\n", 2, 6, 1, 3, 2, 1},
	}
	for _, tc := range cases {
		doc := MustParse([]byte(tc.src))
		blocks := doc.Root.Find(NodeCodeBlock)
		if len(blocks) != 1 {
			t.Fatalf("%q: %d code blocks:\n%s", tc.src, len(blocks), fullNodeDump(doc.Root))
		}
		r := blocks[0].Range
		if r.StartByte != tc.blockStart || r.EndByte != tc.blockEnd || r.StartLine != tc.startLine || r.StartCol != tc.startCol || r.EndLine != tc.eLine || r.EndCol != tc.eCol {
			t.Errorf("%q: code block range %+v, want bytes %d-%d, %d:%d-%d:%d", tc.src, r, tc.blockStart, tc.blockEnd, tc.startLine, tc.startCol, tc.eLine, tc.eCol)
		}
	}
}

// The soft break after a paragraph line that a quoted list interrupts covers
// the newline itself.
func TestLazyContainerNewlineRange(t *testing.T) {
	doc := MustParse([]byte("> a\n> - b\nc\n"))
	var soft *Node
	doc.Root.Walk(func(n *Node) bool {
		if n.Type == NodeSoftBreak && soft == nil {
			soft = n
		}
		return true
	})
	if soft == nil {
		t.Fatalf("no soft break:\n%s", fullNodeDump(doc.Root))
	}
	r := soft.Range
	if r.StartByte != 3 || r.EndByte != 4 || r.StartLine != 1 || r.StartCol != 4 || r.EndLine != 2 || r.EndCol != 1 {
		t.Errorf("the soft break has the range %+v, want bytes 3-4 at 1:4-2:1\n%s", r, fullNodeDump(doc.Root))
	}
}

// The byte map of quote stripping.
func TestStripBlockquoteMarkerMap(t *testing.T) {
	// Six spaces from the two tabs, then x.
	content, offs, ok := stripBlockquoteMarkerMap(">\t\tx", true)
	want := []int{1, 1, 2, 2, 2, 2, 3, 4}
	if !ok || content != "      x" || len(offs) != len(want) {
		t.Fatalf("content %q offsets %v", content, offs)
	}
	for i := range want {
		if offs[i] != want[i] {
			t.Errorf("offset %d is %d, want %d (%v)", i, offs[i], want[i], offs)
		}
	}
	for _, in := range []string{"> a", ">  a", ">\ta", "  >\t b", "> \t\ta", ">>\tb", ">"} {
		c1, ok1 := stripBlockquoteMarker(in)
		c2, offs, ok2 := stripBlockquoteMarkerMap(in, true)
		if c1 != c2 || ok1 != ok2 || (ok2 && len(offs) != len(c2)+1) {
			t.Errorf("%q: stripBlockquoteMarker %q, map version %q with %d offsets", in, c1, c2, len(offs))
		}
	}
}

// Merged text keeps the undecoded source, so that splitting at line ends and
// mapping offsets still see the source bytes.
func TestMergedTextKeepsItsRawSource(t *testing.T) {
	var nodes []*Node
	appendTextRange(&nodes, "&amp;", sourceRange([]byte("&amp;!\n"), 0, 5))
	appendTextRange(&nodes, "!", sourceRange([]byte("&amp;!\n"), 5, 6))
	if len(nodes) != 1 || nodes[0].Literal != "&!" || nodes[0].raw != "&amp;!" {
		t.Fatalf("merged node: %+v", nodes[0])
	}
	if nodes[0].Range.StartByte != 0 || nodes[0].Range.EndByte != 6 {
		t.Errorf("merged range %+v", nodes[0].Range)
	}
	var plain []*Node
	appendTextRange(&plain, "a", sourceRange([]byte("ab"), 0, 1))
	appendTextRange(&plain, "b", sourceRange([]byte("ab"), 1, 2))
	if plain[0].Literal != "ab" || plain[0].raw != "" {
		t.Errorf("plain merge: %+v", plain[0])
	}
}

// Both clone functions of the parse cache keep the raw source of a text node.
func TestCacheClonesKeepRawSource(t *testing.T) {
	root := &Node{Type: NodeParagraph, Children: []*Node{{Type: NodeText, Literal: "&", raw: "&amp;", Range: sourceRange([]byte("&amp;\n"), 0, 5)}}}
	for name, c := range map[string]*Node{
		"cloneAndShift":         cloneAndShift(root, 0, nil),
		"cloneAndShiftAnchored": cloneAndShiftAnchored(root, 0, 1, 1, 1, 1),
		"cloneForCache":         cloneForCache(root, 0),
	} {
		if c.Children[0].raw != "&amp;" {
			t.Errorf("%s dropped raw: %q", name, c.Children[0].raw)
		}
	}
}
