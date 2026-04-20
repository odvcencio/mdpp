package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const jsonrpcVersion = "2.0"

type ResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type ResponseMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

type NotificationMessage struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

const (
	errorCodeParseError     = -32700
	errorCodeInvalidRequest = -32600
	errorCodeMethodNotFound = -32601
	errorCodeInvalidParams  = -32602
	errorCodeInternalError  = -32603
)

var errExit = errors.New("lsp exit requested")

func readFramedMessage(r *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length header %q", value)
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, errors.New("missing Content-Length header")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

func writeFramedMessage(w io.Writer, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var header bytes.Buffer
	fmt.Fprintf(&header, "Content-Length: %d\r\n\r\n", len(body))
	if _, err := w.Write(header.Bytes()); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func response(id json.RawMessage, result any) *ResponseMessage {
	return &ResponseMessage{
		JSONRPC: jsonrpcVersion,
		ID:      cloneRawMessage(id),
		Result:  result,
	}
}

func errorResponse(id json.RawMessage, code int, message string) *ResponseMessage {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &ResponseMessage{
		JSONRPC: jsonrpcVersion,
		ID:      cloneRawMessage(id),
		Error:   &ResponseError{Code: code, Message: message},
	}
}

func notification(method string, params any) NotificationMessage {
	return NotificationMessage{
		JSONRPC: jsonrpcVersion,
		Method:  method,
		Params:  params,
	}
}

func cloneRawMessage(in json.RawMessage) json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(json.RawMessage, len(in))
	copy(out, in)
	return out
}

func decodeParams[T any](params json.RawMessage) (T, error) {
	var out T
	if len(params) == 0 || bytes.Equal(bytes.TrimSpace(params), []byte("null")) {
		return out, nil
	}
	err := json.Unmarshal(params, &out)
	return out, err
}
