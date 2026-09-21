package retdec

import (
	"context"
	"encoding/json"
	"errors"

	"codergag/internal/reverse"
)

// RetDecAdapter implements the reverse.Adapter interface for RetDec decompiler exports.
// RetDec (https://retdec.com) is a retargetable machine-code decompiler based on LLVM.
// It automatically produces decompiled C code and structured function metadata from
// binaries, reducing the need for token-expensive LLM analysis of raw assembly.
type RetDecAdapter struct{}

// Name returns the adapter name.
func (RetDecAdapter) Name() string { return "retdec" }

// RetDecExport represents the expected JSON structure from RetDec export.
// This mirrors RetDec's decompilation output, which can be obtained via:
//   - RetDec REST API: https://retdec.com/api/
//   - Local RetDec CLI: retdec-decompiler --output-format json binary
//   - RetDec Docker image: docker run -v $PWD:/mount retdec-decompiler binary
type RetDecExport struct {
	BinaryID  string           `json:"binary_id"`
	Path      string           `json:"path"`
	SHA256    string           `json:"sha256"`
	Functions []RetDecFunction `json:"functions"`
}

// RetDecFunction represents a function in RetDec export.
type RetDecFunction struct {
	Address          string            `json:"address"`
	Name             string            `json:"name"`
	Size             int               `json:"size,omitempty"`
	Calls            []string          `json:"calls,omitempty"`
	Strings          []string          `json:"strings,omitempty"`
	DecompilerOutput string            `json:"decompiler_output,omitempty"`
	BasicBlocks      []RetDecBasicBlock `json:"basic_blocks,omitempty"`
}

// RetDecBasicBlock represents a basic block in RetDec export.
type RetDecBasicBlock struct {
	Address     string               `json:"address"`
	Instruction []RetDecInstruction  `json:"instructions,omitempty"`
}

// RetDecInstruction represents an instruction in RetDec export.
type RetDecInstruction struct {
	Address  string `json:"address"`
	Mnemonic string `json:"mnemonic"`
	Operands string `json:"operands,omitempty"`
}

// Convert transforms RetDec export JSON into NormalizedBinary.
// The input should be a RetDecExport JSON blob produced by the RetDec
// decompiler. If the input is not []byte, an error is returned.
func (RetDecAdapter) Convert(ctx context.Context, input any) (*reverse.NormalizedBinary, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("retdec adapter expects []byte input (JSON export)")
	}

	var export RetDecExport
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
		Tool:      "retdec",
		Functions: funcs,
	}, nil
}
