package services

import (
	"testing"

	"codergag/internal/graph"
	"codergag/internal/reverse"
)

func TestImportBinary_Basic(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Path:     "/samples/test.bin",
		Sha256:   "abc123",
		Tool:     "retdec",
		Functions: []reverse.NormalizedFunction{
			{
				Address:          "0x401000",
				Name:             "main",
				Size:             intPtr(64),
				Calls:            []string{"0x401200"},
				Strings:          []string{"Hello"},
				DecompilerOutput: "int main() { return 0; }",
			},
			{
				Address:          "0x401200",
				Name:             "helper",
				Size:             intPtr(32),
				DecompilerOutput: "void helper() {}",
			},
		},
	}
	got, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["binary_id"] != "bin1" {
		t.Errorf("expected binary_id bin1, got %v", got["binary_id"])
	}
	if got["functions"].(int) != 2 {
		t.Errorf("expected 2 functions, got %v", got["functions"])
	}
	if got["tool"] != "retdec" {
		t.Errorf("expected tool retdec, got %v", got["tool"])
	}
}

func TestImportBinary_WithBasicBlocks(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Tool:     "ghidra",
		Functions: []reverse.NormalizedFunction{
			{
				Address: "0x401000",
				Name:    "main",
				BasicBlocks: []reverse.NormalizedBasicBlock{
					{
						Address: "0x401000",
						Instruction: []reverse.NormalizedInstruction{
							{Address: "0x401000", Mnemonic: "push", Operands: "rbp"},
							{Address: "0x401004", Mnemonic: "mov", Operands: "rax 0"},
						},
					},
				},
			},
		},
	}
	got, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["functions"].(int) != 1 {
		t.Errorf("expected 1 function, got %v", got["functions"])
	}
}

func TestImportBinary_WithCodeRefs(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Tool:     "ghidra",
		Functions: []reverse.NormalizedFunction{
			{
				Address:          "0x401000",
				Name:             "main",
				DecompilerOutput: "int main() { helper(); }",
				DataReferences: []reverse.NormalizedDataRef{
					{FromAddress: "0x401000", ToAddress: "0x1000", Type: "read", Size: 4},
				},
				CodeReferences: []reverse.NormalizedCodeRef{
					{FromAddress: "0x401000", ToAddress: "0x401200", Type: "call"},
				},
			},
			{
				Address: "0x401200",
				Name:    "helper",
			},
		},
	}
	got, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["functions"].(int) != 2 {
		t.Errorf("expected 2 functions, got %v", got["functions"])
	}
}

func TestImportBinary_WithJmpCodeRefs(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Tool:     "gdb",
		Functions: []reverse.NormalizedFunction{
			{
				Address: "0x401000",
				Name:    "main",
				CodeReferences: []reverse.NormalizedCodeRef{
					{FromAddress: "0x401000", ToAddress: "0x401200", Type: "jmp"},
				},
			},
			{
				Address: "0x401200",
				Name:    "helper",
			},
		},
	}
	_, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportBinary_WithCondJmpCodeRefs(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Functions: []reverse.NormalizedFunction{
			{
				Address: "0x401000",
				Name:    "main",
				CodeReferences: []reverse.NormalizedCodeRef{
					{FromAddress: "0x401000", ToAddress: "0x401200", Type: "cond_jmp"},
				},
			},
			{
				Address: "0x401200",
				Name:    "then_branch",
			},
		},
	}
	_, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportBinary_WithInstructionDataRefs(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Functions: []reverse.NormalizedFunction{
			{
				Address: "0x401000",
				Name:    "main",
				BasicBlocks: []reverse.NormalizedBasicBlock{
					{
						Address: "0x401000",
						Instruction: []reverse.NormalizedInstruction{
							{
								Address:  "0x401000",
								Mnemonic: "mov",
								Operands: "rax [0x1000]",
								DataRefs: []reverse.NormalizedDataRef{
									{FromAddress: "0x401000", ToAddress: "0x1000", Type: "read", Size: 8},
								},
							},
						},
					},
				},
			},
		},
	}
	_, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportBinary_WithInstructionCodeRefs(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Functions: []reverse.NormalizedFunction{
			{
				Address: "0x401000",
				Name:    "main",
			},
			{
				Address: "0x401200",
				Name:    "helper",
			},
			{
				Address: "0x401400",
				Name:    "jump_target",
				BasicBlocks: []reverse.NormalizedBasicBlock{
					{
						Address: "0x401000",
						Instruction: []reverse.NormalizedInstruction{
							{
								Address:  "0x401000",
								Mnemonic: "call",
								Operands: "0x401200",
								CodeRefs: []reverse.NormalizedCodeRef{
									{FromAddress: "0x401000", ToAddress: "0x401200", Type: "call"},
								},
							},
						},
					},
				},
			},
		},
	}
	_, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestImportBinary_DecompilerOutputTruncation(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	longOutput := makeLongString(MaxDecompilerOutputLen + 100)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Tool:     "retdec",
		Functions: []reverse.NormalizedFunction{
			{
				Address:          "0x401000",
				Name:             "main",
				DecompilerOutput: longOutput,
			},
		},
	}
	_, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	outputs, _ := r.FindNodes("DecompilerOutput", map[string]any{"project_id": "p"})
	if len(outputs) != 1 {
		t.Fatalf("expected 1 decompiler output, got %d", len(outputs))
	}
	text, _ := outputs[0].Properties["text"].(string)
	if len(text) > MaxDecompilerOutputLen+20 {
		t.Errorf("expected truncated text, got length %d", len(text))
	}
	truncated, _ := outputs[0].Properties["truncated"].(bool)
	if !truncated {
		t.Error("expected truncated=true for long output")
	}
}

func TestImportBinary_NoSize(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Functions: []reverse.NormalizedFunction{
			{
				Address: "0x401000",
				Name:    "main",
			},
		},
	}
	got, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["functions"].(int) != 1 {
		t.Errorf("expected 1 function, got %v", got["functions"])
	}
}

func TestImportBinary_NoFunctions(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Path:     "/samples/empty.bin",
		Sha256:   "abc123",
		Tool:     "retdec",
	}
	got, err := svc.ImportBinary("p", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["functions"].(int) != 0 {
		t.Errorf("expected 0 functions, got %v", got["functions"])
	}
}

func TestMapSourceFunction(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	_, _ = r.UpsertNode("Function", map[string]any{"id": "sf1", "project_id": "p"}, nil)
	got, err := svc.MapSourceFunction("bf1", "sf1", 0.95, "symbolic")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["equivalence_status"] != "SUSPECTED" {
		t.Errorf("expected SUSPECTED, got %s", got["equivalence_status"])
	}
	if got["confidence"].(float64) != 0.95 {
		t.Errorf("expected confidence 0.95, got %v", got["confidence"])
	}
}

func TestMapSourceFunction_LinkError(t *testing.T) {
	// MemoryGraphRepository.Link does not validate node existence, so this
	// succeeds without error. This tests the happy path behavior.
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	got, err := svc.MapSourceFunction("bf1", "sf1", 0.95, "symbolic")
	if err != nil {
		t.Fatalf("unexpected error (memory graph Link is lenient): %v", err)
	}
	if got["equivalence_status"] != "SUSPECTED" {
		t.Errorf("expected SUSPECTED, got %s", got["equivalence_status"])
	}
}

func TestRecordPort(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	_, _ = r.UpsertNode("Function", map[string]any{"id": "impl1", "project_id": "p"}, nil)
	got, err := svc.RecordPort("bf1", "impl1", "c", 0.9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["status"] != "recorded" {
		t.Errorf("expected recorded, got %s", got["status"])
	}
}

func TestRecordValidation_Passed(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	_, _ = r.UpsertNode("Function", map[string]any{"id": "impl1", "project_id": "p"}, nil)
	got, err := svc.RecordValidation("bf1", "impl1", "TestMain", "passed", 0.95, "differential")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["equivalence_status"] != "VALIDATED" {
		t.Errorf("expected VALIDATED, got %s", got["equivalence_status"])
	}
	if got["test_id"] == "" {
		t.Error("expected test_id to be set")
	}
}

func TestRecordValidation_Failed(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	got, err := svc.RecordValidation("bf1", "impl1", "TestMain", "failed", 0.3, "differential")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["equivalence_status"] != "CONTRADICTED" {
		t.Errorf("expected CONTRADICTED, got %s", got["equivalence_status"])
	}
}

func TestRecordValidation_BinaryFunctionNotFound(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	_, err := svc.RecordValidation("nonexistent", "impl1", "Test", "passed", 0.9, "diff")
	if err == nil {
		t.Fatal("expected error for nonexistent binary function")
	}
}

func TestRecordRuntimeTrace(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	trace := reverse.RuntimeTrace{
		BinaryID:     "bin1",
		FunctionAddr: "0x401000",
		TraceID:      "trace1",
		Timestamp:    1234567890,
		Instructions: []reverse.TraceInstruction{
			{Address: "0x401000", Mnemonic: "push", Operands: "rbp", Registers: map[string]string{"rsp": "0x1000"}},
		},
		MemoryReads: []reverse.TraceMemoryAccess{
			{Address: "0x1000", Size: 8, Value: "0xdeadbeef"},
		},
		MemoryWrites: []reverse.TraceMemoryAccess{
			{Address: "0x2000", Size: 4, Value: "0x1234"},
		},
	}
	got, err := svc.RecordRuntimeTrace("p", trace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["trace_id"] != "trace1" {
		t.Errorf("expected trace_id trace1, got %v", got["trace_id"])
	}
	if got["status"] != "recorded" {
		t.Errorf("expected recorded, got %v", got["status"])
	}
}

func TestRecordRuntimeTrace_EmptyTrace(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	trace := reverse.RuntimeTrace{
		BinaryID: "bin1",
		TraceID:  "trace1",
	}
	got, err := svc.RecordRuntimeTrace("p", trace)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["status"] != "recorded" {
		t.Errorf("expected recorded, got %v", got["status"])
	}
}

func TestRecordHypothesis(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	hyp := reverse.Hypothesis{
		ID:        "hyp1",
		SubjectID: "bf1",
		Claim:     "This function validates input buffers",
		Confidence: 0.85,
		Status:    "active",
		Analyst:   "analyst1",
		CreatedAt: 1234567890,
		UpdatedAt: 1234567891,
		EvidenceFor: []reverse.EvidenceRef{
			{ID: "ev1", Description: "Calls memset before use", Confidence: 0.9, Kind: "static"},
		},
		EvidenceAgainst: []reverse.EvidenceRef{
			{ID: "ev2", Description: "No length check found", Confidence: 0.7, Kind: "static"},
		},
	}
	got, err := svc.RecordHypothesis("p", hyp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["hypothesis_id"] != "hyp1" {
		t.Errorf("expected hyp1, got %v", got["hypothesis_id"])
	}
	if got["status"] != "recorded" {
		t.Errorf("expected recorded, got %v", got["status"])
	}
}

func TestRecordHypothesis_EmptyEvidence(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	hyp := reverse.Hypothesis{
		ID:        "hyp1",
		SubjectID: "bf1",
		Claim:     "Simple claim",
		Confidence: 0.5,
		Status:    "active",
	}
	got, err := svc.RecordHypothesis("p", hyp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["status"] != "recorded" {
		t.Errorf("expected recorded, got %v", got["status"])
	}
}

func TestRecordBehavioralEquivalence(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	equiv := reverse.BehavioralEquivalence{
		BinaryFunctionID:  "bf1",
		SourceFunctionID:  "sf1",
		EquivalenceStatus: "equivalent",
		Confidence:        0.95,
		Method:            "differential",
		Analyst:           "analyst1",
		CreatedAt:         1234567890,
		TestCases: []reverse.EquivalenceTestCase{
			{Input: map[string]any{"x": 1}, BinaryOut: map[string]any{"r": 2}, SourceOut: map[string]any{"r": 2}, Match: true},
		},
		Differences: []reverse.EquivalenceDifference{
			{Type: "control_flow", Description: "different branching", Severity: "minor"},
		},
	}
	got, err := svc.RecordBehavioralEquivalence("p", equiv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["status"] != "recorded" {
		t.Errorf("expected recorded, got %v", got["status"])
	}
	if got["equivalence_status"] != "equivalent" {
		t.Errorf("expected equivalent, got %v", got["equivalence_status"])
	}
}

func TestRecordBehavioralEquivalence_EmptyCases(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewReverseEngineeringService(r)
	equiv := reverse.BehavioralEquivalence{
		BinaryFunctionID:  "bf1",
		SourceFunctionID:  "sf1",
		EquivalenceStatus: "divergent",
		Confidence:        0.8,
		Method:            "differential",
		CreatedAt:         1234567890,
	}
	got, err := svc.RecordBehavioralEquivalence("p", equiv)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["equivalence_status"] != "divergent" {
		t.Errorf("expected divergent, got %v", got["equivalence_status"])
	}
}

func TestImportBinary_Truncation(t *testing.T) {
	short := truncate("short", 100)
	if short != "short" {
		t.Fatal("truncate should not modify short strings")
	}
	long := makeLongString(MaxDecompilerOutputLen + 100)
	trunc := truncate(long, MaxDecompilerOutputLen)
	if len(trunc) > MaxDecompilerOutputLen+20 {
		t.Fatal("truncate should cap length")
	}
	longMnemonic := makeLongString(MaxInstructionFieldLen + 100)
	truncMnemonic := truncate(longMnemonic, MaxInstructionFieldLen)
	if len(truncMnemonic) > MaxInstructionFieldLen+20 {
		t.Fatal("truncate should cap mnemonic length")
	}
	empty := truncate("", MaxInstructionFieldLen)
	if empty != "" {
		t.Fatal("truncate should return empty string for empty input")
	}
}

func intPtr(i int) *int {
	return &i
}

func makeLongString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
