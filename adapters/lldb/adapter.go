package lldb

import (
	"context"
	"encoding/json"
	"errors"

	"codergag/internal/reverse"
)

// LLDBAdapter implements the reverse.Adapter interface for LLDB exports.
type LLDBAdapter struct{}

// Name returns the adapter name.
func (LLDBAdapter) Name() string { return "lldb" }

// LLDBExport represents the expected JSON structure from LLDB Python script export.
type LLDBExport struct {
	BinaryID  string         `json:"binary_id"`
	Path      string         `json:"path"`
	SHA256    string         `json:"sha256"`
	Functions []LLDBFunction `json:"functions"`
}

// LLDBFunction represents a function in LLDB export.
type LLDBFunction struct {
	Address          string           `json:"address"`
	Name             string           `json:"name"`
	Size             int              `json:"size,omitempty"`
	Calls            []string         `json:"calls,omitempty"`
	Strings          []string         `json:"strings,omitempty"`
	DecompilerOutput string           `json:"decompiler_output,omitempty"`
	BasicBlocks      []LLDBBasicBlock `json:"basic_blocks,omitempty"`
}

// LLDBBasicBlock represents a basic block in LLDB export.
type LLDBBasicBlock struct {
	Address     string            `json:"address"`
	Instruction []LLDBInstruction `json:"instructions,omitempty"`
}

// LLDBInstruction represents an instruction in LLDB export.
type LLDBInstruction struct {
	Address  string `json:"address"`
	Mnemonic string `json:"mnemonic"`
	Operands string `json:"operands"`
}

// Convert transforms LLDB export JSON into NormalizedBinary.
func (LLDBAdapter) Convert(ctx context.Context, input any) (*reverse.NormalizedBinary, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("lldb adapter expects []byte input (JSON export)")
	}

	var export LLDBExport
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
		Tool:      "lldb",
		Functions: funcs,
	}, nil
}
