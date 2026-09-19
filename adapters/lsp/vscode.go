package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
)

// VSCodeClient implements the LSPClient interface for VS Code.
type VSCodeClient struct {
	serverURI string
	conn      net.Conn
}

// Name returns the client name.
func (VSCodeClient) Name() string { return "vscode" }

// Initialize performs the LSP handshake with the VS Code LSP server.
func (c *VSCodeClient) Initialize(ctx context.Context) error {
	if c.serverURI == "" {
		return errors.New("server URI is required")
	}

	conn, err := net.Dial("tcp", strings.TrimPrefix(c.serverURI, "tcp://"))
	if err != nil {
		return fmt.Errorf("connect to LSP server: %w", err)
	}

	c.conn = conn
	return nil
}

// Shutdown closes the LSP connection.
func (c *VSCodeClient) Shutdown(ctx context.Context) error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// OpenDocument opens a document in the LSP server.
func (c *VSCodeClient) OpenDocument(ctx context.Context, uri string, content string) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": "go",
				"version":    1,
				"text":       content,
			},
		},
	}
	return c.sendMessage(msg)
}

// CloseDocument closes a document in the LSP server.
func (c *VSCodeClient) CloseDocument(ctx context.Context, uri string) error {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/didClose",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
		},
	}
	return c.sendMessage(msg)
}

// GetCompletions requests completion items at a position.
func (c *VSCodeClient) GetCompletions(ctx context.Context, uri string, line, character int) ([]CompletionItem, error) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/completion",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":    map[string]any{"line": line, "character": character},
		},
	}
	raw, err := c.sendMessageWithResult(msg)
	if err != nil {
		return nil, err
	}
	var result []CompletionItem
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetHover requests hover information at a position.
func (c *VSCodeClient) GetHover(ctx context.Context, uri string, line, character int) (*HoverInfo, error) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/hover",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":    map[string]any{"line": line, "character": character},
		},
	}
	raw, err := c.sendMessageWithResult(msg)
	if err != nil {
		return nil, err
	}
	var result HoverInfo
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetDefinition requests the definition of a symbol at a position.
func (c *VSCodeClient) GetDefinition(ctx context.Context, uri string, line, character int) (*Location, error) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/definition",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":    map[string]any{"line": line, "character": character},
		},
	}
	raw, err := c.sendMessageWithResult(msg)
	if err != nil {
		return nil, err
	}
	var result Location
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// GetReferences requests references to a symbol at a position.
func (c *VSCodeClient) GetReferences(ctx context.Context, uri string, line, character int) ([]Location, error) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/references",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
			"position":    map[string]any{"line": line, "character": character},
		},
	}
	raw, err := c.sendMessageWithResult(msg)
	if err != nil {
		return nil, err
	}
	var result []Location
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetDocumentSymbols requests all symbols in a document.
func (c *VSCodeClient) GetDocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/documentSymbol",
		"params": map[string]any{
			"textDocument": map[string]any{"uri": uri},
		},
	}
	raw, err := c.sendMessageWithResult(msg)
	if err != nil {
		return nil, err
	}
	var result []DocumentSymbol
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetWorkspaceSymbols requests workspace-wide symbols.
func (c *VSCodeClient) GetWorkspaceSymbols(ctx context.Context, query string) ([]DocumentSymbol, error) {
	msg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/symbol",
		"params": map[string]any{
			"query": query,
		},
	}
	raw, err := c.sendMessageWithResult(msg)
	if err != nil {
		return nil, err
	}
	var result []DocumentSymbol
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *VSCodeClient) sendMessage(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if c.conn == nil {
		return errors.New("not connected")
	}
	_, err = c.conn.Write(data)
	return err
}

func (c *VSCodeClient) sendMessageWithResult(msg map[string]any) (json.RawMessage, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if c.conn == nil {
		return nil, errors.New("not connected")
	}
	if _, err := c.conn.Write(data); err != nil {
		return nil, err
	}

	resp := make([]byte, 4096)
	n, err := c.conn.Read(resp)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(resp[:n]), nil
}

// NewVSCodeClient creates a new VS Code LSP client.
func NewVSCodeClient(serverURI string) *VSCodeClient {
	return &VSCodeClient{serverURI: serverURI}
}