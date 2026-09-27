package mdpp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseWorkBudgetReturnsSourceAsText(t *testing.T) {
	source := []byte("# Visible source\n\nThis *text* must survive a work-budget fallback.\n")
	doc, err := ParseWithOptions(source, ParseOptions{WorkBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	if doc == nil || doc.Root == nil {
		t.Fatal("budgeted parse returned no document")
	}
	if got := collectText(doc.Root); got != string(source) {
		t.Fatalf("fallback text = %q, want source %q", got, source)
	}
	diagnostics := doc.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "MDPP-PARSE-005" || diagnostics[0].Severity != SeverityWarning {
		t.Fatalf("budget diagnostics = %#v, want one MDPP-PARSE-005 warning", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "parser work budget") {
		t.Fatalf("budget diagnostic does not name the hit: %q", diagnostics[0].Message)
	}
	rendered := NewRenderer().Render(doc)
	if !strings.Contains(rendered, "# Visible source") || !strings.Contains(rendered, "*text*") {
		t.Fatalf("fallback did not render the source as text: %q", rendered)
	}
}

func TestParseContextDeadlineReturnsSourceAsText(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	source := []byte("A cancelled parse still returns all source text.\n")
	doc, err := ParseContext(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if got := collectText(doc.Root); got != string(source) {
		t.Fatalf("cancelled parse text = %q, want source %q", got, source)
	}
	diagnostics := doc.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "MDPP-PARSE-005" || !strings.Contains(diagnostics[0].Message, "context deadline") {
		t.Fatalf("deadline diagnostic = %#v", diagnostics)
	}
}

func TestDefaultParseDeadlineUsesLongBackstop(t *testing.T) {
	const oneMiB = 1 << 20
	for _, test := range []struct {
		bytes int
		want  time.Duration
	}{
		{bytes: 0, want: 20 * time.Second},
		{bytes: 1, want: 20*time.Second + 10*time.Second/time.Duration(oneMiB)},
		{bytes: oneMiB, want: 30 * time.Second},
		{bytes: 2 * oneMiB, want: 40 * time.Second},
	} {
		if got := defaultParseDeadline(test.bytes); got != test.want {
			t.Errorf("defaultParseDeadline(%d) = %s, want %s", test.bytes, got, test.want)
		}
	}
}

func TestShortParseOptionsDeadlineIsNotRoundedUp(t *testing.T) {
	const targetBytes = 16_000
	unit := `a*b_[c](d){e}"f"-->`
	longLine := strings.Repeat(unit, targetBytes/len(unit)+1)[:targetBytes]
	source := []byte("# Synthetic input\n\n" + longLine + "\n")
	started := time.Now()
	doc, err := ParseWithOptions(source, ParseOptions{Deadline: 50 * time.Millisecond})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("short-deadline parse took %s, want under 2s", elapsed)
	}
	if doc == nil || doc.Root == nil {
		t.Fatal("short-deadline parse returned no document")
	}
	diagnostics := doc.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "MDPP-PARSE-005" {
		t.Fatalf("short-deadline diagnostics = %#v, want one MDPP-PARSE-005 warning", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "wall-clock deadline") {
		t.Fatalf("short-deadline diagnostic = %q, want wall-clock deadline", diagnostics[0].Message)
	}
	if got := collectText(doc.Root); got != string(source) {
		t.Fatalf("fallback text length = %d, want original source length %d", len(got), len(source))
	}
}

func TestParsePathologicalInlineStopsOnWorkBudget(t *testing.T) {
	const targetBytes = 12_000
	unit := `a*b_[c](d){e}"f"-->`
	longLine := strings.Repeat(unit, targetBytes/len(unit)+1)[:targetBytes]
	source := []byte("# Synthetic input\n\n" + longLine + "\n")
	// Reserve enough work for the block parse, then require the inline parse to
	// stop at the deterministic work limit.
	workBudget := 2 * parserWorkEstimate(len(source)+1)
	start := time.Now()
	doc, err := ParseWithOptions(source, ParseOptions{WorkBudget: workBudget})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("pathological input took %s, want under 5s", elapsed)
	}
	if doc == nil || doc.Root == nil {
		t.Fatal("pathological parse returned no document")
	}
	diagnostics := doc.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != "MDPP-PARSE-005" || diagnostics[0].Severity != SeverityWarning {
		t.Fatalf("work-budget diagnostics = %#v, want one MDPP-PARSE-005 warning", diagnostics)
	}
	if !strings.Contains(diagnostics[0].Message, "parser work budget") {
		t.Fatalf("diagnostic = %q, want parser work budget", diagnostics[0].Message)
	}
	if got := collectText(doc.Root); got != string(source) {
		t.Fatalf("fallback text length = %d, want original source length %d", len(got), len(source))
	}
}

func TestContainerChunkRangesUseOuterSourcePositions(t *testing.T) {
	source := []byte("before container\n\n:::warning\nbody with *inline* text\n:::\nafter container\n")
	doc, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	var container *Node
	var find func(*Node)
	find = func(node *Node) {
		if node == nil {
			return
		}
		if node.Type == NodeContainerDirective {
			container = node
		}
		for _, child := range node.Children {
			find(child)
		}
	}
	find(doc.Root)
	if container == nil || len(container.Children) == 0 {
		t.Fatal("container directive body was not parsed")
	}
	if got := container.Children[0].Range.StartLine; got != 4 {
		t.Fatalf("container body starts on line %d, want line 4", got)
	}
}

func TestSegmentedZeroStartOrderedList(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("testdata", "parse", "segmented-zero-start-list.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(source) < segmentedDocumentMinBytes || len(topLevelHeadingChunks(source)) < 2 {
		t.Fatalf("fixture must take the segmented path; bytes=%d", len(source))
	}

	start := time.Now()
	doc, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("segmented zero-start list took %s, want under 1s", elapsed)
	}
	if doc == nil || doc.Root == nil {
		t.Fatal("Parse returned no document")
	}
	var list *Node
	var walk func(*Node)
	walk = func(node *Node) {
		if node == nil {
			return
		}
		if node.Type == NodeList && node.Attrs["ordered"] == "true" {
			list = node
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(doc.Root)
	if list == nil {
		t.Fatal("segmented parse did not preserve the ordered list")
	}
	listOffset := list.Range.StartByte
	prefix := source[:listOffset]
	wantLine := bytes.Count(prefix, []byte{'\n'}) + 1
	wantCol := listOffset - bytes.LastIndex(prefix, []byte{'\n'})
	if list.Range.StartLine != wantLine || list.Range.StartCol != wantCol {
		t.Fatalf("segmented list starts at %d:%d, want %d:%d", list.Range.StartLine, list.Range.StartCol, wantLine, wantCol)
	}
	if got := list.Attrs["start"]; got != "0" {
		t.Fatalf("ordered list start = %q, want 0", got)
	}
	if len(list.Children) != 2 {
		t.Fatalf("ordered list items = %d, want 2", len(list.Children))
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-005" {
			t.Fatalf("ordinary synthetic list hit a parse budget: %+v", diagnostic)
		}
	}
}

func TestFastListParserAdvancesPastLargeOrderedMarkers(t *testing.T) {
	marker := strings.Repeat("9", 64)
	source := []byte(marker + ". item\n")
	lines := sourceLines(source)
	list, next := fastListNode(source, lines, 0, &parseCtx{})
	if next != len(lines) {
		t.Fatalf("large ordered marker consumed %d lines, want %d", next, len(lines))
	}
	if list == nil || list.Attrs["ordered"] != "true" || list.Attrs["start"] != marker {
		t.Fatalf("large ordered list = %#v, want ordered start %q", list, marker)
	}
}

func TestParse10000ShortParagraphsTerminates(t *testing.T) {
	source := makeShortParagraphDocument(10_000)
	start := time.Now()
	// Race instrumentation and package parallelism add substantial parser
	// overhead. Keep this completion check separate from the measured speed
	// target recorded in the bounded-parse evidence.
	doc, err := ParseWithOptions(source, ParseOptions{Deadline: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if doc == nil || doc.Root == nil {
		t.Fatal("Parse returned no document")
	}
	if elapsed := time.Since(start); elapsed > time.Minute {
		t.Fatalf("10,000 short paragraphs took %s, want under 1m", elapsed)
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-005" {
			t.Fatalf("ordinary generated document hit a parse budget: %+v", diagnostic)
		}
	}
	paragraphs := 0
	inlineNodes := map[NodeType]int{}
	var countParagraphs func(*Node)
	countParagraphs = func(node *Node) {
		if node == nil {
			return
		}
		if node.Type == NodeParagraph {
			paragraphs++
		}
		if node.Type == NodeEmphasis || node.Type == NodeCodeSpan || node.Type == NodeLink {
			inlineNodes[node.Type]++
		}
		for _, child := range node.Children {
			countParagraphs(child)
		}
	}
	countParagraphs(doc.Root)
	if paragraphs != 10_000 {
		t.Fatalf("parsed paragraphs = %d, want 10,000", paragraphs)
	}
	for _, typ := range []NodeType{NodeEmphasis, NodeCodeSpan, NodeLink} {
		if inlineNodes[typ] != 10_000 {
			t.Fatalf("parsed nodes of type %d = %d, want 10,000", typ, inlineNodes[typ])
		}
	}
}

func TestLiteralContainerMarkerDoesNotTriggerContainerReparse(t *testing.T) {
	source := []byte("# Synthetic marker\n\n" + strings.Repeat("Prose mentions ::: without opening a container.\n\n", 120) + "\n```text\n:::\n```\n")
	start := time.Now()
	doc, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("literal container marker took %s, want under 1s", elapsed)
	}
	if doc == nil || doc.Root == nil {
		t.Fatal("Parse returned no document")
	}
	var containers int
	var countContainers func(*Node)
	countContainers = func(node *Node) {
		if node == nil {
			return
		}
		if node.Type == NodeContainerDirective {
			containers++
		}
		for _, child := range node.Children {
			countContainers(child)
		}
	}
	countContainers(doc.Root)
	if containers != 0 {
		t.Fatalf("literal prose marker created %d container directives", containers)
	}
}

func makeShortParagraphDocument(count int) []byte {
	var source strings.Builder
	for i := 0; i < count; i++ {
		source.WriteString("Short paragraph with *emphasis*, a `code span`, and a [link](https://example.com/path).\n\n")
	}
	return []byte(source.String())
}

func TestParseHeading(t *testing.T) {
	doc := MustParse([]byte("# Hello"))
	root := doc.Root
	if root.Type != NodeDocument {
		t.Fatalf("expected NodeDocument, got %d", root.Type)
	}
	if len(root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	if h.Attrs["level"] != "1" {
		t.Fatalf("expected level 1, got %q", h.Attrs["level"])
	}
	if len(h.Children) == 0 {
		t.Fatal("expected heading to have text children")
	}
	text := collectText(h)
	if text != "Hello" {
		t.Fatalf("expected text %q, got %q", "Hello", text)
	}
}

func TestParseHeadingLevels(t *testing.T) {
	tests := []struct {
		src   string
		level string
		text  string
	}{
		{"## Second", "2", "Second"},
		{"### Third", "3", "Third"},
		{"#### Fourth", "4", "Fourth"},
		{"##### Fifth", "5", "Fifth"},
		{"###### Sixth", "6", "Sixth"},
	}
	for _, tt := range tests {
		doc := MustParse([]byte(tt.src))
		if len(doc.Root.Children) == 0 {
			t.Fatalf("no children for %q", tt.src)
		}
		h := doc.Root.Children[0]
		if h.Type != NodeHeading {
			t.Fatalf("expected NodeHeading for %q, got %d", tt.src, h.Type)
		}
		if h.Attrs["level"] != tt.level {
			t.Errorf("expected level %q for %q, got %q", tt.level, tt.src, h.Attrs["level"])
		}
		text := collectText(h)
		if text != tt.text {
			t.Errorf("expected text %q for %q, got %q", tt.text, tt.src, text)
		}
	}
}

func TestParseHeadingWithExclamation(t *testing.T) {
	doc := MustParse([]byte("# Hello World!"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := doc.Root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	text := collectText(h)
	if text != "Hello World!" {
		t.Fatalf("expected text %q, got %q", "Hello World!", text)
	}
}

func TestParseHeadingWithTerminalPunctuationInLongDocument(t *testing.T) {
	src := []byte(`# Hello World!

A matter of origin, this didn't really exist before February 19th, 2026. That was the evening I would conceive of gotreesitter[^1]. Less than a week or so later, I would foolishly declare it ported and solved to HackerNews[^2] to mixed-ish reviews. I think the general sentiment was that maybe this could be useful to *someone*, *somewhere*, *eventually*-- but that this was simply too immature, too vibe-coded, and too much maintenance burden for a single person.

[^1]: [gotreesitter](https://github.com/odvcencio/gotreesitter)
[^2]: [Disaster, but lucky still](https://link-to-article)
`)

	doc := MustParse(src)
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	h := doc.Root.Children[0]
	if h.Type != NodeHeading {
		t.Fatalf("expected NodeHeading, got %d", h.Type)
	}
	if text := collectText(h); text != "Hello World!" {
		t.Fatalf("expected heading text %q, got %q", "Hello World!", text)
	}
}

func TestParseNodeRanges(t *testing.T) {
	src := []byte("# Hello\n\nA **bold** move\n")
	doc := MustParse(src)

	assertRange(t, "document", doc.Root.Range, Range{StartByte: 0, EndByte: len(src), StartLine: 1, StartCol: 1, EndLine: 4, EndCol: 1})
	if len(doc.Root.Children) < 2 {
		t.Fatalf("expected heading and paragraph, got %d children", len(doc.Root.Children))
	}

	heading := doc.Root.Children[0]
	assertRange(t, "heading", heading.Range, Range{StartByte: 0, EndByte: 8, StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 1})
	if len(heading.Children) != 1 {
		t.Fatalf("expected one heading child, got %d", len(heading.Children))
	}
	assertRange(t, "heading text", heading.Children[0].Range, Range{StartByte: 2, EndByte: 7, StartLine: 1, StartCol: 3, EndLine: 1, EndCol: 8})

	paragraph := doc.Root.Children[1]
	assertRange(t, "paragraph", paragraph.Range, Range{StartByte: 9, EndByte: 25, StartLine: 3, StartCol: 1, EndLine: 4, EndCol: 1})

	var strong *Node
	for _, child := range paragraph.Children {
		if child.Type == NodeStrong {
			strong = child
			break
		}
	}
	if strong == nil {
		t.Fatal("expected strong node")
	}
	assertRange(t, "strong", strong.Range, Range{StartByte: 11, EndByte: 19, StartLine: 3, StartCol: 3, EndLine: 3, EndCol: 11})
	if len(strong.Children) != 1 {
		t.Fatalf("expected one strong child, got %d", len(strong.Children))
	}
	assertRange(t, "strong text", strong.Children[0].Range, Range{StartByte: 13, EndByte: 17, StartLine: 3, StartCol: 5, EndLine: 3, EndCol: 9})
}

func TestParseSoftBreakRange(t *testing.T) {
	doc := MustParse([]byte("one\ntwo\n"))
	if len(doc.Root.Children) != 1 {
		t.Fatalf("expected one paragraph, got %d", len(doc.Root.Children))
	}
	para := doc.Root.Children[0]
	if len(para.Children) != 3 {
		t.Fatalf("expected text, soft break, text; got %d children", len(para.Children))
	}
	assertRange(t, "first text", para.Children[0].Range, Range{StartByte: 0, EndByte: 3, StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 4})
	assertRange(t, "soft break", para.Children[1].Range, Range{StartByte: 3, EndByte: 4, StartLine: 1, StartCol: 4, EndLine: 2, EndCol: 1})
	assertRange(t, "second text", para.Children[2].Range, Range{StartByte: 4, EndByte: 7, StartLine: 2, StartCol: 1, EndLine: 2, EndCol: 4})
}

func TestPostProcessedNodeRanges(t *testing.T) {
	doc := MustParse([]byte("Nice :sweat_smile: and $x$.\n"))
	emoji := findFirstNodeOfType(doc.Root, NodeEmoji)
	if emoji == nil {
		t.Fatal("expected emoji node")
	}
	assertRange(t, "emoji", emoji.Range, Range{StartByte: 5, EndByte: 18, StartLine: 1, StartCol: 6, EndLine: 1, EndCol: 19})

	math := findFirstNodeOfType(doc.Root, NodeMathInline)
	if math == nil {
		t.Fatal("expected inline math node")
	}
	assertRange(t, "inline math", math.Range, Range{StartByte: 23, EndByte: 26, StartLine: 1, StartCol: 24, EndLine: 1, EndCol: 27})
}

func TestDirectiveNodeRanges(t *testing.T) {
	tocDoc := MustParse([]byte("# A\n\n[[toc]]\n"))
	toc := findFirstNodeOfType(tocDoc.Root, NodeTableOfContents)
	if toc == nil {
		t.Fatal("expected TOC node")
	}
	assertRange(t, "toc", toc.Range, Range{StartByte: 5, EndByte: 13, StartLine: 3, StartCol: 1, EndLine: 4, EndCol: 1})

	embedDoc := MustParse([]byte("[[embed:https://example.com/video]]\n"))
	embed := findFirstNodeOfType(embedDoc.Root, NodeAutoEmbed)
	if embed == nil {
		t.Fatal("expected auto embed node")
	}
	assertRange(t, "embed", embed.Range, Range{StartByte: 0, EndByte: 36, StartLine: 1, StartCol: 1, EndLine: 2, EndCol: 1})
}

func TestContainerDirectiveParseAttrsAndNestedContent(t *testing.T) {
	src := []byte(":::warning \"Heads up\" {.extra #warn audience=\"dev\"}\nBody **bold**.\n:::\n")
	doc := MustParse(src)
	container := findFirstNodeOfType(doc.Root, NodeContainerDirective)
	if container == nil {
		t.Fatal("expected container directive")
	}
	if got := container.Attr("name"); got != "warning" {
		t.Fatalf("name = %q, want warning", got)
	}
	if got := container.Attr("title"); got != "Heads up" {
		t.Fatalf("title = %q, want Heads up", got)
	}
	if got := container.Attr("class"); got != "extra" {
		t.Fatalf("class = %q, want extra", got)
	}
	if got := container.Attr("id"); got != "warn" {
		t.Fatalf("id = %q, want warn", got)
	}
	if got := container.Attr("attrs"); !strings.Contains(got, `"audience":"dev"`) {
		t.Fatalf("attrs = %q, want audience JSON", got)
	}
	assertRange(t, "container", container.Range, Range{StartByte: 0, EndByte: len(src), StartLine: 1, StartCol: 1, EndLine: 4, EndCol: 1})
	if findFirstNodeOfType(container, NodeStrong) == nil {
		t.Fatal("expected nested inline markdown to be parsed")
	}
}

func TestContainerDirectiveUnquotesCanonicalTitleEscapes(t *testing.T) {
	doc := MustParse([]byte(":::note \"line\\n\\x84\"\n"))
	container := findFirstNodeOfType(doc.Root, NodeContainerDirective)
	if container == nil {
		t.Fatal("expected container directive")
	}
	if got, want := container.Attr("title"), "line\n\x84"; got != want {
		t.Fatalf("title = %q, want %q", got, want)
	}
}

func TestContainerDirectiveUnclosedDiagnostic(t *testing.T) {
	doc := MustParse([]byte(":::note\nBody\n"))
	if len(doc.Diagnostics()) != 1 {
		t.Fatalf("expected one diagnostic, got %#v", doc.Diagnostics())
	}
	if got := doc.Diagnostics()[0].Code; got != "MDPP-PARSE-002" {
		t.Fatalf("diagnostic code = %q, want MDPP-PARSE-002", got)
	}
}

func TestParseParagraph(t *testing.T) {
	doc := MustParse([]byte("Just some plain text."))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	if p.Type != NodeParagraph {
		t.Fatalf("expected NodeParagraph, got %d", p.Type)
	}
	text := collectText(p)
	if text != "Just some plain text." {
		t.Fatalf("expected %q, got %q", "Just some plain text.", text)
	}
}

func TestParseCodeBlock(t *testing.T) {
	src := "```go\nfmt.Println(\"hello\")\n```"
	doc := MustParse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	cb := doc.Root.Children[0]
	if cb.Type != NodeCodeBlock {
		t.Fatalf("expected NodeCodeBlock, got %d", cb.Type)
	}
	if cb.Attrs["language"] != "go" {
		t.Fatalf("expected language %q, got %q", "go", cb.Attrs["language"])
	}
	if cb.Literal == "" {
		t.Fatal("expected non-empty code literal")
	}
}

func TestParseDiagramFence(t *testing.T) {
	src := "```mermaid\nflowchart TD\n  A --> B\n```"
	doc := MustParse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	diagram := doc.Root.Children[0]
	if diagram.Type != NodeDiagram {
		t.Fatalf("expected NodeDiagram, got %d", diagram.Type)
	}
	if diagram.Attrs["syntax"] != "mermaid" {
		t.Fatalf("expected syntax %q, got %q", "mermaid", diagram.Attrs["syntax"])
	}
	if diagram.Attrs["kind"] != "flowchart" {
		t.Fatalf("expected kind %q, got %q", "flowchart", diagram.Attrs["kind"])
	}
	if !strings.Contains(diagram.Literal, "A --> B") {
		t.Fatalf("expected diagram literal to contain edge, got %q", diagram.Literal)
	}
}

func TestParseDiagramAliasFence(t *testing.T) {
	doc := MustParse([]byte("```erd\nUser ||--o{ Post : writes\n```"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	diagram := doc.Root.Children[0]
	if diagram.Type != NodeDiagram {
		t.Fatalf("expected NodeDiagram, got %d", diagram.Type)
	}
	if diagram.Attrs["kind"] != "erd" {
		t.Fatalf("expected kind %q, got %q", "erd", diagram.Attrs["kind"])
	}
}

func TestParseBoldItalic(t *testing.T) {
	doc := MustParse([]byte("**bold** and *italic*"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	if p.Type != NodeParagraph {
		t.Fatalf("expected NodeParagraph, got %d", p.Type)
	}

	var foundStrong, foundEmphasis bool
	for _, c := range p.Children {
		if c.Type == NodeStrong {
			foundStrong = true
			text := collectText(c)
			if text != "bold" {
				t.Errorf("expected strong text %q, got %q", "bold", text)
			}
		}
		if c.Type == NodeEmphasis {
			foundEmphasis = true
			text := collectText(c)
			if text != "italic" {
				t.Errorf("expected emphasis text %q, got %q", "italic", text)
			}
		}
	}
	if !foundStrong {
		t.Error("expected NodeStrong child")
	}
	if !foundEmphasis {
		t.Error("expected NodeEmphasis child")
	}
}

func TestParseList(t *testing.T) {
	doc := MustParse([]byte("- one\n- two"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	list := doc.Root.Children[0]
	if list.Type != NodeList {
		t.Fatalf("expected NodeList, got %d", list.Type)
	}
	if len(list.Children) < 2 {
		t.Fatalf("expected at least 2 list items, got %d", len(list.Children))
	}
	for _, item := range list.Children {
		if item.Type != NodeListItem {
			t.Errorf("expected NodeListItem, got %d", item.Type)
		}
	}
}

func TestParseOrderedListMarkers(t *testing.T) {
	for _, src := range []string{"1. one\n2. two", "1) one\n2) two"} {
		doc := MustParse([]byte(src))
		if len(doc.Root.Children) == 0 {
			t.Fatalf("%q: expected at least one child", src)
		}
		list := doc.Root.Children[0]
		if list.Type != NodeList {
			t.Fatalf("%q: expected NodeList, got %d", src, list.Type)
		}
		if list.Attrs["ordered"] != "true" {
			t.Fatalf("%q: expected ordered list attrs, got %#v", src, list.Attrs)
		}
	}
}

func TestParseOrderedListStart(t *testing.T) {
	doc := MustParse([]byte("3) three\n4) four"))
	list := doc.Root.Children[0]
	if list.Attrs["ordered"] != "true" {
		t.Fatalf("expected ordered list attrs, got %#v", list.Attrs)
	}
	if list.Attrs["start"] != "3" {
		t.Fatalf("expected start=3, got %#v", list.Attrs)
	}
}

func TestParseBlockquote(t *testing.T) {
	doc := MustParse([]byte("> quote"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	bq := doc.Root.Children[0]
	if bq.Type != NodeBlockquote {
		t.Fatalf("expected NodeBlockquote, got %d", bq.Type)
	}
	text := collectText(bq)
	if text != "quote" {
		t.Fatalf("expected %q, got %q", "quote", text)
	}
}

func TestRenderLongSimpleBlockquote(t *testing.T) {
	source := `> At the time of this writing, gotreesitter[^1] has 457 stars and counting in 2 months of life and has a serious infrastructure shape. It is parity harness and benchmark comparisons keep it honest and steered towards a north star. There is no way to one-shot gotreesitter[^1] with a prompt.`
	html := NewRenderer().RenderString(source)

	assertContains(t, html, "<blockquote>")
	assertContains(t, html, "gotreesitter")
	assertContains(t, html, `class="footnote-ref"`)
	assertNotContains(t, html, "&gt; At the time")
}

func TestParseLink(t *testing.T) {
	doc := MustParse([]byte("[text](https://example.com)"))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	var link *Node
	for _, c := range p.Children {
		if c.Type == NodeLink {
			link = c
			break
		}
	}
	if link == nil {
		t.Fatal("expected NodeLink child")
	}
	if link.Attrs["href"] != "https://example.com" {
		t.Errorf("expected href %q, got %q", "https://example.com", link.Attrs["href"])
	}
	text := collectText(link)
	if text != "text" {
		t.Errorf("expected link text %q, got %q", "text", text)
	}
}

func TestParseImage(t *testing.T) {
	doc := MustParse([]byte(`![alt text](image.png "A title")`))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	p := doc.Root.Children[0]
	var img *Node
	for _, c := range p.Children {
		if c.Type == NodeImage {
			img = c
			break
		}
	}
	if img == nil {
		t.Fatal("expected NodeImage child")
	}
	if img.Attrs["src"] != "image.png" {
		t.Errorf("expected src %q, got %q", "image.png", img.Attrs["src"])
	}
	if img.Attrs["alt"] != "alt text" {
		t.Errorf("expected alt %q, got %q", "alt text", img.Attrs["alt"])
	}
	if img.Attrs["title"] != "A title" {
		t.Errorf("expected title %q, got %q", "A title", img.Attrs["title"])
	}
}

func TestParseTable(t *testing.T) {
	src := "| A | B |\n|---|---|\n| 1 | 2 |"
	doc := MustParse([]byte(src))
	if len(doc.Root.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	table := doc.Root.Children[0]
	if table.Type != NodeTable {
		t.Fatalf("expected NodeTable, got %d", table.Type)
	}
	if len(table.Children) < 2 {
		t.Fatalf("expected at least 2 rows (header + data), got %d", len(table.Children))
	}
	for _, row := range table.Children {
		if row.Type != NodeTableRow {
			t.Errorf("expected NodeTableRow, got %d", row.Type)
		}
		if len(row.Children) < 2 {
			t.Errorf("expected at least 2 cells, got %d", len(row.Children))
		}
		for _, cell := range row.Children {
			if cell.Type != NodeTableCell {
				t.Errorf("expected NodeTableCell, got %d", cell.Type)
			}
		}
	}
}

func TestParseComplexDocument(t *testing.T) {
	src := `# Title

A paragraph with **bold** and *italic*.

- item one
- item two

> a quote

` + "```js\nconsole.log(1)\n```" + `

[link](https://example.com)

| H1 | H2 |
|----|-----|
| a  | b   |
`

	doc := MustParse([]byte(src))
	if doc.Root.Type != NodeDocument {
		t.Fatal("expected NodeDocument root")
	}

	types := map[NodeType]bool{}
	for _, c := range doc.Root.Children {
		types[c.Type] = true
	}

	expected := []NodeType{
		NodeHeading,
		NodeParagraph,
		NodeList,
		NodeBlockquote,
		NodeCodeBlock,
		NodeTable,
	}
	for _, e := range expected {
		if !types[e] {
			t.Errorf("expected node type %d in complex document", e)
		}
	}
}

// collectText recursively collects all text content from a node tree.
func collectText(n *Node) string {
	if n == nil {
		return ""
	}
	if n.Type == NodeText {
		return n.Literal
	}
	var s string
	for _, c := range n.Children {
		s += collectText(c)
	}
	return s
}

func assertRange(t *testing.T, label string, got Range, want Range) {
	t.Helper()
	if got != want {
		t.Fatalf("%s range = %#v, want %#v", label, got, want)
	}
}

// FuzzParse asserts that Parse never panics on arbitrary input. The contract
// is that any byte slice produces either a *Document (possibly carrying
// MDPP-PARSE-000 diagnostics) or an error — but never a panic.
//
// Seed corpus: every input.md under examples/conformance/, plus a handful of
// inline seeds targeting the recursion shapes that triggered the 2026-05-26
// crash before the parser depth guard landed.
func FuzzParse(f *testing.F) {
	corpusRoot := filepath.Join("examples", "conformance")
	entries, _ := os.ReadDir(corpusRoot)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(corpusRoot, e.Name(), "input.md"))
		if err != nil {
			continue
		}
		f.Add(data)
	}

	f.Add([]byte("| col |\n|---|\n| `[[toc]]` |\n\n## Alpha\n"))
	f.Add([]byte("| col |\n|---|\n| `:::warning body :::` |\n"))
	f.Add([]byte(":::warning\n:::tip\nbody\n:::\n"))
	f.Add([]byte(strings.Repeat(":::a\n", 200) + strings.Repeat(":::\n", 200)))
	f.Add([]byte("[[toc]]\n\n## A\n\n:::warning [[toc]] :::\n"))

	f.Fuzz(func(t *testing.T, src []byte) {
		if len(src) > 1<<14 {
			t.Skip()
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Parse panicked on %d bytes: %v\nbytes: %q", len(src), r, src)
			}
		}()
		start := time.Now()
		doc, _ := Parse(src)
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("Parse took %s for %d bytes, want under 2s", elapsed, len(src))
		}
		if doc == nil {
			t.Fatal("Parse returned nil document on non-error path")
		}
	})
}
