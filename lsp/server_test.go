package lsp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestServerInitializeCapabilities(t *testing.T) {
	s := NewServer()
	result := s.handleInitialize()
	if !result.Capabilities.HoverProvider {
		t.Fatal("expected hover provider")
	}
	if !result.Capabilities.DocumentFormattingProvider {
		t.Fatal("expected formatting provider")
	}
	if !result.Capabilities.SemanticTokensProvider.Full {
		t.Fatal("expected full semantic tokens")
	}
}

func TestServerDidOpenPublishesDiagnostics(t *testing.T) {
	s := NewServer()
	params := DidOpenTextDocumentParams{TextDocument: TextDocumentItem{
		URI:     "file:///doc.md",
		Version: 1,
		Text:    "http://example.com\n",
	}}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, respErr, err := s.dispatch(&out, incomingMessage{Method: "textDocument/didOpen", Params: data})
	if err != nil {
		t.Fatal(err)
	}
	if respErr != nil {
		t.Fatalf("unexpected response error: %+v", respErr)
	}
	if !strings.Contains(out.String(), "textDocument/publishDiagnostics") || !strings.Contains(out.String(), "MD034") {
		t.Fatalf("expected diagnostic notification, got %q", out.String())
	}
}

func TestServerFormattingAndPreview(t *testing.T) {
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: "file:///doc.md", Version: 1, Text: "Title\n=====\n"})

	edits, err := s.formatting(DocumentFormattingParams{TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 || edits[0].NewText != "# Title\n" {
		t.Fatalf("unexpected edits: %#v", edits)
	}

	preview, err := s.renderPreview(RenderPreviewParams{TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.HTML, `data-mdpp-source-start`) {
		t.Fatalf("expected source-positioned preview HTML, got %q", preview.HTML)
	}
}
