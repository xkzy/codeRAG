package jetbrains

import (
	"context"
	"encoding/json"
	"errors"
)

// JetBrainsPlugin is the JetBrains plugin shell that connects to codeRAG MCP server.
type JetBrainsPlugin struct {
	serverURI string
	// IDE is the specific JetBrains IDE (e.g., "idea", "pycharm", "goland", "webstorm").
	IDE string
}

// Name returns the plugin name.
func (JetBrainsPlugin) Name() string { return "jetbrains" }

// JetBrainsConfig holds JetBrains plugin configuration.
type JetBrainsConfig struct {
	ServerURI string `json:"server_uri"`
	ProjectID string `json:"project_id"`
	IDE       string `json:"ide"`
}

// Convert transforms JetBrains plugin state into a normalized format.
func (p *JetBrainsPlugin) Convert(ctx context.Context, input any) (*NormalizedJetBrainsState, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("jetbrains adapter expects []byte input (JSON export)")
	}

	var state NormalizedJetBrainsState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// NormalizedJetBrainsState is the normalized output from a JetBrains plugin.
type NormalizedJetBrainsState struct {
	Plugin     string            `json:"plugin"`
	IDE        string            `json:"ide"`
	ProjectID  string            `json:"project_id"`
	ActiveFile *ActiveFileInfo   `json:"active_file"`
	Selections []SelectionInfo   `json:"selections"`
	Diagnostics []DiagnosticInfo  `json:"diagnostics"`
}

// ActiveFileInfo represents the currently active file in JetBrains.
type ActiveFileInfo struct {
	URI      string `json:"uri"`
	Content  string `json:"content"`
	Language string `json:"language"`
}

// SelectionInfo represents a text selection in JetBrains.
type SelectionInfo struct {
	URI    string `json:"uri"`
	Line   int    `json:"line"`
	Column int    `json:"character"`
}

// DiagnosticInfo represents a diagnostic from the IDE.
type DiagnosticInfo struct {
	URI      string `json:"uri"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Line     int    `json:"line"`
}

// NewJetBrainsPlugin creates a new JetBrains plugin shell.
func NewJetBrainsPlugin(serverURI, ide string) *JetBrainsPlugin {
	return &JetBrainsPlugin{serverURI: serverURI, IDE: ide}
}

// ServerURI returns the configured server URI.
func (p *JetBrainsPlugin) ServerURI() string {
	return p.serverURI
}

// IDE returns the configured IDE.
func (p *JetBrainsPlugin) GetIDE() string {
	return p.IDE
}

// IsConnected returns true if the plugin is connected to the server.
func (p *JetBrainsPlugin) IsConnected() bool {
	return p.serverURI != ""
}