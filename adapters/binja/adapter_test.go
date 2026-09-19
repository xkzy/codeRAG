package binja

import (
	"context"
	"encoding/json"
	"testing"

)

func TestBinjaAdapter(t *testing.T) {
	adapter := BinjaAdapter{}
	if adapter.Name() != "binja" {
		t.Fatalf("expected name 'binja', got %s", adapter.Name())
	}

	input := BinjaExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []BinjaFunction{
			{
				Address: "0x401000",
				Name:    "main",
				Size:    64,
				BasicBlocks: []BinjaBasicBlock{
					{
						Address: "0x401000",
						Instruction: []BinjaInstruction{
							{Address: "0x401000", Mnemonic: "push", Operands: "rbp"},
						},
					},
				},
			},
		},
	}

	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}

	if result.Tool != "binja" {
		t.Errorf("expected tool binja, got %s", result.Tool)
	}
	if len(result.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(result.Functions))
	}
}