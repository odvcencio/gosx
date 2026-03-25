package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/odvcencio/gosx"
)

const (
	textDocumentSyncFull = 1
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type server struct {
	docs     map[string]string
	reader   *bufio.Reader
	writer   io.Writer
	shutdown bool
}

type textDocumentItem struct {
	URI     string `json:"uri"`
	Text    string `json:"text"`
	Version int    `json:"version"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type textChange struct {
	Text string `json:"text"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []textChange                    `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
}

type formattingParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
}

type textEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// Serve runs a minimal LSP server over stdio.
func Serve(stdin io.Reader, stdout io.Writer) error {
	s := &server{
		docs:   make(map[string]string),
		reader: bufio.NewReader(stdin),
		writer: stdout,
	}

	for {
		payload, err := readMessage(s.reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		var req request
		if err := json.Unmarshal(payload, &req); err != nil {
			continue
		}
		if err := s.handle(req); err != nil {
			return err
		}
		if s.shutdown {
			return nil
		}
	}
}

func (s *server) handle(req request) error {
	switch req.Method {
	case "initialize":
		return s.respond(req.ID, map[string]any{
			"capabilities": map[string]any{
				"textDocumentSync":           textDocumentSyncFull,
				"documentFormattingProvider": true,
			},
			"serverInfo": map[string]any{
				"name":    "gosx-lsp",
				"version": gosx.Version,
			},
		})
	case "initialized", "$/setTrace", "workspace/didChangeConfiguration":
		return nil
	case "shutdown":
		s.shutdown = true
		return s.respond(req.ID, nil)
	case "exit":
		s.shutdown = true
		return nil
	case "textDocument/didOpen":
		var params didOpenParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.respondError(req.ID, -32602, err.Error())
		}
		s.docs[params.TextDocument.URI] = params.TextDocument.Text
		return s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didChange":
		var params didChangeParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.respondError(req.ID, -32602, err.Error())
		}
		if len(params.ContentChanges) > 0 {
			s.docs[params.TextDocument.URI] = params.ContentChanges[len(params.ContentChanges)-1].Text
		}
		return s.publishDiagnostics(params.TextDocument.URI)
	case "textDocument/didClose":
		var params didCloseParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.respondError(req.ID, -32602, err.Error())
		}
		delete(s.docs, params.TextDocument.URI)
		return s.notify("textDocument/publishDiagnostics", map[string]any{
			"uri":         params.TextDocument.URI,
			"diagnostics": []Diagnostic{},
		})
	case "textDocument/formatting":
		var params formattingParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return s.respondError(req.ID, -32602, err.Error())
		}
		source, ok := s.docs[params.TextDocument.URI]
		if !ok {
			return s.respond(req.ID, []textEdit{})
		}
		formatted, err := FormatSource([]byte(source))
		if err != nil {
			return s.respondError(req.ID, -32603, err.Error())
		}
		return s.respond(req.ID, []textEdit{{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 1 << 20, Character: 0},
			},
			NewText: string(formatted),
		}})
	default:
		if len(req.ID) == 0 {
			return nil
		}
		return s.respond(req.ID, nil)
	}
}

func (s *server) publishDiagnostics(uri string) error {
	source := s.docs[uri]
	diags := Analyze(URIToPath(uri), []byte(source))
	return s.notify("textDocument/publishDiagnostics", map[string]any{
		"uri":         uri,
		"diagnostics": diags,
	})
}

func (s *server) notify(method string, params any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	}
	return writeMessage(s.writer, payload)
}

func (s *server) respond(id json.RawMessage, result any) error {
	return writeMessage(s.writer, response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *server) respondError(id json.RawMessage, code int, message string) error {
	if len(id) == 0 {
		return nil
	}
	return writeMessage(s.writer, response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &responseError{
			Code:    code,
			Message: message,
		},
	})
}

func readMessage(r *bufio.Reader) ([]byte, error) {
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
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(parts[0], "Content-Length") {
			if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &contentLength); err != nil {
				return nil, err
			}
		}
	}

	if contentLength < 0 {
		return nil, io.ErrUnexpectedEOF
	}

	payload := make([]byte, contentLength)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func writeMessage(w io.Writer, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(data)); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
