package lsp

import (
	"context"
	"testing"
)

func TestVSCodeClientName(t *testing.T) {
	c := NewVSCodeClient("tcp://localhost:8080")
	if c.Name() != "vscode" {
		t.Errorf("expected name 'vscode', got %s", c.Name())
	}
}

func TestJetBrainsClientName(t *testing.T) {
	c := NewJetBrainsClient("tcp://localhost:8080", "idea")
	if c.Name() != "jetbrains:idea" {
		t.Errorf("expected name 'jetbrains:idea', got %s", c.Name())
	}
}

func TestLSPAdapterConvert(t *testing.T) {
	adapter := LSPAdapter{}

	input := `{
		"client": "vscode",
		"uri": "file:///test.go",
		"language": "go",
		"symbols": [
			{
				"name": "main",
				"kind": "function",
				"range": {"start": {"line": 0, "character": 0}, "end": {"line": 0, "character": 10}},
				"selection_range": {"start": {"line": 0, "character": 0}, "end": {"line": 0, "character": 10}}
			}
		],
		"completions": [
			{"label": "fmt", "kind": "module"}
		],
		"hover": {
			"contents": [
				{"language": "go", "value": "func main()"}
			]
		},
		"definition": {
			"uri": "file:///main.go",
			"range": {"start": {"line": 5, "character": 0}, "end": {"line": 5, "character": 10}}
		},
		"references": [
			{"uri": "file:///main.go", "range": {"start": {"line": 10, "character": 0}, "end": {"line": 10, "character": 10}}}
		]
	}`

	result, err := adapter.Convert(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if result.Client != "vscode" {
		t.Errorf("expected client 'vscode', got %s", result.Client)
	}
	if result.URI != "file:///test.go" {
		t.Errorf("expected URI 'file:///test.go', got %s", result.URI)
	}
	if len(result.Symbols) != 1 {
		t.Errorf("expected 1 symbol, got %d", len(result.Symbols))
	}
	if result.Symbols[0].Name != "main" {
		t.Errorf("expected symbol name 'main', got %s", result.Symbols[0].Name)
	}
	if len(result.Completions) != 1 {
		t.Errorf("expected 1 completion, got %d", len(result.Completions))
	}
	if result.Hover == nil {
		t.Error("expected hover info")
	}
	if result.Definition == nil {
		t.Error("expected definition")
	}
	if len(result.References) != 1 {
		t.Errorf("expected 1 reference, got %d", len(result.References))
	}
}

func TestLSPAdapterInvalidInput(t *testing.T) {
	adapter := LSPAdapter{}
	_, err := adapter.Convert(context.Background(), "not bytes")
	if err == nil {
		t.Error("expected error for non-byte input")
	}
}

func TestLSPAdapterInvalidJSON(t *testing.T) {
	adapter := LSPAdapter{}
	_, err := adapter.Convert(context.Background(), []byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestVSCodeClientInitializeWithoutURI(t *testing.T) {
	c := NewVSCodeClient("")
	err := c.Initialize(context.Background())
	if err == nil {
		t.Error("expected error when server URI is empty")
	}
}

func TestJetBrainsClientInitializeWithoutURI(t *testing.T) {
	c := NewJetBrainsClient("", "idea")
	err := c.Initialize(context.Background())
	if err == nil {
		t.Error("expected error when server URI is empty")
	}
}