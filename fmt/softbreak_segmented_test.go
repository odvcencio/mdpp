package fmt

import (
	"bytes"
	"html"
	"regexp"
	"strings"
	"testing"

	"m31labs.dev/mdpp"
)

// Keep this fixture at the segmented-parser boundary. The list continuation
// paragraphs deliberately include inline markup in their marker lines: that
// keeps the continuation as a safe soft-break structure instead of allowing a
// later fixed-point pass to flatten it accidentally.
const segmentedListFixtureBoundary = 2048

func TestSegmentedListContinuationPreservesSoftBreakSpace(t *testing.T) {
	source := segmentedListContinuationSource(segmentedListFixtureBoundary)
	if len(source) != segmentedListFixtureBoundary {
		t.Fatalf("fixture length = %d, want %d", len(source), segmentedListFixtureBoundary)
	}

	for _, tc := range []struct {
		name   string
		source []byte
		fast   bool
	}{
		{name: "primary below boundary", source: segmentedListContinuationSource(segmentedListFixtureBoundary - 1), fast: false},
		{name: "segmented fast path", source: source, fast: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, tree, err := mdpp.ParseWithTree(tc.source)
			if err != nil {
				t.Fatalf("ParseWithTree: %v", err)
			}
			usedPrimaryTree := tree != nil
			if tree != nil {
				tree.Release()
			}
			if usedPrimaryTree == tc.fast {
				t.Fatalf("primary tree = %t, want %t", usedPrimaryTree, !tc.fast)
			}
			if doc == nil || doc.AST() == nil {
				t.Fatal("ParseWithTree returned no AST")
			}
			if got := len(doc.AST().Find(mdpp.NodeHeading)); got != 3 {
				t.Fatalf("heading count = %d, want 3", got)
			}
			beforeShape := listShape(doc.AST())
			if beforeShape != (listShapeCounts{lists: 1, listItems: 2, listItemsUnderLists: 2}) {
				t.Fatalf("source list shape = %+v, want one list with two ancestral items", beforeShape)
			}

			once, err := Format(tc.source)
			if err != nil {
				t.Fatalf("Format: %v", err)
			}
			if len(once) > len(tc.source)*2 {
				t.Fatalf("formatted source grew from %d to %d bytes", len(tc.source), len(once))
			}
			want := segmentedListContinuationCanonical(tc.fast)
			if !bytes.Contains(once, []byte(want)) {
				t.Fatalf("canonical continuation text missing; want %q in:\n%s", want, once)
			}
			if bytes.Contains(once, []byte("Metalvariants")) {
				t.Fatalf("word-separating space was dropped:\n%s", once)
			}
			if tc.fast && !bytes.Contains(once, []byte("beta, gamma")) {
				t.Fatalf("segmented punctuation boundary was not canonicalized with a space:\n%s", once)
			}
			if !tc.fast && !bytes.Contains(once, []byte("beta,\n  gamma.")) {
				t.Fatalf("primary punctuation boundary was not preserved:\n%s", once)
			}
			if bytes.Contains(once, []byte("beta,gamma")) {
				t.Fatalf("punctuation boundary was joined:\n%s", once)
			}

			twice, err := Format(once)
			if err != nil {
				t.Fatalf("Format twice: %v", err)
			}
			if !bytes.Equal(once, twice) {
				t.Fatalf("Format is not idempotent:\nfirst:\n%s\nsecond:\n%s", once, twice)
			}

			formattedDoc, err := mdpp.Parse(once)
			if err != nil {
				t.Fatalf("Parse formatted source: %v", err)
			}
			if afterShape := listShape(formattedDoc.AST()); afterShape != beforeShape {
				t.Fatalf("list ancestry changed: source=%+v formatted=%+v", beforeShape, afterShape)
			}
			if got, want := normalizedBlockText(doc.AST()), normalizedBlockText(formattedDoc.AST()); got != want {
				t.Fatalf("block text changed:\nsource:    %q\nformatted: %q", want, got)
			}
			if got, want := normalizedRenderedText(mdpp.NewRenderer().RenderString(string(tc.source))), normalizedRenderedText(mdpp.NewRenderer().RenderString(string(once))); got != want {
				t.Fatalf("rendered text changed:\nsource:    %q\nformatted: %q", want, got)
			}
		})
	}
}

func TestUnwrapOutsideListContinuationDoesNotAddPrefix(t *testing.T) {
	source := []byte("# H\n  first line\n  second line\n")
	once, err := Format(source)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if got, want := string(once), "# H\nfirst line second line\n"; got != want {
		t.Fatalf("outside-list continuation drifted: got %q, want %q", got, want)
	}
	if strings.Contains(string(once), "\n first line") || strings.Contains(string(once), "\n  first line") {
		t.Fatalf("outside-list continuation retained an unintended leading prefix: %q", once)
	}
	twice, err := Format(once)
	if err != nil {
		t.Fatalf("Format twice: %v", err)
	}
	if !bytes.Equal(once, twice) {
		t.Fatalf("Format is not idempotent:\nfirst: %q\nsecond: %q", once, twice)
	}
	if got, want := normalizedBlockText(mustParseAST(t, source)), normalizedBlockText(mustParseAST(t, once)); got != want {
		t.Fatalf("outside-list rendered/block text changed: source=%q formatted=%q", got, want)
	}
	if got, want := normalizedRenderedText(mdpp.NewRenderer().RenderString(string(source))), normalizedRenderedText(mdpp.NewRenderer().RenderString(string(once))); got != want {
		t.Fatalf("outside-list rendered text changed: source=%q formatted=%q", got, want)
	}
}

func TestUnwrapParagraphTextSourceRangeJoin(t *testing.T) {
	for _, tc := range []struct {
		name       string
		source     string
		leftEnd    int
		rightStart int
		want       string
	}{
		{name: "ordinary newline", source: "left\nright", leftEnd: 4, rightStart: 5, want: "left right"},
		{name: "same line", source: "left right", leftEnd: 4, rightStart: 5, want: "leftright"},
		{name: "hard break spaces", source: "left  \nright", leftEnd: 4, rightStart: 7, want: "leftright"},
		{name: "hard break backslash", source: "left\\\nright", leftEnd: 4, rightStart: 6, want: "leftright"},
		{name: "blank line", source: "left\n\nright", leftEnd: 4, rightStart: 6, want: "leftright"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := &mdpp.Node{Children: []*mdpp.Node{
				{Type: mdpp.NodeText, Literal: "left", Range: mdpp.Range{StartByte: 0, EndByte: tc.leftEnd, StartLine: 1}},
				{Type: mdpp.NodeText, Literal: "right", Range: mdpp.Range{StartByte: tc.rightStart, EndByte: len(tc.source), StartLine: 2}},
			}}
			if got := unwrapParagraphText(n, []byte(tc.source)); got != tc.want {
				t.Fatalf("unwrapParagraphText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTextChildrenCrossSoftBreakRejectsMalformedRanges(t *testing.T) {
	source := []byte("left\nright")
	for _, tc := range []struct {
		name     string
		previous mdpp.Range
		current  mdpp.Range
	}{
		{name: "negative previous start", previous: mdpp.Range{StartByte: -1, EndByte: 4, StartLine: 1}, current: mdpp.Range{StartByte: 5, EndByte: 10, StartLine: 2}},
		{name: "negative previous end", previous: mdpp.Range{StartByte: 0, EndByte: -1, StartLine: 1}, current: mdpp.Range{StartByte: 5, EndByte: 10, StartLine: 2}},
		{name: "negative current start", previous: mdpp.Range{StartByte: 0, EndByte: 4, StartLine: 1}, current: mdpp.Range{StartByte: -1, EndByte: 10, StartLine: 2}},
		{name: "negative current end", previous: mdpp.Range{StartByte: 0, EndByte: 4, StartLine: 1}, current: mdpp.Range{StartByte: 5, EndByte: -1, StartLine: 2}},
		{name: "out of bounds previous", previous: mdpp.Range{StartByte: 0, EndByte: len(source) + 1, StartLine: 1}, current: mdpp.Range{StartByte: 5, EndByte: 10, StartLine: 2}},
		{name: "out of bounds current", previous: mdpp.Range{StartByte: 0, EndByte: 4, StartLine: 1}, current: mdpp.Range{StartByte: 5, EndByte: len(source) + 1, StartLine: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := &mdpp.Node{Type: mdpp.NodeText, Literal: "left", Range: tc.previous}
			current := &mdpp.Node{Type: mdpp.NodeText, Literal: "right", Range: tc.current}
			if textChildrenCrossSoftBreak(previous, current, source) {
				t.Fatal("malformed ranges were accepted")
			}
		})
	}
}

func segmentedListContinuationSource(size int) []byte {
	const prefix = "# H0\n\n" +
		"- The compiler emits a versioned `KernelABI` for generated CUDA and Metal\n" +
		"  variants. Pure-Go GoTreeSitter parses the generated C++-compatible signature;\n" +
		"  vendor qualifiers and Metal attributes are masked in a byte-preserving view,\n" +
		"  while address spaces, access mode, and binding locations are recovered from\n" +
		"  the original source.\n" +
		"- `KernelVariant.Binary` is an optional, hash-checked offline image descriptor\n" +
		"  beta,\n" +
		"  gamma. compiler.CompileKernelVariants and eos compile --offline-backend ...\n" +
		"  are the explicit build actions that attach images; each image carries both\n" +
		"  its own hash and the source fingerprint it was\n" +
		"  compiled from.\n" +
		"\n# H1\n\n"
	if len(prefix) > size {
		panic("segmented list fixture prefix exceeds requested size")
	}
	const suffix = "\n# H2\n"
	if len(prefix)+len(suffix) > size {
		panic("segmented list fixture prefix and suffix exceed requested size")
	}
	return []byte(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
}

func segmentedListContinuationCanonical(fast bool) string {
	if fast {
		return "- The compiler emits a versioned `KernelABI` for generated CUDA and Metal\n variants. Pure-Go GoTreeSitter parses the generated C++-compatible signature; vendor qualifiers"
	}
	return "- The compiler emits a versioned `KernelABI` for generated CUDA and Metal\n  variants. Pure-Go GoTreeSitter parses the generated C++-compatible signature;"
}

type listShapeCounts struct {
	lists               int
	listItems           int
	listItemsUnderLists int
}

func listShape(root *mdpp.Node) listShapeCounts {
	var counts listShapeCounts
	var walk func(*mdpp.Node, bool)
	walk = func(n *mdpp.Node, underList bool) {
		if n == nil {
			return
		}
		if n.Type == mdpp.NodeList {
			counts.lists++
			underList = true
		}
		if n.Type == mdpp.NodeListItem || n.Type == mdpp.NodeTaskListItem {
			counts.listItems++
			if underList {
				counts.listItemsUnderLists++
			}
		}
		for _, child := range n.Children {
			walk(child, underList)
		}
	}
	walk(root, false)
	return counts
}

func mustParseAST(t *testing.T, source []byte) *mdpp.Node {
	t.Helper()
	doc, err := mdpp.Parse(source)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return doc.AST()
}

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

func normalizedRenderedText(rendered string) string {
	return strings.Join(strings.Fields(html.UnescapeString(htmlTagRe.ReplaceAllString(rendered, " "))), " ")
}

func normalizedBlockText(root *mdpp.Node) string {
	var b strings.Builder
	var walk func(*mdpp.Node)
	walk = func(n *mdpp.Node) {
		if n == nil {
			return
		}
		switch n.Type {
		case mdpp.NodeText, mdpp.NodeCodeSpan:
			b.WriteString(n.Literal)
		case mdpp.NodeSoftBreak, mdpp.NodeHardBreak:
			b.WriteByte(' ')
		default:
			for _, child := range n.Children {
				walk(child)
			}
			if n.Type == mdpp.NodeHeading || n.Type == mdpp.NodeParagraph || n.Type == mdpp.NodeListItem {
				b.WriteByte(' ')
			}
		}
	}
	walk(root)
	return strings.Join(strings.Fields(b.String()), " ")
}
