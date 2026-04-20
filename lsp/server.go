package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/odvcencio/mdpp"
)

type Server struct {
	store    *DocumentStore
	shutdown bool
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

func (s *Server) handleBody(w io.Writer, body []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("lsp panic: %v", r)
		}
	}()

	var msg incomingMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return writeFramedMessage(w, errorResponse(nil, errorCodeParseError, err.Error()))
	}
	if msg.Method == "" {
		return writeFramedMessage(w, errorResponse(msg.ID, errorCodeInvalidRequest, "missing method"))
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
		return writeFramedMessage(w, errorResponse(msg.ID, respErr.Code, respErr.Message))
	}
	return writeFramedMessage(w, response(msg.ID, result))
}

func (s *Server) dispatch(w io.Writer, msg incomingMessage) (any, *ResponseError, error) {
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
		doc := s.store.Open(params.TextDocument)
		return nil, nil, s.publishDiagnostics(w, doc)
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
		return nil, nil, writeFramedMessage(w, notification("textDocument/publishDiagnostics", PublishDiagnosticsParams{URI: params.TextDocument.URI}))
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
			DocumentFormattingProvider: true,
			CompletionProvider: CompletionOptions{
				TriggerCharacters: []string{"[", ":", "!"},
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
	return writeFramedMessage(w, notification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         open.URI,
		Version:     &version,
		Diagnostics: documentDiagnostics(open.URI, doc, index),
	}))
}
