package gdb

import (
	"context"
	"encoding/json"
	"errors"

	"codergag/internal/reverse"
)

// GDBAdapter implements the reverse.Adapter interface for GDB exports.
type GDBAdapter struct{}

// Name returns the adapter name.
func (GDBAdapter) Name() string { return "gdb" }

// GDBExport represents the expected JSON structure from GDB Python script export.
type GDBExport struct {
	BinaryID  string        `json:"binary_id"`
	Path      string        `json:"path"`
	SHA256    string        `json:"sha256"`
	Functions []GDBFunction `json:"functions"`
}

// GDBFunction represents a function in GDB export.
type GDBFunction struct {
	Address          string          `json:"address"`
	Name             string          `json:"name"`
	Size             int             `json:"size,omitempty"`
	Calls            []string        `json:"calls,omitempty"`
	Strings          []string        `json:"strings,omitempty"`
	DecompilerOutput string          `json:"decompiler_output,omitempty"`
	BasicBlocks      []GDBBasicBlock `json:"basic_blocks,omitempty"`
}

// GDBBasicBlock represents a basic block in GDB export.
type GDBBasicBlock struct {
	Address     string           `json:"address"`
	Instruction []GDBInstruction `json:"instructions,omitempty"`
}

// GDBInstruction represents an instruction in GDB export.
type GDBInstruction struct {
	Address  string `json:"address"`
	Mnemonic string `json:"mnemonic"`
	Operands string `json:"operands"`
}

// Convert transforms GDB export JSON into NormalizedBinary.
func (GDBAdapter) Convert(ctx context.Context, input any) (*reverse.NormalizedBinary, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("gdb adapter expects []byte input (JSON export)")
	}

	var export GDBExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, err
	}

	funcs := make([]reverse.NormalizedFunction, 0, len(export.Functions))
	for _, f := range export.Functions {
		var size *int
		if f.Size > 0 {
			size = &f.Size
		}

		bbs := make([]reverse.NormalizedBasicBlock, 0, len(f.BasicBlocks))
		for _, bb := range f.BasicBlocks {
			insns := make([]reverse.NormalizedInstruction, 0, len(bb.Instruction))
			for _, insn := range bb.Instruction {
				insns = append(insns, reverse.NormalizedInstruction{
					Address:  insn.Address,
					Mnemonic: insn.Mnemonic,
					Operands: insn.Operands,
				})
			}
			bbs = append(bbs, reverse.NormalizedBasicBlock{
				Address:     bb.Address,
				Instruction: insns,
			})
		}

		funcs = append(funcs, reverse.NormalizedFunction{
			Address:          f.Address,
			Name:             f.Name,
			Size:             size,
			Calls:            f.Calls,
			Strings:          f.Strings,
			DecompilerOutput: f.DecompilerOutput,
			BasicBlocks:      bbs,
		})
	}

	return &reverse.NormalizedBinary{
		BinaryID:  export.BinaryID,
		Path:      export.Path,
		Sha256:    export.SHA256,
		Tool:      "gdb",
		Functions: funcs,
	}, nil
}
