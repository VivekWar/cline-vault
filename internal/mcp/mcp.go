// Package mcp implements Vault's MCP server: a stdio JSON-RPC 2.0 transport.
//
// stdout carries ONLY JSON-RPC responses, one per line; all logging goes to
// stderr. The server loop reads from an io.Reader and writes to an io.Writer
// so it can be tested without spawning a process.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"os"

	"vault/internal/state"
)

// DefaultMaxLineBytes bounds a single stdin JSON-RPC line. stderr payloads
// inside tools/call arguments can be large, so the limit is generous.
const DefaultMaxLineBytes = 4 << 20 // 4 MB

// request is one decoded JSON-RPC 2.0 message.
type request struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id"` // nil when the "id" key is absent (a notification)
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params"`
}

// rpcError is a JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// response is one JSON-RPC 2.0 response line. ID is the raw request id echoed
// byte-for-byte. Result and Error are mutually exclusive; omitempty keeps the
// unused one out of the wire format.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Server is the MCP over stdio server.
type Server struct {
	r       io.Reader
	w       io.Writer
	st      *state.State
	log     *log.Logger
	maxLine int
}

// NewServer returns a Server reading requests from r and writing responses to
// w, persisting state under root (VAULT_ROOT).
func NewServer(r io.Reader, w io.Writer, root string) *Server {
	return &Server{
		r:       r,
		w:       w,
		st:      state.New(root),
		log:     log.New(os.Stderr, "", 0), // stdout is reserved for JSON-RPC
		maxLine: DefaultMaxLineBytes,
	}
}

// Serve runs the JSON-RPC loop until the input reaches EOF.
func (s *Server) Serve() error {
	br := bufio.NewReader(s.r)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if lineTooLong(line, s.maxLine) {
				// Discard the oversized line and keep serving.
				s.write(response{
					JSONRPC: "2.0",
					ID:      json.RawMessage("null"),
					Error:   &rpcError{Code: -32700, Message: "Parse error"},
				})
			} else {
				s.handleLine(line)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// lineTooLong reports whether line exceeds maxLine bytes, ignoring the
// trailing newline.
func lineTooLong(line []byte, maxLine int) bool {
	n := len(line)
	if n > 0 && line[n-1] == '\n' {
		n--
	}
	return n > maxLine
}

// handleLine processes one raw stdin line.
func (s *Server) handleLine(line []byte) {
	trimmed := bytes.TrimSpace(line)
	var req request
	if len(trimmed) == 0 || json.Unmarshal(trimmed, &req) != nil {
		// Malformed JSON: -32700 with id null.
		s.write(response{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &rpcError{Code: -32700, Message: "Parse error"},
		})
		return
	}
	// Any message without an id is a notification: NEVER respond.
	if req.ID == nil {
		s.log.Printf("notification (no response): method=%q", req.Method)
		return
	}
	id := *req.ID
	if req.JSONRPC != "2.0" || req.Method == "" {
		s.write(response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32600, Message: "Invalid Request"}})
		return
	}
	s.write(s.dispatch(req, id))
}

// dispatch routes a request to its handler. id is the raw echoed id.
func (s *Server) dispatch(req request, id json.RawMessage) response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(id, req.Params)
	case "ping":
		// ping MUST serialize as {"result":{}}, never null.
		return response{JSONRPC: "2.0", ID: id, Result: json.RawMessage("{}")}
	case "tools/list":
		return s.handleToolsList(id)
	case "tools/call":
		return s.handleToolsCall(id, req.Params)
	default:
		return response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: -32601, Message: "Method not found"}}
	}
}

// write emits one response as a single Write call that includes the trailing
// newline. stdout carries ONLY JSON-RPC responses.
func (s *Server) write(res response) {
	data, err := json.Marshal(res)
	if err != nil {
		s.log.Printf("marshal response: %v", err)
		return
	}
	data = append(data, '\n')
	if _, err := s.w.Write(data); err != nil {
		s.log.Printf("write response: %v", err)
	}
}
