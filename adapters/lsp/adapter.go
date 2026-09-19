package lsp

import (
	"context"
	"encoding/json"
	"errors"
)

// LSPClient is the interface that all LSP clients must implement.
type LSPClient interface {
	// Name returns the client name (e.g., "vscode", "jetbrains", "emacs").
	Name() string

	// Initialize performs LSP handshake with the server.
	Initialize(ctx context.Context) error

	// Shutdown closes the LSP connection.
	Shutdown(ctx context.Context) error

	// OpenDocument opens a document in the LSP server.
	OpenDocument(ctx context.Context, uri string, content string) error

	// CloseDocument closes a document in the LSP server.
	CloseDocument(ctx context.Context, uri string) error

	// GetCompletions requests completion items at a position.
	GetCompletions(ctx context.Context, uri string, line, character int) ([]CompletionItem, error)

	// GetHover requests hover information at a position.
	GetHover(ctx context.Context, uri string, line, character int) (*HoverInfo, error)

	// GetDefinition requests the definition of a symbol at a position.
	GetDefinition(ctx context.Context, uri string, line, character int) (*Location, error)

	// GetReferences requests references to a symbol at a position.
	GetReferences(ctx context.Context, uri string, line, character int) ([]Location, error)

	// GetDocumentSymbols requests all symbols in a document.
	GetDocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error)

	// GetWorkspaceSymbols requests workspace-wide symbols.
	GetWorkspaceSymbols(ctx context.Context, query string) ([]DocumentSymbol, error)
}

// LSPAdapter implements the reverse.Adapter interface for LSP-based editors.
type LSPAdapter struct {
	client LSPClient
}

// Name returns the adapter name.
func (LSPAdapter) Name() string { return "lsp" }

// Convert transforms LSP results into a normalized format for codeRAG.
func (a LSPAdapter) Convert(ctx context.Context, input any) (*NormalizedLSPResult, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("lsp adapter expects []byte input (JSON export)")
	}

	var result NormalizedLSPResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// NormalizedLSPResult is the normalized output from an LSP client.
type NormalizedLSPResult struct {
	Client      string           `json:"client"`
	URI         string           `json:"uri"`
	Language    string           `json:"language"`
	Symbols     []DocumentSymbol `json:"symbols"`
	Completions []CompletionItem `json:"completions"`
	Hover       *HoverInfo       `json:"hover"`
	Definition  *Location        `json:"definition"`
	References  []Location       `json:"references"`
}

// DocumentSymbol represents a symbol in a document.
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           string           `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selection_range"`
	Children       []DocumentSymbol `json:"children"`
	Detail         string           `json:"detail"`
}

// Range represents a text range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Position represents a position in a text.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// CompletionItem represents a completion suggestion.
type CompletionItem struct {
	Label      string `json:"label"`
	Kind       string `json:"kind"`
	Detail     string `json:"detail"`
	InsertText string `json:"insert_text"`
}

// HoverInfo represents hover information.
type HoverInfo struct {
	Contents []HoverContent `json:"contents"`
}

// HoverContent represents hover content.
type HoverContent struct {
	Language string `json:"language"`
	Value    string `json:"value"`
}

// Location represents a location in a document.
type Location struct {
	URI   string `json:"uri"`
	Range *Range `json:"range"`
}

// NewLSPAdapter creates a new LSP adapter with the given client.
func NewLSPAdapter(client LSPClient) *LSPAdapter {
	return &LSPAdapter{client: client}
}

// Client returns the underlying LSP client.
func (a *LSPAdapter) Client() LSPClient {
	return a.client
}
