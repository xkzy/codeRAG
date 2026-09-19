package binja

import (
	"context"
	"encoding/json"
	"errors"

	"codergag/internal/reverse"
)

// BinjaAdapter implements the reverse.Adapter interface for Binary Ninja exports.
type BinjaAdapter struct{}

// Name returns the adapter name.
func (BinjaAdapter) Name() string { return "binja" }

// BinjaExport represents the expected JSON structure from Binary Ninja export.
type BinjaExport struct {
	BinaryID  string            `json:"binary_id"`
	Path      string            `json:"path"`
	SHA256    string            `json:"sha256"`
	Functions []BinjaFunction   `json:"functions"`
}

// BinjaFunction represents a function in Binary Ninja export.
type BinjaFunction struct {
	Address          string               `json:"address"`
	Name             string               `json:"name"`
	Size             int                  `json:"size,omitempty"`
	Calls            []string             `json:"calls,omitempty"`
	Strings          []string             `json:"strings,omitempty"`
	DecompilerOutput string               `json:"decompiler_output,omitempty"`
	BasicBlocks      []BinjaBasicBlock    `json:"basic_blocks,omitempty"`
}

// BinjaBasicBlock represents a basic block in Binary Ninja export.
type BinjaBasicBlock struct {
	Address     string               `json:"address"`
	Instruction []BinjaInstruction    `json:"instructions,omitempty"`
}

// BinjaInstruction represents an instruction in Binary Ninja export.
type BinjaInstruction struct {
	Address  string `json:"address"`
	Mnemonic string `json:"mnemonic"`
	Operands string `json:"operands"`
}

// Convert transforms Binary Ninja export JSON into NormalizedBinary.
func (BinjaAdapter) Convert(ctx context.Context, input any) (*reverse.NormalizedBinary, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("binja adapter expects []byte input (JSON export)")
	}

	var export BinjaExport
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
		Tool:      "binja",
		Functions: funcs,
	}, nil
}