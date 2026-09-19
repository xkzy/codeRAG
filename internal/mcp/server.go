package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Message is an incoming JSON-RPC request or notification (no ID).
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response is an outgoing JSON-RPC response: exactly one of Result or Error,
// never "method", and "id":null when the request could not be parsed.
type response struct {
	JSONRPC string       `json:"jsonrpc"`
	ID      any          `json:"id"`
	Result  any          `json:"result,omitempty"`
	Error   *ErrorObject `json:"error,omitempty"`
}

type ErrorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type InitializeResult struct {
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
	ServerInfo      map[string]any `json:"serverInfo"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type Server struct {
	version  string
	registry *ToolRegistry
	reader   *bufio.Reader
	writer   io.Writer
	// newline is the framing of the message being answered: MCP's stdio
	// transport is one JSON object per line; Content-Length (LSP style) is
	// still accepted and answered in kind.
	newline bool
}

type ServerOption func(*Server)

func WithVersion(v string) ServerOption {
	return func(s *Server) { s.version = v }
}

func NewServer(
	registry *ToolRegistry,
	opts ...ServerOption,
) *Server {
	s := &Server{
		version:  "0.1.0",
		registry: registry,
		reader:   bufio.NewReader(os.Stdin),
		writer:   os.Stdout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errServer         = -32000
)

// supportedProtocols are the MCP revisions this server can speak; the tool
// surface is identical across them.
var supportedProtocols = map[string]bool{"2024-11-05": true, "2025-03-26": true, "2025-06-18": true}

func (s *Server) Run() error {
	for {
		payload, err := s.readPayload()
		if payload != nil {
			s.dispatch(payload)
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// readPayload returns the next message body, detecting the framing. A nil
// payload with a nil error means "nothing to process yet" (blank line).
func (s *Server) readPayload() ([]byte, error) {
	line, err := s.reader.ReadString('\n')
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil, err
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "content-length:") {
		s.newline = true
		return []byte(trimmed), err // newline-delimited JSON; err may be a final EOF
	}
	s.newline = false
	length, convErr := strconv.Atoi(strings.TrimSpace(trimmed[len("content-length:"):]))
	if convErr != nil || length < 0 {
		return nil, fmt.Errorf("invalid content-length: %q", trimmed)
	}
	for { // remaining headers, ended by a blank line
		h, herr := s.reader.ReadString('\n')
		if herr != nil {
			return nil, herr
		}
		if strings.TrimSpace(h) == "" {
			break
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(s.reader, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (s *Server) dispatch(payload []byte) {
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		s.write(response{JSONRPC: "2.0", Error: &ErrorObject{Code: errParse, Message: "parse error: " + err.Error()}})
		return
	}
	result, rpcErr := s.handle(&msg)
	if msg.ID == nil {
		return // notifications are never answered
	}
	s.write(response{JSONRPC: "2.0", ID: msg.ID, Result: result, Error: rpcErr})
}

func (s *Server) handle(msg *Message) (any, *ErrorObject) {
	switch msg.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		version := "2024-11-05"
		if supportedProtocols[p.ProtocolVersion] {
			version = p.ProtocolVersion
		}
		return InitializeResult{
			ProtocolVersion: version,
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      map[string]any{"name": "codeRAG", "version": s.version},
		}, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": s.registry.Definitions()}, nil

	case "tools/call":
		var callParams struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &callParams); err != nil {
			return nil, &ErrorObject{Code: errInvalidParams, Message: "invalid params: " + err.Error()}
		}
		var args map[string]any
		if len(callParams.Arguments) > 0 {
			if err := json.Unmarshal(callParams.Arguments, &args); err != nil {
				return nil, &ErrorObject{Code: errInvalidParams, Message: "invalid arguments: " + err.Error()}
			}
		}
		res, err := s.registry.Call(callParams.Name, args)
		if err != nil {
			return nil, &ErrorObject{Code: errServer, Message: err.Error()}
		}
		text, err := json.Marshal(res)
		if err != nil {
			return nil, &ErrorObject{Code: errServer, Message: err.Error()}
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}}, nil
	}
	if strings.HasPrefix(msg.Method, "notifications/") || msg.Method == "initialized" {
		return nil, nil
	}
	return nil, &ErrorObject{Code: errMethodNotFound, Message: "method not found: " + msg.Method}
}

// write emits one response as a single Write so concurrent output cannot interleave.
func (s *Server) write(r response) {
	data, err := json.Marshal(r)
	if err != nil {
		return
	}
	if s.newline {
		s.writer.Write(append(data, '\n'))
		return
	}
	s.writer.Write(append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))), data...))
}
