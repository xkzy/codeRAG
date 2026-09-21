package retdec

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRetDecAdapterName(t *testing.T) {
	adapter := RetDecAdapter{}
	if adapter.Name() != "retdec" {
		t.Fatalf("expected name 'retdec', got %s", adapter.Name())
	}
}

func TestRetDecAdapterConvert(t *testing.T) {
	adapter := RetDecAdapter{}

	input := RetDecExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []RetDecFunction{
			{
				Address:          "0x401000",
				Name:             "main",
				Size:             64,
				Calls:            []string{"0x401200"},
				Strings:          []string{"Hello World"},
				DecompilerOutput: "int main() { printf(\"Hello World\"); }",
				BasicBlocks: []RetDecBasicBlock{
					{
						Address: "0x401000",
						Instruction: []RetDecInstruction{
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

	if result.Tool != "retdec" {
		t.Errorf("expected tool retdec, got %s", result.Tool)
	}
	if len(result.Functions) != 1 {
		t.Errorf("expected 1 function, got %d", len(result.Functions))
	}
	fn := result.Functions[0]
	if fn.Address != "0x401000" {
		t.Errorf("expected address 0x401000, got %s", fn.Address)
	}
	if fn.Name != "main" {
		t.Errorf("expected name main, got %s", fn.Name)
	}
	if fn.Size == nil || *fn.Size != 64 {
		t.Errorf("expected size 64, got %v", fn.Size)
	}
	if len(fn.Calls) != 1 || fn.Calls[0] != "0x401200" {
		t.Errorf("expected calls [0x401200], got %v", fn.Calls)
	}
	if fn.DecompilerOutput != "int main() { printf(\"Hello World\"); }" {
		t.Errorf("unexpected decompiler output: %s", fn.DecompilerOutput)
	}
	if len(fn.BasicBlocks) != 1 {
		t.Errorf("expected 1 basic block, got %d", len(fn.BasicBlocks))
	}
	if len(fn.BasicBlocks[0].Instruction) != 1 {
		t.Errorf("expected 1 instruction, got %d", len(fn.BasicBlocks[0].Instruction))
	}
}

func TestRetDecAdapterConvert_InvalidInput(t *testing.T) {
	adapter := RetDecAdapter{}
	_, err := adapter.Convert(context.Background(), "not bytes")
	if err == nil {
		t.Fatal("expected error for non-bytes input, got nil")
	}
}

func TestRetDecAdapterConvert_InvalidJSON(t *testing.T) {
	adapter := RetDecAdapter{}
	_, err := adapter.Convert(context.Background(), []byte("{invalid"))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestRetDecAdapterConvert_EmptyFunctions(t *testing.T) {
	adapter := RetDecAdapter{}
	input := RetDecExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
	}
	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}
	if len(result.Functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(result.Functions))
	}
}

func TestRetDecAdapterConvert_NilSize(t *testing.T) {
	adapter := RetDecAdapter{}
	input := RetDecExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []RetDecFunction{
			{
				Address: "0x401000",
				Name:    "main",
				Size:    0,
			},
		},
	}
	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert failed: %v", err)
	}
	if result.Functions[0].Size != nil {
		t.Errorf("expected nil size, got %v", result.Functions[0].Size)
	}
}
