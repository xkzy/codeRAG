package services

import (
	"fmt"
	"testing"

	"codergag/internal/graph"
	"codergag/internal/ids"
	"codergag/internal/reverse"
)

func makeBinary(idx, fnCount, bbPerFn, insnPerBB int) reverse.NormalizedBinary {
	binID := fmt.Sprintf("benchbin%d", idx)
	functions := make([]reverse.NormalizedFunction, fnCount)
	for f := 0; f < fnCount; f++ {
		addr := fmt.Sprintf("0x401%03x", f*0x100)
		calls := []string{}
		if f+1 < fnCount {
			calls = []string{fmt.Sprintf("0x401%03x", (f+1)*0x100)}
		}
		bbList := make([]reverse.NormalizedBasicBlock, bbPerFn)
		for bb := 0; bb < bbPerFn; bb++ {
			bbAddr := fmt.Sprintf("%s_%02x", addr, bb)
			insns := make([]reverse.NormalizedInstruction, insnPerBB)
			for ins := 0; ins < insnPerBB; ins++ {
				insnAddr := fmt.Sprintf("%s_%02x", bbAddr, ins)
				dataRefs := []reverse.NormalizedDataRef{}
				if ins%3 == 0 && f%5 == 0 {
					dataRefs = []reverse.NormalizedDataRef{
						{FromAddress: insnAddr, ToAddress: "0x1000", Type: "read", Size: 4},
					}
				}
				codeRefs := []reverse.NormalizedCodeRef{}
				if ins == insnPerBB-1 && len(calls) > 0 {
					codeRefs = []reverse.NormalizedCodeRef{
						{FromAddress: insnAddr, ToAddress: calls[0], Type: "call"},
					}
				}
				insns[ins] = reverse.NormalizedInstruction{
					Address:  insnAddr,
					Mnemonic: []string{"mov", "push", "pop", "add", "sub", "cmp", "jmp", "call"}[ins%8],
					Operands: fmt.Sprintf("r%d, [0x1000]", ins%8),
					DataRefs: dataRefs,
					CodeRefs: codeRefs,
				}
			}
			bbList[bb] = reverse.NormalizedBasicBlock{
				Address:     bbAddr,
				Instruction: insns,
			}
		}
		size := 256
		functions[f] = reverse.NormalizedFunction{
			Address:          addr,
			Name:             fmt.Sprintf("func_%d", f),
			Size:             &size,
			Calls:            calls,
			Strings:          []string{fmt.Sprintf("string_in_func_%d", f)},
			DecompilerOutput: fmt.Sprintf("int func_%d(int arg) { return arg + %d; }", f, f),
			BasicBlocks:      bbList,
		}
	}
	return reverse.NormalizedBinary{
		BinaryID:  binID,
		Path:      fmt.Sprintf("/samples/bench%d.bin", idx),
		Sha256:    fmt.Sprintf("sha256_%d", idx),
		Tool:      "bench",
		Functions: functions,
	}
}

func BenchmarkImportBinary_Small(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 10, 3, 5)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data.BinaryID = fmt.Sprintf("benchbin%d", i)
		data.Functions[0].Address = fmt.Sprintf("0x401%03x", i)
		_, _ = app.Reverse.ImportBinary("p", data)
	}
}

func BenchmarkImportBinary_Medium(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 50, 5, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data.BinaryID = fmt.Sprintf("benchbin%d", i)
		_, _ = app.Reverse.ImportBinary("p", data)
	}
}

func BenchmarkImportBinary_Large(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 200, 10, 20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		data.BinaryID = fmt.Sprintf("benchbin%d", i)
		_, _ = app.Reverse.ImportBinary("p", data)
	}
}

func BenchmarkFindBinaryFunction_Indexed(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	stableID := ids.BinFuncID(data.BinaryID, data.Functions[50].Address)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.Graph.GetNode(stableID)
	}
}

func BenchmarkFindBinaryFunction_ByAddress(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Graph.FindNodes("BinaryFunction", map[string]any{
			"project_id": "p",
			"address":    data.Functions[50].Address,
		})
	}
}

func BenchmarkGetFunctionCallers(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	callerID := ids.BinFuncID(data.BinaryID, data.Functions[0].Address)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Graph.Neighbors(callerID, "CALLS", graph.DirIn)
	}
}

func BenchmarkGetFunctionCallees(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	callerID := ids.BinFuncID(data.BinaryID, data.Functions[0].Address)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Graph.Neighbors(callerID, "CALLS", graph.DirOut)
	}
}

func BenchmarkGetBasicBlocks(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	fnID := ids.BinFuncID(data.BinaryID, data.Functions[0].Address)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Graph.Neighbors(fnID, "CONTAINS", graph.DirOut)
	}
}

func BenchmarkGetCFG(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	fnID := ids.BinFuncID(data.BinaryID, data.Functions[0].Address)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cfg, _ := app.Graph.Neighbors(fnID, "CONTAINS", graph.DirOut)
		for _, edgeNode := range cfg {
			if edgeNode.Node.Kind == "BasicBlock" {
				_, _ = app.Graph.Neighbors(edgeNode.Node.ID, "CONTAINS", graph.DirOut)
			}
		}
	}
}

func BenchmarkExpandCallGraph_Local(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 50, 3, 5)
	_, _ = app.Reverse.ImportBinary("p", data)
	fnID := ids.BinFuncID(data.BinaryID, data.Functions[0].Address)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		visited := map[string]bool{}
		expandCalls(app, fnID, visited, 2)
	}
}

func expandCalls(app *Application, nodeID string, visited map[string]bool, depth int) {
	if depth <= 0 || visited[nodeID] {
		return
	}
	visited[nodeID] = true
	callees, _ := app.Graph.Neighbors(nodeID, "CALLS", graph.DirOut)
	for _, cn := range callees {
		expandCalls(app, cn.Node.ID, visited, depth-1)
	}
}

func BenchmarkContextCompilation_BinaryFunction(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	fnID := ids.BinFuncID(data.BinaryID, data.Functions[50].Address)
	app.Graph.GetNode(fnID)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = app.Context.Compile(ContextRequest{
			ProjectID: "p",
			Question:  "Tell me about the function at " + data.Functions[50].Address,
			Level:     CtxLevelStructured,
			Limit:     10,
		})
	}
}

func BenchmarkContextCompilation_DeepCallGraph(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		visited := map[string]bool{}
		expandCalls(app, ids.BinFuncID(data.BinaryID, data.Functions[0].Address), visited, 5)
		_, _ = app.Context.Compile(ContextRequest{
			ProjectID: "p",
			Question:  "Analyze the call chain starting from func_0",
			Level:     CtxLevelCompressed,
			Limit:     20,
		})
	}
}

func BenchmarkGraphExpansion_FullBinary(b *testing.B) {
	app := ApplicationInMemory()
	data := makeBinary(0, 100, 5, 10)
	_, _ = app.Reverse.ImportBinary("p", data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		nodes, _ := app.Graph.FindNodes("BinaryFunction", map[string]any{"project_id": "p"})
		for _, n := range nodes {
			_, _ = app.Graph.Neighbors(n.ID, "CALLS", graph.DirOut)
			_, _ = app.Graph.Neighbors(n.ID, "CALLS", graph.DirIn)
			_, _ = app.Graph.Neighbors(n.ID, "CONTAINS", graph.DirOut)
		}
	}
}
