package objdump

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"codergag/internal/reverse"
)

// ObjdumpAdapter implements the reverse.Adapter interface for objdump/readelf output.
type ObjdumpAdapter struct{}

// Name returns the adapter name.
func (ObjdumpAdapter) Name() string { return "objdump" }

// ObjdumpExport represents a JSON wrapper for objdump/readelf text output.
type ObjdumpExport struct {
	BinaryID      string `json:"binary_id"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	ObjdumpOutput string `json:"objdump_output,omitempty"`
	ReadelfOutput string `json:"readelf_output,omitempty"`
}

// Convert transforms objdump/readelf text output into NormalizedBinary.
func (ObjdumpAdapter) Convert(ctx context.Context, input any) (*reverse.NormalizedBinary, error) {
	data, ok := input.([]byte)
	if !ok {
		return nil, errors.New("objdump adapter expects []byte input (JSON wrapper with objdump/readelf output)")
	}

	var export ObjdumpExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, err
	}

	functions := parseObjdump(export.ObjdumpOutput)
	symbols := parseReadelf(export.ReadelfOutput)

	// Merge symbols into functions (add names to address-matched functions)
	symMap := make(map[string]string)
	for _, s := range symbols {
		symMap[s.Address] = s.Name
	}

	for i := range functions {
		if name, ok := symMap[functions[i].Address]; ok && functions[i].Name == "" {
			functions[i].Name = name
		}
	}

	return &reverse.NormalizedBinary{
		BinaryID:  export.BinaryID,
		Path:      export.Path,
		Sha256:    export.SHA256,
		Tool:      "objdump",
		Functions: functions,
	}, nil
}

// Symbol represents a symbol from readelf -s.
type Symbol struct {
	Address string
	Name    string
}

// parseObjdump parses `objdump -d` output for functions and instructions.
func parseObjdump(output string) []reverse.NormalizedFunction {
	if output == "" {
		return nil
	}

	funcRegex := regexp.MustCompile(`^([0-9a-fA-F]+) <([^>]+)>:`)
	insnRegex := regexp.MustCompile(`^\s+([0-9a-fA-F]+):\s+([0-9a-fA-F\s]+)\s+(\w+)\s*(.*)$`)

	var functions []reverse.NormalizedFunction
	var currentFn *reverse.NormalizedFunction
	var currentBB *reverse.NormalizedBasicBlock

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		// Function header: "0000000000401000 <main>:"
		if matches := funcRegex.FindStringSubmatch(line); matches != nil {
			if currentFn != nil {
				if currentBB != nil && len(currentBB.Instruction) > 0 {
					currentFn.BasicBlocks = append(currentFn.BasicBlocks, *currentBB)
				}
				functions = append(functions, *currentFn)
			}

			addr := matches[1]
			name := matches[2]
			currentFn = &reverse.NormalizedFunction{
				Address:     addr,
				Name:        name,
				BasicBlocks: []reverse.NormalizedBasicBlock{},
			}
			currentBB = &reverse.NormalizedBasicBlock{
				Address:     addr,
				Instruction: []reverse.NormalizedInstruction{},
			}
			continue
		}

		// Instruction: "  401000:       55                      push   %rbp"
		if currentFn != nil {
			if matches := insnRegex.FindStringSubmatch(line); matches != nil {
				insnAddr := matches[1]
				mnemonic := matches[3]
				operands := strings.TrimSpace(matches[4])

				// Check if this starts a new basic block (simple heuristic: jump/call/ret)
				if isControlFlow(mnemonic) && len(currentBB.Instruction) > 0 {
					currentFn.BasicBlocks = append(currentFn.BasicBlocks, *currentBB)
					currentBB = &reverse.NormalizedBasicBlock{
						Address:     insnAddr,
						Instruction: []reverse.NormalizedInstruction{},
					}
				}

				currentBB.Instruction = append(currentBB.Instruction, reverse.NormalizedInstruction{
					Address:  insnAddr,
					Mnemonic: mnemonic,
					Operands: operands,
				})
			}
		}
	}

	if currentFn != nil {
		if currentBB != nil && len(currentBB.Instruction) > 0 {
			currentFn.BasicBlocks = append(currentFn.BasicBlocks, *currentBB)
		}
		functions = append(functions, *currentFn)
	}

	return functions
}

// parseReadelf parses `readelf -s` output for symbols.
func parseReadelf(output string) []Symbol {
	if output == "" {
		return nil
	}

	// readelf -s output format:
	// Num:    Value          Size Type    Bind   Vis      Ndx Name
	//   0: 0000000000000000     0 NOTYPE  LOCAL  DEFAULT  UND
	//   1: 0000000000401000    43 FUNC    GLOBAL DEFAULT   14 main
	symRegex := regexp.MustCompile(`^\s*\d+:\s+([0-9a-fA-F]+)\s+\d+\s+\w+\s+\w+\s+\w+\s+\w+\s+(\S+)$`)

	var symbols []Symbol
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if matches := symRegex.FindStringSubmatch(line); matches != nil {
			addr := matches[1]
			name := matches[2]
			// Skip empty names and section symbols
			if name != "" && !strings.HasPrefix(name, ".") {
				symbols = append(symbols, Symbol{Address: addr, Name: name})
			}
		}
	}

	return symbols
}

// isControlFlow returns true for instructions that typically end a basic block.
func isControlFlow(mnemonic string) bool {
	m := strings.ToLower(mnemonic)
	return m == "jmp" || m == "je" || m == "jne" || m == "jg" || m == "jl" ||
		m == "jge" || m == "jle" || m == "ja" || m == "jb" || m == "call" ||
		m == "ret" || m == "retq" || m == "iret" || m == "syscall" ||
		m == "int" || strings.HasPrefix(m, "j") && len(m) == 2
}
