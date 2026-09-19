package ida

import (
	"context"
	"encoding/json"
	"errors"

	"codergag/internal/reverse"
)

// IDAAdapter implements the reverse.Adapter interface for IDA Pro exports.
type IDAAdapter struct{}

// Name returns the adapter name.
func (IDAAdapter) Name() string { return "ida" }

// IDAExport represents the expected JSON structure from IDA export plugins.
type IDAExport struct {
	BinaryID  string        `json:"binary_id"`
	Path      string        `json:"path"`
	SHA256    string        `json:"sha256"`
	Functions []IDAFunction `json:"functions"`
}

// IDAFunction represents a function in IDA export.
type IDAFunction struct {
	Address          string          `json:"address"`
	Name             string          `json:"name"`
	Size             int             `json:"size,omitempty"`
	Calls            []string        `json:"calls,omitempty"`
	Strings          []string        `json:"strings,omitempty"`
	DecompilerOutput string          `json:"decompiler_output,omitempty"`
	BasicBlocks      []IDABasicBlock `json:"basic_blocks,omitempty"`
}

// IDABasicBlock represents a basic block in IDA export.
type IDABasicBlock struct {
	Address     string           `json:"address"`
	Instruction []IDAInstruction `json:"instructions,omitempty"`
}

// IDAInstruction represents an instruction in IDA export.
type IDAInstruction struct {
	Address  string `json:"address"`
	Mnemonic string `json:"mnemonic"`
	Operands string `json:"operands"`
}

// Convert transforms IDA export JSON into NormalizedBinary.
func (IDAAdapter) Convert(ctx context.Context, input any) (*reverse.NormalizedBinary, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("ida adapter expects []byte input (JSON export)")
	}

	var export IDAExport
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
		Tool:      "ida",
		Functions: funcs,
	}, nil
}
