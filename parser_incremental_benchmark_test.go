package mdpp

import (
	"fmt"
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

func BenchmarkMarkdownEditorFull256KiB(b *testing.B) {
	source, _ := makeMarkdownEditorBenchmarkSource(256 << 10)
	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser := NewParser()
		if _, err := parser.Parse(source); err != nil {
			b.Fatal(err)
		}
		parser.Close()
	}
}

func BenchmarkMarkdownEditorIncrementalChanged256KiB(b *testing.B) {
	sourceA, editSites := makeMarkdownEditorBenchmarkSource(256 << 10)
	offset := editSites[len(editSites)/2]
	sourceB := make([]byte, 0, len(sourceA)+1)
	sourceB = append(sourceB, sourceA[:offset]...)
	sourceB = append(sourceB, 'x')
	sourceB = append(sourceB, sourceA[offset:]...)

	insert := gotreesitter.InputEdit{
		StartByte:   uint32(offset),
		OldEndByte:  uint32(offset),
		NewEndByte:  uint32(offset + 1),
		StartPoint:  markdownEditorPointForOffset(sourceA, offset),
		OldEndPoint: markdownEditorPointForOffset(sourceA, offset),
		NewEndPoint: markdownEditorPointForOffset(sourceB, offset+1),
	}
	deleteEdit := gotreesitter.InputEdit{
		StartByte:   uint32(offset),
		OldEndByte:  uint32(offset + 1),
		NewEndByte:  uint32(offset),
		StartPoint:  markdownEditorPointForOffset(sourceB, offset),
		OldEndPoint: markdownEditorPointForOffset(sourceB, offset+1),
		NewEndPoint: markdownEditorPointForOffset(sourceA, offset),
	}

	parser := NewParser()
	initial, err := parser.Parse(sourceA)
	if err != nil {
		b.Fatal(err)
	}
	requireNoMarkdownEditorPanicDiagnostic(b, initial)
	b.Cleanup(parser.Close)

	currentA := true
	b.ReportAllocs()
	b.SetBytes(int64(len(sourceA)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		source := sourceB
		edit := insert
		if !currentA {
			source = sourceA
			edit = deleteEdit
		}
		doc, err := parser.ParseIncremental(source, edit)
		if err != nil {
			b.Fatal(err)
		}
		requireNoMarkdownEditorPanicDiagnostic(b, doc)
		currentA = !currentA
	}
}

func requireNoMarkdownEditorPanicDiagnostic(b *testing.B, doc *Document) {
	b.Helper()
	if doc == nil {
		b.Fatal("nil Markdown editor document")
	}
	for _, diagnostic := range doc.Diagnostics() {
		if diagnostic.Code == "MDPP-PARSE-000" {
			b.Fatalf("Markdown editor benchmark hit panic recovery: %+v", diagnostic)
		}
	}
}

func BenchmarkMarkdownBlockIncrementalChanged256KiB(b *testing.B) {
	sourceA, editSites := makeMarkdownEditorBenchmarkSource(256 << 10)
	offset := editSites[len(editSites)/2]
	sourceB := make([]byte, 0, len(sourceA)+1)
	sourceB = append(sourceB, sourceA[:offset]...)
	sourceB = append(sourceB, 'x')
	sourceB = append(sourceB, sourceA[offset:]...)
	insert := gotreesitter.InputEdit{
		StartByte:   uint32(offset),
		OldEndByte:  uint32(offset),
		NewEndByte:  uint32(offset + 1),
		StartPoint:  markdownEditorPointForOffset(sourceA, offset),
		OldEndPoint: markdownEditorPointForOffset(sourceA, offset),
		NewEndPoint: markdownEditorPointForOffset(sourceB, offset+1),
	}
	deleteEdit := gotreesitter.InputEdit{
		StartByte:   uint32(offset),
		OldEndByte:  uint32(offset + 1),
		NewEndByte:  uint32(offset),
		StartPoint:  markdownEditorPointForOffset(sourceB, offset),
		OldEndPoint: markdownEditorPointForOffset(sourceB, offset+1),
		NewEndPoint: markdownEditorPointForOffset(sourceA, offset),
	}

	tree, err := parsePooled(blockLang(), mdEntry, sourceA)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if tree != nil {
			tree.Release()
		}
	})
	currentA := true
	b.ReportAllocs()
	b.SetBytes(int64(len(sourceA)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		source := sourceB
		edit := insert
		if !currentA {
			source = sourceA
			edit = deleteEdit
		}
		tree.Edit(edit)
		next, parseErr := parseIncrementalFromTree(blockLang(), mdEntry, source, tree)
		if parseErr != nil {
			b.Fatal(parseErr)
		}
		if next != tree {
			tree.Release()
		}
		tree = next
		currentA = !currentA
	}
}

func BenchmarkMarkdownCachedASTRebuild256KiB(b *testing.B) {
	source, _ := makeMarkdownEditorBenchmarkSource(256 << 10)
	tree, err := parsePooled(blockLang(), mdEntry, source)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(tree.Release)
	bound := gotreesitter.Bind(tree)
	ctx := &parseCtx{cache: newParseCache(), seen: make(map[cacheKey]struct{})}
	_ = convertBlockCtx(bound, bound.RootNode(), source, ctx)

	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.seen = make(map[cacheKey]struct{})
		if root := convertBlockCtx(bound, bound.RootNode(), source, ctx); root == nil {
			b.Fatal("nil rebuilt AST")
		}
	}
}

func BenchmarkMarkdownCachedASTPostProcess256KiB(b *testing.B) {
	source, _ := makeMarkdownEditorBenchmarkSource(256 << 10)
	tree, err := parsePooled(blockLang(), mdEntry, source)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(tree.Release)
	bound := gotreesitter.Bind(tree)
	ctx := &parseCtx{cache: newParseCache(), seen: make(map[cacheKey]struct{})}
	_ = convertBlockCtx(bound, bound.RootNode(), source, ctx)

	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx.seen = make(map[cacheKey]struct{})
		root := convertBlockCtx(bound, bound.RootNode(), source, ctx)
		doc := &Document{Root: root, Source: source}
		doc.extractFrontmatter()
		postProcess(doc)
	}
}

func BenchmarkMarkdownLinkRefCollection256KiB(b *testing.B) {
	source, _ := makeMarkdownEditorBenchmarkSource(256 << 10)
	tree, err := parsePooled(blockLang(), mdEntry, source)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(tree.Release)
	bound := gotreesitter.Bind(tree)

	b.ReportAllocs()
	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = collectLinkRefDefs(bound, bound.RootNode())
	}
}

func makeMarkdownEditorBenchmarkSource(target int) ([]byte, []int) {
	var source strings.Builder
	source.Grow(target + 1024)
	source.WriteString("# Markdown++ editor benchmark\n\n")
	editSites := make([]int, 0, target/320)
	for i := 0; source.Len() < target || len(editSites) < 4; i++ {
		fmt.Fprintf(&source, "## Section %06d\n\n", i)
		fmt.Fprintf(&source, "> Quoted context for section %06d.\n", i)
		fmt.Fprintf(&source, "> - nested item %06d\n", i)
		fmt.Fprintf(&source, "> - second nested item %06d\n\n", i)
		source.WriteString("Paragraph with **emphasis**, a [link](https://example.com), and edit token_")
		editSites = append(editSites, source.Len())
		fmt.Fprintf(&source, "%06d for incremental reuse.\n\n", i)
		fmt.Fprintf(&source, "```go\nfmt.Printf(\"section %%d\", %d)\n```\n\n", i)
	}
	return []byte(source.String()), editSites
}

func markdownEditorPointForOffset(source []byte, offset int) gotreesitter.Point {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	var point gotreesitter.Point
	for _, b := range source[:offset] {
		if b == '\n' {
			point.Row++
			point.Column = 0
		} else {
			point.Column++
		}
	}
	return point
}
