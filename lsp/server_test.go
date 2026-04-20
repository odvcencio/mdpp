package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	if len(result.Capabilities.CodeActionProvider.CodeActionKinds) == 0 {
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

func TestServerJSONRPCHarness(t *testing.T) {
	uri := DocumentURI("file:///doc.md")
	src := "http://example.com\nTitle\n=====\n"

	var input bytes.Buffer
	writeRPCFrame(&input, rpcRequest(1, "initialize", InitializeParams{
		ClientInfo: &ClientInfo{Name: "test-harness"},
	}))
	writeRPCFrame(&input, rpcNotification("textDocument/didOpen", DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{
			URI:     uri,
			Version: 1,
			Text:    src,
		},
	}))
	writeRPCFrame(&input, rpcRequest(2, "textDocument/hover", HoverParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     positionAfterSubstring(src, "Title"),
	}))
	writeRPCFrame(&input, rpcRequest(3, "textDocument/codeAction", CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Range: Range{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 2, Character: 0},
		},
		Context: CodeActionContext{Only: []string{"source.fixAll"}},
	}))
	writeRPCFrame(&input, rpcRequest(4, "textDocument/semanticTokens/full", SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}))

	var output bytes.Buffer
	if err := NewServer().Serve(context.Background(), bytes.NewReader(input.Bytes()), &output); err != nil {
		t.Fatal(err)
	}

	frames := readRPCFrames(t, output.Bytes())
	if len(frames) != 5 {
		t.Fatalf("expected 5 RPC frames, got %d: %#v", len(frames), frames)
	}

	initFrame := rpcFrameByID(frames, "1")
	var initResult InitializeResult
	if err := json.Unmarshal(initFrame.Result, &initResult); err != nil {
		t.Fatal(err)
	}
	if !initResult.Capabilities.SemanticTokensProvider.Full || !initResult.Capabilities.HoverProvider {
		t.Fatalf("unexpected initialize result: %#v", initResult)
	}

	diagFrame := rpcFrameByMethod(frames, "textDocument/publishDiagnostics")
	var diagParams PublishDiagnosticsParams
	if err := json.Unmarshal(diagFrame.Params, &diagParams); err != nil {
		t.Fatal(err)
	}
	if len(diagParams.Diagnostics) == 0 {
		t.Fatalf("expected diagnostics notification, got %#v", diagParams)
	}

	hoverFrame := rpcFrameByID(frames, "2")
	var hover Hover
	if err := json.Unmarshal(hoverFrame.Result, &hover); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hover.Contents.Value, "Heading") {
		t.Fatalf("expected heading hover result, got %#v", hover)
	}

	actionFrame := rpcFrameByID(frames, "3")
	var actions []CodeAction
	if err := json.Unmarshal(actionFrame.Result, &actions); err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Kind != "source.fixAll.mdpp" {
		t.Fatalf("expected fix-all action, got %#v", actions)
	}

	tokensFrame := rpcFrameByID(frames, "4")
	var tokens SemanticTokens
	if err := json.Unmarshal(tokensFrame.Result, &tokens); err != nil {
		t.Fatal(err)
	}
	if len(tokens.Data) == 0 {
		t.Fatalf("expected semantic tokens in harness response, got %#v", tokens)
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

func TestServerCompletionContexts(t *testing.T) {
	t.Run("footnote ids", func(t *testing.T) {
		src := "Text [^o\n\n[^one]: Footnote one.\n[^other]: Footnote other.\n"
		items := completionItemsAt(t, src, positionAfterSubstring(src, "[^o"))
		item := completionItemByLabel(items, "one")
		if item == nil {
			t.Fatalf("expected footnote completion, got %#v", items)
		}
		if item.InsertText != "one]" {
			t.Fatalf("unexpected footnote insert text: %#v", item)
		}
	})

	t.Run("reference label after brackets", func(t *testing.T) {
		src := "See [intro][\n\n[intro]: /intro\n[other]: /other\n"
		items := completionItemsAt(t, src, positionAfterSubstring(src, "]["))
		item := completionItemByLabel(items, "intro")
		if item == nil {
			t.Fatalf("expected reference completion, got %#v", items)
		}
		if item.InsertText != "intro]" {
			t.Fatalf("unexpected reference insert text: %#v", item)
		}
	})

	t.Run("reference shortcut", func(t *testing.T) {
		src := "See [int\n\n[intro]: /intro\n[other]: /other\n"
		items := completionItemsAt(t, src, positionAfterSubstring(src, "int"))
		item := completionItemByLabel(items, "intro")
		if item == nil {
			t.Fatalf("expected shortcut reference completion, got %#v", items)
		}
	})

	t.Run("emoji shortcode", func(t *testing.T) {
		src := "Nice :sweat_\n"
		items := completionItemsAt(t, src, positionAfterSubstring(src, ":sweat_"))
		item := completionItemByLabel(items, "sweat_smile")
		if item == nil {
			t.Fatalf("expected emoji completion, got %#v", items)
		}
		if item.InsertText != "sweat_smile:" {
			t.Fatalf("unexpected emoji insert text: %#v", item)
		}
	})

	t.Run("frontmatter keys", func(t *testing.T) {
		src := "---\nti\n---\n\n# Content\n"
		items := completionItemsAt(t, src, positionAfterSubstring(src, "ti"))
		item := completionItemByLabel(items, "title")
		if item == nil {
			t.Fatalf("expected frontmatter key completion, got %#v", items)
		}
		if item.InsertText != "title: " {
			t.Fatalf("unexpected frontmatter insert text: %#v", item)
		}
	})
}

func TestServerSemanticTokensCoverage(t *testing.T) {
	src := "---\ntitle: Hello\n---\n\n> quote\n- item\n\n| H | B |\n|---|---|\n| x | y |\n\n`code`\n\n```go\nfmt.Println(1)\n```\n"
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: "file:///doc.md", Version: 1, Text: src})

	tokens, err := s.semanticTokensFull(SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"}})
	if err != nil {
		t.Fatal(err)
	}
	got := semanticTokenTypeSet(tokens.Data)
	for _, want := range []int{
		tokenFrontmatterKey,
		tokenFrontmatterValue,
		tokenComment,
		tokenOperator,
		tokenTableHeader,
		tokenTableSeparator,
		tokenString,
	} {
		if !got[want] {
			t.Fatalf("missing semantic token type %d in %#v", want, tokens.Data)
		}
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

func positionAfterSubstring(src string, needle string) Position {
	idx := strings.Index(src, needle)
	if idx < 0 {
		panic("missing substring " + needle)
	}
	return NewLineIndex([]byte(src)).OffsetToPosition(idx + len(needle))
}

func completionItemsAt(t *testing.T, src string, pos Position) []CompletionItem {
	t.Helper()
	s := NewServer()
	s.store.Open(TextDocumentItem{URI: "file:///doc.md", Version: 1, Text: src})
	result, err := s.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///doc.md"},
		Position:     pos,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		return nil
	}
	return result.Items
}

func completionItemByLabel(items []CompletionItem, label string) *CompletionItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
	}
	return nil
}

type rpcFrameEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func rpcRequest(id int, method string, params any) []byte {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return framedJSON(body)
}

func rpcNotification(method string, params any) []byte {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return framedJSON(body)
}

func framedJSON(body []byte) []byte {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(body))
	buf.Write(body)
	return buf.Bytes()
}

func writeRPCFrame(dst *bytes.Buffer, frame []byte) {
	_, _ = dst.Write(frame)
}

func readRPCFrames(t *testing.T, input []byte) []rpcFrameEnvelope {
	t.Helper()
	reader := bufio.NewReader(bytes.NewReader(input))
	var frames []rpcFrameEnvelope
	for {
		body, err := readFramedMessage(reader)
		if err != nil {
			if err == io.EOF {
				return frames
			}
			t.Fatal(err)
		}
		var frame rpcFrameEnvelope
		if err := json.Unmarshal(body, &frame); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, frame)
	}
}

func rpcFrameByID(frames []rpcFrameEnvelope, id string) rpcFrameEnvelope {
	for _, frame := range frames {
		if string(frame.ID) == id {
			return frame
		}
	}
	panic("missing response id " + id)
}

func rpcFrameByMethod(frames []rpcFrameEnvelope, method string) rpcFrameEnvelope {
	for _, frame := range frames {
		if frame.Method == method {
			return frame
		}
	}
	panic("missing notification " + method)
}

func semanticTokenTypeSet(data []uint32) map[int]bool {
	out := map[int]bool{}
	for i := 0; i+4 < len(data); i += 5 {
		out[int(data[i+3])] = true
	}
	return out
}
