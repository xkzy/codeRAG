package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
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

func (s *Server) Run() error {
	for {
		msg, err := s.readMessage()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if msg == nil {
			continue
		}
		s.handleMessage(msg)
	}
}

func (s *Server) readMessage() (*Message, error) {
	header, err := s.reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, nil
	}
	header = strings.ToLower(header) // header names are case-insensitive
	if !strings.HasPrefix(header, "content-length:") {
		return nil, nil
	}
	lengthStr := strings.TrimPrefix(header, "content-length:")
	lengthStr = strings.TrimSpace(lengthStr)
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return nil, fmt.Errorf("invalid content-length: %s", lengthStr)
	}

	_, err = s.reader.ReadString('\n')
	if err != nil {
		return nil, err
	}

	body := make([]byte, length)
	if _, err := io.ReadFull(s.reader, body); err != nil {
		return nil, err
	}
	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (s *Server) handleMessage(msg *Message) {
	var result any
	var rpcErr *ErrorObject

	defer func() {
		s.writeResponse(msg, result, rpcErr)
	}()

	switch msg.Method {
	case "initialize":
		result = InitializeResult{
			ProtocolVersion: "2024-11-05",
			Capabilities:    map[string]any{"tools": map[string]any{}},
			ServerInfo:      map[string]any{"name": "codeRAG", "version": s.version},
		}

	case "tools/list":
		tools := s.registry.Definitions()
		result = map[string]any{"tools": tools}

	case "tools/call":
		var callParams struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &callParams); err != nil {
			rpcErr = &ErrorObject{Code: -32602, Message: "invalid params: " + err.Error()}
			return
		}
		var args map[string]any
		if len(callParams.Arguments) > 0 {
			if err := json.Unmarshal(callParams.Arguments, &args); err != nil {
				rpcErr = &ErrorObject{Code: -32602, Message: "invalid arguments: " + err.Error()}
				return
			}
		}
		res, err := s.registry.Call(callParams.Name, args)
		if err != nil {
			rpcErr = &ErrorObject{Code: -32000, Message: err.Error()}
			return
		}

		text, err := json.Marshal(res)
		if err != nil {
			rpcErr = &ErrorObject{Code: -32000, Message: err.Error()}
			return
		}
		result = map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": string(text)},
			},
		}

	case "initialized":
		// Notification, no response needed

	default:
		return
	}
}

func (s *Server) writeResponse(msg *Message, result any, errObj *ErrorObject) {
	if msg.ID == nil && msg.Method != "initialized" {
		return
	}
	if msg.Method == "initialized" {
		return
	}

	var resp Message
	resp.JSONRPC = "2.0"
	resp.ID = msg.ID
	if errObj != nil {
		resp.Error = errObj
	} else {
		resp.Result = result
	}

	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	s.writer.Write([]byte(header))
	s.writer.Write(data)
}

func ReadMessage(reader *bufio.Reader) (*Message, error) {
	header, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	header = strings.TrimSpace(header)
	if header == "" {
		return nil, nil
	}
	lengthStr := strings.TrimPrefix(strings.ToLower(header), "content-length:")
	lengthStr = strings.TrimSpace(lengthStr)
	length, err := strconv.Atoi(lengthStr)
	if err != nil {
		return nil, fmt.Errorf("invalid content-length: %s", lengthStr)
	}
	reader.ReadString('\n')
	body := make([]byte, length)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	var msg Message
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

var _ = bytes.MinRead
