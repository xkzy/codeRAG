package vscode

import (
	"context"
	"testing"
)

func TestVSCodeExtensionName(t *testing.T) {
	e := NewVSCodeExtension("tcp://localhost:8080")
	if e.Name() != "vscode" {
		t.Errorf("expected name 'vscode', got %s", e.Name())
	}
}

func TestVSCodeExtensionConvert(t *testing.T) {
	e := NewVSCodeExtension("tcp://localhost:8080")

	input := `{
		"extension": "vscode",
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
			{"uri": "file:///test.go", "severity": "error", "message": "unused variable", "line": 3}
		]
	}`

	result, err := e.Convert(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if result.Extension != "vscode" {
		t.Errorf("expected extension 'vscode', got %s", result.Extension)
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

func TestVSCodeExtensionInvalidInput(t *testing.T) {
	e := NewVSCodeExtension("tcp://localhost:8080")
	_, err := e.Convert(context.Background(), "not bytes")
	if err == nil {
		t.Error("expected error for non-byte input")
	}
}

func TestVSCodeExtensionInvalidJSON(t *testing.T) {
	e := NewVSCodeExtension("tcp://localhost:8080")
	_, err := e.Convert(context.Background(), []byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestVSCodeExtensionIsConnected(t *testing.T) {
	e := NewVSCodeExtension("")
	if e.IsConnected() {
		t.Error("expected not connected when URI is empty")
	}

	e2 := NewVSCodeExtension("tcp://localhost:8080")
	if !e2.IsConnected() {
		t.Error("expected connected when URI is set")
	}
}

func TestVSCodeExtensionServerURI(t *testing.T) {
	e := NewVSCodeExtension("tcp://localhost:8080")
	if e.ServerURI() != "tcp://localhost:8080" {
		t.Errorf("expected server URI 'tcp://localhost:8080', got %s", e.ServerURI())
	}
}