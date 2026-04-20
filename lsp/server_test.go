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
	if !result.Capabilities.DefinitionProvider || !result.Capabilities.ReferencesProvider {
		t.Fatal("expected definition and references providers")
	}
	if !result.Capabilities.CodeActionProvider {
		t.Fatal("expected code action provider")
	}
	if !result.Capabilities.FoldingRangeProvider || !result.Capabilities.DocumentSymbolProvider {
		t.Fatal("expected folding range and document symbol providers")
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

func TestServerCodeActionsFromLintFixes(t *testing.T) {
	src := "text  \n\n[stale]: https://example.com\n"
	uri := DocumentURI("file:///doc.md")
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: src})

	actions, err := s.codeActions(CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 3, Character: 0},
		},
		Context: CodeActionContext{Only: []string{"quickfix"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 {
		t.Fatalf("expected trailing whitespace and unused reference fixes, got %#v", actions)
	}
	byCode := map[string]CodeAction{}
	for _, action := range actions {
		if len(action.Diagnostics) != 1 {
			t.Fatalf("expected one diagnostic per action, got %#v", action)
		}
		byCode[action.Diagnostics[0].Code] = action
	}
	if action := byCode["MD009"]; action.Edit == nil || action.Edit.Changes[uri][0].NewText != "" {
		t.Fatalf("expected MD009 delete edit, got %#v", action)
	}
	if action := byCode["MDPP105"]; action.Edit == nil || action.Edit.Changes[uri][0].NewText != "" {
		t.Fatalf("expected MDPP105 delete edit, got %#v", action)
	}
}

func TestServerCodeActionsSourceFixAll(t *testing.T) {
	src := "Title\n=====\n"
	uri := DocumentURI("file:///doc.md")
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: uri, Version: 1, Text: src})

	actions, err := s.codeActions(CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 1, Character: 5},
		},
		Context: CodeActionContext{Only: []string{"source.fixAll"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 {
		t.Fatalf("expected one source fix-all action, got %#v", actions)
	}
	if actions[0].Kind != "source.fixAll.mdpp" {
		t.Fatalf("expected source.fixAll.mdpp, got %#v", actions[0])
	}
	if got := actions[0].Edit.Changes[uri][0].NewText; got != "# Title\n" {
		t.Fatalf("unexpected fix-all text: %q", got)
	}
}

func TestServerDefinitionAndReferences(t *testing.T) {
	src := "# Intro\n\nSee [intro](#intro) and note[^a].\n\n[^a]: Note.\n"
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: "file:///doc.md", Version: 1, Text: src})

	defs, err := s.definition(DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"},
		Position:     positionForSubstring(src, "#intro"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 || defs[0].Range.Start.Line != 0 {
		t.Fatalf("expected heading definition, got %#v", defs)
	}

	footnoteDefs, err := s.definition(DefinitionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"},
		Position:     positionForSubstring(src, "[^a]."),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(footnoteDefs) != 1 || footnoteDefs[0].Range.Start.Line != 4 {
		t.Fatalf("expected footnote definition, got %#v", footnoteDefs)
	}

	refs, err := s.references(ReferenceParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"},
		Position:     positionForSubstring(src, "Intro"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].Range.Start.Line != 2 {
		t.Fatalf("expected one heading reference, got %#v", refs)
	}
}

func TestServerFoldingRangesAndDocumentSymbols(t *testing.T) {
	src := "# Top\n\n:::note\n## Inside\nBody\n:::\n\n```go\nfmt.Println(1)\n```\n"
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: "file:///doc.md", Version: 1, Text: src})

	folds := s.foldingRanges(FoldingRangeParams{TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"}})
	if len(folds) < 2 {
		t.Fatalf("expected heading/container/code folds, got %#v", folds)
	}

	symbols := s.documentSymbols(DocumentSymbolParams{TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"}})
	if len(symbols) == 0 || symbols[0].Name != "Top" {
		t.Fatalf("expected top heading symbol, got %#v", symbols)
	}
	foundContainer := false
	for _, sym := range symbols {
		if sym.Name == ":::note" {
			foundContainer = true
		}
	}
	if !foundContainer {
		t.Fatalf("expected container symbol, got %#v", symbols)
	}
}

func TestServerRenameHeadingAndFootnote(t *testing.T) {
	src := "# Intro\n\nSee [intro](#intro) and note[^a].\n\n[^a]: Note.\n"
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: "file:///doc.md", Version: 1, Text: src})

	prep, err := s.prepareRename(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"},
		Position:     positionForSubstring(src, "Intro"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prep == nil || prep.Start.Line != 0 {
		t.Fatalf("expected heading prepare range, got %#v", prep)
	}

	edit, err := s.rename(RenameParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"},
		Position:     positionForSubstring(src, "Intro"),
		NewName:      "Overview",
	})
	if err != nil {
		t.Fatal(err)
	}
	headingEdits := edit.Changes["file:///doc.md"]
	if len(headingEdits) != 2 {
		t.Fatalf("expected heading text and anchor edits, got %#v", headingEdits)
	}
	if headingEdits[0].NewText != "Overview" && headingEdits[1].NewText != "Overview" {
		t.Fatalf("missing heading text edit: %#v", headingEdits)
	}
	if headingEdits[0].NewText != "#overview" && headingEdits[1].NewText != "#overview" {
		t.Fatalf("missing anchor edit: %#v", headingEdits)
	}

	footnoteEdit, err := s.rename(RenameParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"},
		Position:     positionForSubstring(src, "[^a]."),
		NewName:      "note",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(footnoteEdit.Changes["file:///doc.md"]); got != 2 {
		t.Fatalf("expected two footnote id edits, got %d: %#v", got, footnoteEdit)
	}
}

func positionForSubstring(src string, needle string) Position {
	idx := strings.Index(src, needle)
	if idx < 0 {
		panic("missing substring " + needle)
	}
	return NewLineIndex([]byte(src)).OffsetToPosition(idx)
}
