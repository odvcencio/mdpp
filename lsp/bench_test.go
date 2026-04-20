package lsp

import "testing"

const benchLSPMarkdown = "# Title\n\nSee [docs][ref] and note[^a].\n\n[ref]: https://example.com \"Docs\"\n[^a]: Footnote text.\n\n## Section\n\n- item one\n- item two\n\n```go\nfmt.Println(1)\n```\n\n:rocket:\n"

func BenchmarkServerHover(b *testing.B) {
	uri := DocumentURI("file:///bench.md")
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: benchLSPMarkdown})
	pos := positionForSubstring(benchLSPMarkdown, "Title")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.hover(HoverParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
			Position:     pos,
		})
	}
}

func BenchmarkServerSemanticTokensFull(b *testing.B) {
	uri := DocumentURI("file:///bench.md")
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: benchLSPMarkdown})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.semanticTokensFull(SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: uri}})
	}
}

func BenchmarkServerFormatting(b *testing.B) {
	uri := DocumentURI("file:///bench.md")
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: benchLSPMarkdown})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.formatting(DocumentFormattingParams{TextDocument: TextDocumentIdentifier{URI: uri}})
	}
}
