package lldb

import (
	"context"
	"encoding/json"
	"testing"
)

func TestLLDBAdapter(t *testing.T) {
	adapter := LLDBAdapter{}
	if adapter.Name() != "lldb" {
		t.Fatalf("expected name 'lldb', got %s", adapter.Name())
	}

	input := LLDBExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []LLDBFunction{
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

	if result.Tool != "lldb" {
		t.Errorf("expected tool lldb, got %s", result.Tool)
	}
	if len(result.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(result.Functions))
	}
}
