package ghidra

import (
	"context"
	"encoding/json"
	"testing"

)

func TestGhidraAdapter(t *testing.T) {
	adapter := GhidraAdapter{}
	if adapter.Name() != "ghidra" {
		t.Fatalf("expected name 'ghidra', got %s", adapter.Name())
	}

	input := GhidraExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []GhidraFunction{
			{
				Address:          "0x401000",
				Name:             "main",
				Size:             64,
				Calls:            []string{"0x401200"},
				Strings:          []string{"Hello World"},
				DecompilerOutput: "int main() { printf(\"Hello World\"); }",
				BasicBlocks: []GhidraBasicBlock{
					{
						Address: "0x401000",
						Instruction: []GhidraInstruction{
							{Address: "0x401000", Mnemonic: "push", Operands: "rbp"},
							{Address: "0x401001", Mnemonic: "mov", Operands: "rsp, rbp"},
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

	if result.BinaryID != "fw-1.0" {
		t.Errorf("expected BinaryID fw-1.0, got %s", result.BinaryID)
	}
	if result.Tool != "ghidra" {
		t.Errorf("expected tool ghidra, got %s", result.Tool)
	}
	if len(result.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(result.Functions))
	}
	fn := result.Functions[0]
	if fn.Name != "main" {
		t.Errorf("expected function name main, got %s", fn.Name)
	}
	if fn.Size == nil || *fn.Size != 64 {
		t.Errorf("expected size 64, got %v", fn.Size)
	}
	if len(fn.BasicBlocks) != 1 {
		t.Errorf("expected 1 basic block, got %d", len(fn.BasicBlocks))
	}
	if len(fn.BasicBlocks[0].Instruction) != 2 {
		t.Errorf("expected 2 instructions, got %d", len(fn.BasicBlocks[0].Instruction))
	}
}