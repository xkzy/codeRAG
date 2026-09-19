package ida

import (
	"context"
	"encoding/json"
	"testing"

)

func TestIDAAdapter(t *testing.T) {
	adapter := IDAAdapter{}
	if adapter.Name() != "ida" {
		t.Fatalf("expected name 'ida', got %s", adapter.Name())
	}

	input := IDAExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []IDAFunction{
			{
				Address:          "0x401000",
				Name:             "main",
				Size:             64,
				Calls:            []string{"0x401200"},
				Strings:          []string{"Hello World"},
				DecompilerOutput: "int main() { printf(\"Hello World\"); }",
				BasicBlocks: []IDABasicBlock{
					{
						Address: "0x401000",
						Instruction: []IDAInstruction{
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

	if result.Tool != "ida" {
		t.Errorf("expected tool ida, got %s", result.Tool)
	}
	if len(result.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(result.Functions))
	}
}