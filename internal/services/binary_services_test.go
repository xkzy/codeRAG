package services

import (
	"context"
	"testing"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/reverse"
)

func TestBinaryVerificationService_RecordVerificationTest(t *testing.T) {
	app := ApplicationInMemory()
	svc := NewBinaryVerificationService(app.Graph)

	testCase := VerificationTestCase{
		Name:         "test_parse",
		Input:        map[string]any{"buf": []byte{1, 2, 3}},
		BinaryOutput: map[string]any{"result": 0},
		PortedOutput: map[string]any{"result": 0},
		Match:        true,
	}

	result, err := svc.RecordVerificationTest("p", "bin1", "0x401000", "test_parse", testCase)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Error("expected result, got nil")
	}
	if result["test_id"] == nil {
		t.Error("expected test_id in result")
	}
}

func TestPortingService_RecordSemanticMapping(t *testing.T) {
	app := ApplicationInMemory()
	svc := NewPortingService(app.Graph)

	mapping := SemanticMapping{
		SourceConstruct:    "int32_t",
		SemanticMeaning:    "32-bit signed integer",
		TargetConstruct:    "i32",
		TranslationRule:   "preserves wraparound",
		CompatibilityIssue: "",
		Confidence:         0.95,
	}

	result, err := svc.RecordSemanticMapping("p", "bin1", "0x401000", mapping)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Error("expected result, got nil")
	}
}

func TestPortingService_RecordPortingDecision(t *testing.T) {
	app := ApplicationInMemory()
	svc := NewPortingService(app.Graph)

	decision := PortingDecision{
		OriginalConstruct: "malloc/free",
		TargetConstruct:   "Box::new/Drop",
		Reason:           "Rust memory safety",
		Confidence:       0.9,
	}

	result, err := svc.RecordPortingDecision("p", "bin1", "0x401000", "rust", decision)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Error("expected result, got nil")
	}
}

func TestBinaryAnalysisService_GetCFG(t *testing.T) {
	app := ApplicationInMemory()
	svc := NewBinaryAnalysisService(app.Graph)

	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Tool:     "test",
		Functions: []reverse.NormalizedFunction{
			{
				Address: "0x401000",
				Name:    "main",
				BasicBlocks: []reverse.NormalizedBasicBlock{
					{Address: "0x401000", Instruction: []reverse.NormalizedInstruction{
						{Address: "0x401000", Mnemonic: "push", Operands: "rbp"},
					}},
				},
			},
		},
	}
	_, err := app.Reverse.ImportBinary("p", data)
	if err != nil {
		t.Fatal(err)
	}

	cfg, err := svc.GetCFG("p", "bin1", "0x401000")
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil {
		t.Error("expected CFG, got nil")
	}
}

func TestBinaryAnalysisService_GetDataFlow(t *testing.T) {
	app := ApplicationInMemory()
	svc := NewBinaryAnalysisService(app.Graph)

	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Tool:     "test",
		Functions: []reverse.NormalizedFunction{
			{
				Address: "0x401000",
				Name:    "main",
			},
		},
	}
	_, err := app.Reverse.ImportBinary("p", data)
	if err != nil {
		t.Fatal(err)
	}

	df, err := svc.GetDataFlow("p", "bin1", "0x401000")
	if err != nil {
		t.Fatal(err)
	}
	if df == nil {
		t.Error("expected data flow, got nil")
	}
}

func TestBinaryAnalysisService_InvalidateAndRefresh(t *testing.T) {
	app := ApplicationInMemory()
	svc := NewBinaryAnalysisService(app.Graph)

	data := reverse.NormalizedBinary{
		BinaryID: "bin1",
		Tool:     "test",
		Functions: []reverse.NormalizedFunction{
			{Address: "0x401000", Name: "main"},
		},
	}
	_, err := app.Reverse.ImportBinary("p", data)
	if err != nil {
		t.Fatal(err)
	}

	cfg1, _ := svc.GetCFG("p", "bin1", "0x401000")

	svc.InvalidateBinaryAnalysis("p", "bin1")

	cfg2, _ := svc.GetCFG("p", "bin1", "0x401000")
	if cfg1 == nil || cfg2 == nil {
		t.Error("expected valid CFGs")
	}
}

func TestBinaryBackgroundWorker_SubmitAndProcess(t *testing.T) {
	app := ApplicationInMemory()
	worker := NewBinaryBackgroundWorker(app.Graph)

	job := binaryAnalysisJob{
		ProjectID:    "p",
		BinaryID:     "bin1",
		FunctionAddr: "0x401000",
		JobType:      JobTypeCFG,
		Priority:     1,
	}

	ok := worker.Submit(job)
	if !ok {
		t.Error("expected submit to succeed")
	}

	worker.Start(context.Background())
	worker.Stop()
}

func TestGraphBatchOperations(t *testing.T) {
	mem := graph.NewMemoryGraphRepository()
	graphRepo := &batchableGraph{mem}

	items := []graph.NodeBatchItem{
		{Identity: map[string]any{"project_id": "p", "name": "test1"}, Properties: map[string]any{"value": 1}},
		{Identity: map[string]any{"project_id": "p", "name": "test2"}, Properties: map[string]any{"value": 2}},
	}

	nodes, err := graphRepo.UpsertNodesBatch("TestNode", items)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(nodes))
	}

	edges := []graph.EdgeBatchItem{
		{Kind: "LINKS", FromID: nodes[0].ID, ToID: nodes[1].ID},
	}
	edgeResults, err := graphRepo.LinkBatch(edges)
	if err != nil {
		t.Fatal(err)
	}
	if len(edgeResults) != 1 {
		t.Errorf("expected 1 edge, got %d", len(edgeResults))
	}
}

type batchableGraph struct {
	*graph.MemoryGraphRepository
}

func (g *batchableGraph) UpsertNodesBatch(kind string, items []graph.NodeBatchItem) ([]*models.Node, error) {
	return g.MemoryGraphRepository.UpsertNodesBatch(kind, items)
}

func (g *batchableGraph) LinkBatch(items []graph.EdgeBatchItem) ([]*models.Edge, error) {
	return g.MemoryGraphRepository.LinkBatch(items)
}
