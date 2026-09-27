package mdpp

import (
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func TestEscapedDestinationLinkParagraphRecovers(t *testing.T) {
	source := "[docs](https://example.com/a\\_b) more text\n"
	doc, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Root.Find(NodeParagraph)) != 1 {
		t.Fatalf("expected one paragraph, got %d", len(doc.Root.Find(NodeParagraph)))
	}
	if len(doc.Root.Find(NodeLink)) != 1 {
		t.Fatal("escaped destination was not recovered as a link")
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "docs</a> more text") {
		t.Fatalf("render lost the link or trailing words: %s", rendered)
	}
	if !strings.Contains(string(rendered), `href="https://example.com/a_b"`) {
		t.Fatalf("escaped punctuation was not removed from the link destination: %s", rendered)
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-006" {
			t.Fatalf("targeted recovery should not report an unrecovered error: %+v", diagnostic)
		}
	}
}

func TestChainedReferenceLinksRemainLinked(t *testing.T) {
	source := "before [first][one][second] after\n\n[one]: /one\n[second]: /two\n"
	doc, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	want := `before <a href="/one">first</a><a href="/two">second</a> after`
	if !strings.Contains(string(rendered), want) {
		t.Fatalf("chained reference links did not render as adjacent links: %s", rendered)
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-006" {
			t.Fatalf("valid chained references were reported as malformed: %+v", diagnostic)
		}
	}
}

func TestUnresolvedChainedReferenceLinksRemainLiteral(t *testing.T) {
	doc, err := Parse([]byte("[first][one][second]\n"))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "[first][one][second]") {
		t.Fatalf("unresolved chained references lost their source syntax: %s", rendered)
	}
}

func TestChainedReferenceSyntaxInsideCodeSpanStaysLiteralCode(t *testing.T) {
	doc, err := Parse([]byte("`[first][one][second]`\n"))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "<code>[first][one][second]</code>") {
		t.Fatalf("reference syntax inside a code span was parsed as links: %s", rendered)
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-006" {
			t.Fatalf("literal code syntax was reported as malformed: %+v", diagnostic)
		}
	}
}

func TestInlineLinkSyntaxInsideCodeSpanStaysCode(t *testing.T) {
	doc, err := Parse([]byte("`[x](url)`\n"))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "<code>[x](url)</code>") {
		t.Fatalf("inline-link syntax inside a code span lost code formatting: %s", rendered)
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-006" {
			t.Fatalf("literal code syntax was reported as malformed: %+v", diagnostic)
		}
	}
}

func TestLinkSyntaxCrossingCodeSpanBoundariesStaysCode(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{
			source: "[not a `link](/foo`)\n",
			want:   "<p>[not a <code>link](/foo</code>)</p>",
		},
		{
			source: "[foo`](/uri)`\n",
			want:   "<p>[foo<code>](/uri)</code></p>",
		},
	}
	for _, test := range tests {
		doc, err := Parse([]byte(test.source))
		if err != nil {
			t.Fatal(err)
		}
		rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
		if err != nil {
			t.Fatal(err)
		}
		if got := normalizeSpecHTML(string(rendered)); got != test.want {
			t.Errorf("rendered %q as %q, want %q", test.source, got, test.want)
		}
	}
}

func TestBlockquoteNestingThroughDepth32(t *testing.T) {
	for depth := 1; depth <= 32; depth++ {
		t.Run(levelStr(depth), func(t *testing.T) {
			source := strings.Repeat("> ", depth) + "quote text\n"
			doc, err := Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			if got := countNodes(doc.Root, NodeBlockquote); got != depth {
				t.Fatalf("blockquote depth = %d, want %d", got, depth)
			}
			rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(rendered), "quote text") {
				t.Fatalf("render lost quoted text at depth %d: %s", depth, rendered)
			}
		})
	}
}

func TestMultilineSetextHeadingRetainsEveryLine(t *testing.T) {
	doc, err := Parse([]byte("Foo\nBar\n---\n"))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "<h2>Foo\nBar</h2>") {
		t.Fatalf("multiline setext heading lost its second line: %s", rendered)
	}
}

func TestErrorBlockFallbackPreservesTextAndDiagnostic(t *testing.T) {
	source := "[a](url &quot;tit&quot;)\n"
	doc, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "[a](url") || !strings.Contains(string(rendered), "&amp;quot;") {
		t.Fatalf("error fallback did not render the original text: %s", rendered)
	}
	assertParseDiagnostic(t, doc, "MDPP-PARSE-006")
}

func TestMalformedInlineLinkFallbackHasSourceRange(t *testing.T) {
	source := "[link](foo\nbar)"
	ctx := &parseCtx{}
	children := parseInlineAt(source, []byte(source), 0, ctx)
	doc := &Document{
		Root:        &Node{Type: NodeDocument, Children: []*Node{{Type: NodeParagraph, Children: children}}},
		Source:      []byte(source),
		diagnostics: ctx.recoveryDiagnostics,
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "[link](foo") || !strings.Contains(string(rendered), "bar)") {
		t.Fatalf("malformed inline link source was not preserved: %s", rendered)
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-006" {
			if diagnostic.Severity != SeverityWarning || diagnostic.Range.StartByte != 0 || diagnostic.Range.EndByte != len(source) {
				t.Fatalf("unexpected MDPP-PARSE-006 diagnostic: %+v", diagnostic)
			}
			return
		}
	}
	t.Fatalf("missing MDPP-PARSE-006 diagnostic; got %+v", doc.Diagnostics())
}

func TestInlineErrorNodeFallsBackToSourceWithDiagnostic(t *testing.T) {
	source := `<a href="x"`
	tree, timedOut, err := parsePooledInline([]byte(source))
	if err != nil || tree == nil || timedOut {
		t.Fatalf("inline error fixture did not parse fully: tree=%v timedOut=%t err=%v", tree != nil, timedOut, err)
	}
	if errors := inlineErrorNodes(gotreesitter.Bind(tree).RootNode()); len(errors) == 0 {
		tree.Release()
		t.Fatal("inline error fixture no longer produces a tree-sitter ERROR or MISSING node")
	}
	tree.Release()
	ctx := &parseCtx{}
	children := parseInlineAt(source, []byte(source), 0, ctx)
	doc := &Document{
		Root:        &Node{Type: NodeDocument, Children: []*Node{{Type: NodeParagraph, Children: children}}},
		Source:      []byte(source),
		diagnostics: ctx.recoveryDiagnostics,
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), `&lt;a href=&#34;x&#34;`) {
		t.Fatalf("inline ERROR source was not preserved: %s", rendered)
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-006" {
			if diagnostic.Severity != SeverityWarning || diagnostic.Range.StartByte != 0 || diagnostic.Range.EndByte != len(source) {
				t.Fatalf("unexpected inline MDPP-PARSE-006 diagnostic: %+v", diagnostic)
			}
			return
		}
	}
	t.Fatalf("missing MDPP-PARSE-006 diagnostic; got %+v", doc.Diagnostics())
}

func TestLongLineFallsBackWithinItsTopLevelBlock(t *testing.T) {
	dataURI := "![pixel](data:image/png;base64," + strings.Repeat("A", maxGLRLineBytes+100) + ")"
	source := "# Before\n\n" + dataURI + "\n\n# After\n\nTail paragraph\n"
	doc, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, visible := range []string{"<h1>Before</h1>", "<h1>After</h1>", "Tail paragraph", "![pixel]"} {
		if !strings.Contains(string(rendered), visible) {
			t.Fatalf("render is missing %q around long-line fallback: %s", visible, rendered[:min(len(rendered), 500)])
		}
	}
	assertParseDiagnostic(t, doc, "MDPP-PARSE-004")
}

func TestDepthFallbackRendersEscapedParagraphText(t *testing.T) {
	source := []byte("raw <fallback> & text\n")
	ctx := &parseCtx{containerDepth: maxContainerDepth}
	doc, _ := parseDocumentRetainTreeCtx(source, nil, ctx)
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rendered), "raw &lt;fallback&gt; &amp; text") {
		t.Fatalf("depth fallback did not render escaped source text: %s", rendered)
	}
	assertParseDiagnostic(t, doc, "MDPP-PARSE-003")
}

func TestContainerDepthFallbackKeepsSurroundingBlocks(t *testing.T) {
	const depth = 16
	source := "# Before\n\n" + strings.Repeat("::: note\n", depth) +
		"inside text\n" + strings.Repeat(":::\n", depth) + "\n# After\n\nTail text\n"
	doc, err := Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"<h1>Before</h1>", "inside text", "<h1>After</h1>", "Tail text"} {
		if !strings.Contains(string(rendered), text) {
			t.Errorf("depth fallback lost %q: %s", text, rendered)
		}
	}
	assertParseDiagnostic(t, doc, "MDPP-PARSE-003")
}

func TestFallbackParagraphLiteralRendersForEveryBudgetCode(t *testing.T) {
	for _, code := range []string{"MDPP-PARSE-003", "MDPP-PARSE-004", "MDPP-PARSE-005"} {
		t.Run(code, func(t *testing.T) {
			doc := &Document{
				Root:        &Node{Type: NodeDocument, Children: []*Node{{Type: NodeParagraph, Literal: "source <text> & more"}}},
				diagnostics: []Diagnostic{{Code: code, Severity: SeverityWarning}},
			}
			rendered, err := Render(doc, RenderOptions{HeadingIDs: false})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(rendered), "source &lt;text&gt; &amp; more") {
				t.Fatalf("%s fallback literal rendered blank or unescaped: %s", code, rendered)
			}
		})
	}
}

func TestTrustedRawHTMLPassesThroughUnchanged(t *testing.T) {
	block := `<iframe src="https://m31labs.dev/embed"></iframe>` + "\n"
	inline := `<style>.trusted{color:red}</style>`
	doc := &Document{Root: &Node{Type: NodeDocument, Children: []*Node{
		{Type: NodeHTMLBlock, Literal: block},
		{Type: NodeParagraph, Children: []*Node{{Type: NodeHTMLInline, Literal: inline}}},
	}}}

	rendered, err := Render(doc, RenderOptions{
		UnsafeHTML: true,
		Sanitize:   false,
		HeadingIDs: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := block + "<p>" + inline + "</p>\n"
	if string(rendered) != want {
		t.Fatalf("trusted HTML changed during rendering:\n got: %q\nwant: %q", rendered, want)
	}
}

func assertParseDiagnostic(t *testing.T, doc *Document, code string) {
	t.Helper()
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("missing %s diagnostic; got %+v", code, doc.Diagnostics())
}
