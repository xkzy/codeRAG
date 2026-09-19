package vscode

import (
	"context"
	"encoding/json"
	"errors"
)

// VSCodeExtension is the VS Code extension shell that connects to codeRAG MCP server.
type VSCodeExtension struct {
	serverURI string
}

// Name returns the extension name.
func (VSCodeExtension) Name() string { return "vscode" }

// VSCodeConfig holds VS Code extension configuration.
type VSCodeConfig struct {
	ServerURI string `json:"server_uri"`
	ProjectID string `json:"project_id"`
}

// Convert transforms VS Code extension state into a normalized format.
func (e *VSCodeExtension) Convert(ctx context.Context, input any) (*NormalizedVSCodeState, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("vscode adapter expects []byte input (JSON export)")
	}

	var state NormalizedVSCodeState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// NormalizedVSCodeState is the normalized output from a VS Code extension.
type NormalizedVSCodeState struct {
	Extension   string           `json:"extension"`
	ProjectID   string           `json:"project_id"`
	ActiveFile  *ActiveFileInfo  `json:"active_file"`
	Selections  []SelectionInfo  `json:"selections"`
	Diagnostics []DiagnosticInfo `json:"diagnostics"`
}

// ActiveFileInfo represents the currently active file in VS Code.
type ActiveFileInfo struct {
	URI      string `json:"uri"`
	Content  string `json:"content"`
	Language string `json:"language"`
}

// SelectionInfo represents a text selection in VS Code.
type SelectionInfo struct {
	URI    string `json:"uri"`
	Line   int    `json:"line"`
	Column int    `json:"character"`
}

// DiagnosticInfo represents a diagnostic from the editor.
type DiagnosticInfo struct {
	URI      string `json:"uri"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
}

// NewVSCodeExtension creates a new VS Code extension shell.
func NewVSCodeExtension(serverURI string) *VSCodeExtension {
	return &VSCodeExtension{serverURI: serverURI}
}

// ServerURI returns the configured server URI.
func (e *VSCodeExtension) ServerURI() string {
	return e.serverURI
}

// IsConnected returns true if the extension is connected to the server.
func (e *VSCodeExtension) IsConnected() bool {
	return e.serverURI != ""
}
