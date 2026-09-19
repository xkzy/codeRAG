package gdb

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGDBAdapter(t *testing.T) {
	adapter := GDBAdapter{}
	if adapter.Name() != "gdb" {
		t.Fatalf("expected name 'gdb', got %s", adapter.Name())
	}

	input := GDBExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []GDBFunction{
			{
				Address: "0x401000",
				Name:    "main",
				Size:    64,
			},
		},
	}

	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if result.Tool != "gdb" {
		t.Errorf("expected tool gdb, got %s", result.Tool)
	}
	if len(result.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(result.Functions))
	}
}
