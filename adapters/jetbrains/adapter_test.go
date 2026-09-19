package jetbrains

import (
	"context"
	"testing"
)

func TestJetBrainsPluginName(t *testing.T) {
	p := NewJetBrainsPlugin("tcp://localhost:8080", "idea")
	if p.Name() != "jetbrains" {
		t.Errorf("expected name 'jetbrains', got %s", p.Name())
	}
}

func TestJetBrainsPluginConvert(t *testing.T) {
	p := NewJetBrainsPlugin("tcp://localhost:8080", "idea")

	input := `{
		"plugin": "jetbrains",
		"ide": "idea",
		"project_id": "p",
		"active_file": {
			"uri": "file:///test.go",
			"content": "package main",
			"language": "go"
		},
		"selections": [
			{"uri": "file:///test.go", "line": 5, "character": 10}
		],
		"diagnostics": [
			{"uri": "file:///test.go", "severity": "warning", "message": "unused variable", "line": 3}
		]
	}`

	result, err := p.Convert(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if result.Plugin != "jetbrains" {
		t.Errorf("expected plugin 'jetbrains', got %s", result.Plugin)
	}
	if result.IDE != "idea" {
		t.Errorf("expected IDE 'idea', got %s", result.IDE)
	}
	if result.ProjectID != "p" {
		t.Errorf("expected project_id 'p', got %s", result.ProjectID)
	}
	if result.ActiveFile == nil {
		t.Error("expected active file")
	}
	if result.ActiveFile.URI != "file:///test.go" {
		t.Errorf("expected URI 'file:///test.go', got %s", result.ActiveFile.URI)
	}
	if len(result.Selections) != 1 {
		t.Errorf("expected 1 selection, got %d", len(result.Selections))
	}
	if len(result.Diagnostics) != 1 {
		t.Errorf("expected 1 diagnostic, got %d", len(result.Diagnostics))
	}
}

func TestJetBrainsPluginInvalidInput(t *testing.T) {
	p := NewJetBrainsPlugin("tcp://localhost:8080", "idea")
	_, err := p.Convert(context.Background(), "not bytes")
	if err == nil {
		t.Error("expected error for non-byte input")
	}
}

func TestJetBrainsPluginInvalidJSON(t *testing.T) {
	p := NewJetBrainsPlugin("tcp://localhost:8080", "idea")
	_, err := p.Convert(context.Background(), []byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestJetBrainsPluginIsConnected(t *testing.T) {
	p := NewJetBrainsPlugin("", "idea")
	if p.IsConnected() {
		t.Error("expected not connected when URI is empty")
	}

	p2 := NewJetBrainsPlugin("tcp://localhost:8080", "idea")
	if !p2.IsConnected() {
		t.Error("expected connected when URI is set")
	}
}

func TestJetBrainsPluginServerURI(t *testing.T) {
	p := NewJetBrainsPlugin("tcp://localhost:8080", "idea")
	if p.ServerURI() != "tcp://localhost:8080" {
		t.Errorf("expected server URI 'tcp://localhost:8080', got %s", p.ServerURI())
	}
}

func TestJetBrainsPluginIDE(t *testing.T) {
	p := NewJetBrainsPlugin("tcp://localhost:8080", "goland")
	if p.GetIDE() != "goland" {
		t.Errorf("expected IDE 'goland', got %s", p.GetIDE())
	}
}