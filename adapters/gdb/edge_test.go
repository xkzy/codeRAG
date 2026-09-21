package gdb

import (
	"context"
	"encoding/json"
	"testing"
)

func TestGDBAdapterNonByteInput(t *testing.T) {
	adapter := GDBAdapter{}
	_, err := adapter.Convert(context.Background(), "not bytes")
	if err == nil || err.Error() != "gdb adapter expects []byte input (JSON export)" {
		t.Fatalf("expected error for non-byte input, got %v", err)
	}
}

func TestGDBAdapterInvalidJSON(t *testing.T) {
	adapter := GDBAdapter{}
	_, err := adapter.Convert(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestGDBAdapterWithBasicBlocks(t *testing.T) {
	adapter := GDBAdapter{}
	input := `{
		"binary_id": "fw-1.0",
		"path": "/samples/fw.bin",
		"sha256": "abc123",
		"functions": [
			{
				"address": "0x401000",
				"name": "main",
				"size": 64,
				"calls": ["helper"],
				"strings": ["hello", "world"],
				"basic_blocks": [
					{
						"address": "0x401000",
						"instructions": [
							{"address": "0x401000", "mnemonic": "push", "operands": "%rbp"},
							{"address": "0x401003", "mnemonic": "call", "operands": "0x401020"}
						]
					}
				]
			}
		]
	}`
	result, err := adapter.Convert(context.Background(), []byte(input))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(result.Functions) != 1 {
		t.Fatalf("expected 1 function, got %d", len(result.Functions))
	}
	fn := result.Functions[0]
	if fn.Size == nil || *fn.Size != 64 {
		t.Errorf("expected size=64, got %v", fn.Size)
	}
	if len(fn.Calls) != 1 || fn.Calls[0] != "helper" {
		t.Errorf("expected calls=[helper], got %v", fn.Calls)
	}
	if len(fn.Strings) != 2 {
		t.Errorf("expected 2 strings, got %d", len(fn.Strings))
	}
	if fn.DecompilerOutput != "" {
		t.Errorf("expected empty decompiler output, got %s", fn.DecompilerOutput)
	}
	if len(fn.BasicBlocks) != 1 {
		t.Fatalf("expected 1 basic block, got %d", len(fn.BasicBlocks))
	}
	if len(fn.BasicBlocks[0].Instruction) != 2 {
		t.Fatalf("expected 2 instructions, got %d", len(fn.BasicBlocks[0].Instruction))
	}
	if fn.BasicBlocks[0].Instruction[0].Mnemonic != "push" {
		t.Errorf("expected push, got %s", fn.BasicBlocks[0].Instruction[0].Mnemonic)
	}
}

func TestGDBAdapterWithoutSize(t *testing.T) {
	adapter := GDBAdapter{}
	input := GDBExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []GDBFunction{
			{Address: "0x401000", Name: "main", Size: 0},
		},
	}
	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if fn := result.Functions[0]; fn.Size != nil {
		t.Errorf("expected nil size for Size=0, got %d", *fn.Size)
	}
}

func TestGDBAdapterWithDecompilerOutput(t *testing.T) {
	adapter := GDBAdapter{}
	input := GDBExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []GDBFunction{
			{
				Address:          "0x401000",
				Name:             "main",
				Size:             64,
				DecompilerOutput: "int main() { return 0; }",
			},
		},
	}
	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	fn := result.Functions[0]
	if fn.DecompilerOutput != "int main() { return 0; }" {
		t.Errorf("expected decompiler output, got %s", fn.DecompilerOutput)
	}
}

func TestGDBAdapterEmptyFunctions(t *testing.T) {
	adapter := GDBAdapter{}
	input := GDBExport{
		BinaryID:  "fw-1.0",
		Path:      "/samples/fw.bin",
		SHA256:    "abc123",
		Functions: nil,
	}
	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if len(result.Functions) != 0 {
		t.Errorf("expected 0 functions, got %d", len(result.Functions))
	}
	if result.Tool != "gdb" {
		t.Errorf("expected tool gdb, got %s", result.Tool)
	}
}

func TestGDBAdapterWithEmptyBasicBlocks(t *testing.T) {
	adapter := GDBAdapter{}
	input := GDBExport{
		BinaryID: "fw-1.0",
		Path:     "/samples/fw.bin",
		SHA256:   "abc123",
		Functions: []GDBFunction{
			{Address: "0x401000", Name: "main", Size: 32, BasicBlocks: []GDBBasicBlock{}},
		},
	}
	data, _ := json.Marshal(input)
	result, err := adapter.Convert(context.Background(), data)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	fn := result.Functions[0]
	if len(fn.BasicBlocks) != 0 {
		t.Errorf("expected 0 basic blocks, got %d", len(fn.BasicBlocks))
	}
}
