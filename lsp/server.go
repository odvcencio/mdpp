package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/odvcencio/mdpp"
)

type Server struct {
	store    *DocumentStore
	shutdown bool

	// writer is the JSON-RPC output stream set once in Serve (and lazily in
	// dispatch for tests). Async parse goroutines publish notifications via
	// this writer under writerMu.
	writerMu sync.Mutex
	writer   io.Writer

	// parseWG tracks in-flight async parses so Serve can drain them before
	// returning.
	parseWG sync.WaitGroup
}

type incomingMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func NewServer() *Server {
	return &Server{store: NewDocumentStore()}
}

func Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	return NewServer().Serve(ctx, r, w)
}

func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	s.setWriter(w)
	// Drain any in-flight async parses before returning so callers (tests,
	// hosts that pipe a finite stream through Serve) see the notifications
	// published on parse completion.
	defer s.parseWG.Wait()

	reader := bufio.NewReader(r)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		body, err := readFramedMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := s.handleBody(w, body); err != nil {
			if errors.Is(err, errExit) {
				return nil
			}
			return err
		}
	}
}

func (s *Server) setWriter(w io.Writer) {
	s.writerMu.Lock()
	s.writer = w
	s.writerMu.Unlock()
}

// writeFramed writes a framed notification/response under the writer mutex so
// async goroutines don't interleave bytes with the dispatch loop.
func (s *Server) writeFramed(payload any) error {
	s.writerMu.Lock()
	defer s.writerMu.Unlock()
	if s.writer == nil {
		return nil
	}
	return writeFramedMessage(s.writer, payload)
}

func (s *Server) handleBody(w io.Writer, body []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("lsp panic: %v", r)
		}
	}()

	// Make sure the writer is registered so the writeFramed path serializes
	// main-loop writes with goroutine writes. Tests that drive handleBody
	// directly enter through here too.
	s.writerMu.Lock()
	if s.writer == nil {
		s.writer = w
	}
	s.writerMu.Unlock()

	var msg incomingMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return s.writeFramed(errorResponse(nil, errorCodeParseError, err.Error()))
	}
	if msg.Method == "" {
		return s.writeFramed(errorResponse(msg.ID, errorCodeInvalidRequest, "missing method"))
	}

	hasID := len(msg.ID) > 0
	result, respErr, err := s.dispatch(w, msg)
	if err != nil {
		return err
	}
	if !hasID {
		return nil
	}
	if respErr != nil {
		return s.writeFramed(errorResponse(msg.ID, respErr.Code, respErr.Message))
	}
	return s.writeFramed(response(msg.ID, result))
}

func (s *Server) dispatch(w io.Writer, msg incomingMessage) (any, *ResponseError, error) {
	// Make sure async publishers have a writer to write to. Serve sets this
	// up front; tests that exercise dispatch() directly take this path.
	s.writerMu.Lock()
	if s.writer == nil {
		s.writer = w
	}
	s.writerMu.Unlock()

	switch msg.Method {
	case "initialize":
		return s.handleInitialize(), nil, nil
	case "initialized":
		return nil, nil, nil
	case "shutdown":
		s.shutdown = true
		return nil, nil, nil
	case "exit":
		return nil, nil, errExit
	case "textDocument/didOpen":
		params, err := decodeParams[DidOpenTextDocumentParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		s.parseWG.Add(1)
		s.store.OpenAsync(params.TextDocument, func(open *OpenDocument) {
			defer s.parseWG.Done()
			_ = s.asyncPublishDiagnostics(open)
		})
		return nil, nil, nil
	case "textDocument/didChange":
		params, err := decodeParams[DidChangeTextDocumentParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		doc, ok := s.store.Get(params.TextDocument.URI)
		if !ok {
			return nil, rpcParamErrorString("document is not open"), nil
		}
		if err := doc.ApplyChanges(params.TextDocument.Version, params.ContentChanges); err != nil {
			return nil, rpcParamError(err), nil
		}
		return nil, nil, s.publishDiagnostics(w, doc)
	case "textDocument/didSave":
		params, err := decodeParams[DidSaveTextDocumentParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		doc, ok := s.store.Get(params.TextDocument.URI)
		if ok && params.Text != nil {
			_, _, _, version := doc.Snapshot()
			if err := doc.ApplyChanges(version, []TextDocumentContentChangeEvent{{Text: *params.Text}}); err != nil {
				return nil, rpcParamError(err), nil
			}
			return nil, nil, s.publishDiagnostics(w, doc)
		}
		return nil, nil, nil
	case "textDocument/didClose":
		params, err := decodeParams[DidCloseTextDocumentParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		s.store.Close(params.TextDocument.URI)
		return nil, nil, s.writeFramed(notification("textDocument/publishDiagnostics", PublishDiagnosticsParams{URI: params.TextDocument.URI, Diagnostics: []Diagnostic{}}))
	case "textDocument/hover":
		params, err := decodeParams[HoverParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.hover(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/definition":
		params, err := decodeParams[DefinitionParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.definition(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/references":
		params, err := decodeParams[ReferenceParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.references(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/prepareRename":
		params, err := decodeParams[TextDocumentPositionParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.prepareRename(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/rename":
		params, err := decodeParams[RenameParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.rename(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/codeAction":
		params, err := decodeParams[CodeActionParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.codeActions(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/completion":
		params, err := decodeParams[CompletionParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.completion(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/formatting":
		params, err := decodeParams[DocumentFormattingParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.formatting(params)
		if err != nil {
			return nil, &ResponseError{Code: errorCodeInternalError, Message: err.Error()}, nil
		}
		return result, nil, nil
	case "textDocument/semanticTokens/full":
		params, err := decodeParams[SemanticTokensParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.semanticTokensFull(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/semanticTokens/range":
		params, err := decodeParams[SemanticTokensRangeParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.semanticTokensRange(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	case "textDocument/foldingRange":
		params, err := decodeParams[FoldingRangeParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return s.foldingRanges(params), nil, nil
	case "textDocument/documentSymbol":
		params, err := decodeParams[DocumentSymbolParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return s.documentSymbols(params), nil, nil
	case "markdownpp/renderPreview":
		params, err := decodeParams[RenderPreviewParams](msg.Params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		result, err := s.renderPreview(params)
		if err != nil {
			return nil, rpcParamError(err), nil
		}
		return result, nil, nil
	default:
		return nil, &ResponseError{Code: errorCodeMethodNotFound, Message: "method not found: " + msg.Method}, nil
	}
}

func (s *Server) handleInitialize() InitializeResult {
	return InitializeResult{
		Capabilities: ServerCapabilities{
			TextDocumentSync: TextDocumentSyncOptions{
				OpenClose: true,
				Change:    textDocumentSyncKindIncremental,
				Save:      SaveOptions{IncludeText: true},
			},
			HoverProvider:              true,
			DefinitionProvider:         true,
			ReferencesProvider:         true,
			RenameProvider:             true,
			CodeActionProvider:         CodeActionOptions{CodeActionKinds: []string{"quickfix", "source.fixAll", "source.fixAll.mdpp"}},
			FoldingRangeProvider:       true,
			DocumentSymbolProvider:     true,
			DocumentFormattingProvider: true,
			CompletionProvider: CompletionOptions{
				TriggerCharacters: []string{"[", "]", "^", ":", "!", "#"},
			},
			SemanticTokensProvider: SemanticTokensOptions{
				Legend: SemanticTokensLegend{TokenTypes: semanticTokenTypes, TokenModifiers: semanticTokenModifiers},
				Range:  true,
				Full:   true,
			},
		},
		ServerInfo: ServerInfo{Name: "mdpp-lsp", Version: mdpp.Version},
	}
}

func rpcParamError(err error) *ResponseError {
	return &ResponseError{Code: errorCodeInvalidParams, Message: err.Error()}
}

func rpcParamErrorString(msg string) *ResponseError {
	return &ResponseError{Code: errorCodeInvalidParams, Message: msg}
}

func (s *Server) publishDiagnostics(w io.Writer, open *OpenDocument) error {
	if open == nil {
		return nil
	}
	doc, _, index, version := open.Snapshot()
	return s.writeFramed(notification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         open.URI,
		Version:     &version,
		Diagnostics: documentDiagnostics(open.URI, doc, index),
	}))
}

// asyncPublishDiagnostics publishes diagnostics through the server-owned
// writer, serialized with the main dispatch loop via writerMu. Safe to call
// from goroutines.
func (s *Server) asyncPublishDiagnostics(open *OpenDocument) error {
	if open == nil {
		return nil
	}
	doc, _, index, version := open.Snapshot()
	return s.writeFramed(notification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         open.URI,
		Version:     &version,
		Diagnostics: documentDiagnostics(open.URI, doc, index),
	}))
}
