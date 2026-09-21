package services

import (
	"strconv"
	"testing"
	"time"

	"codergag/internal/graph"
)

func TestCheckInterception_SubjectAbsent(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewAntiLoopDetector(r)
	_, err := svc.CheckInterception("p", "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCheckInterception_SubjectWrongProject(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, map[string]any{})
	svc := NewAntiLoopDetector(r)
	_, err := svc.CheckInterception("other", "bf1")
	if err == nil {
		t.Fatal("expected error for cross-project")
	}
}

func TestCheckInterception_NoIssues(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	svc := NewAntiLoopDetector(r)
	res, err := svc.CheckInterception("p", "bf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.LoopDetected || res.HallucinationRisk {
		t.Fatalf("expected no issues, got %+v", res)
	}
}

func TestCheckInterception_LoopingClaims(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	bf, _ := r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	for i := 0; i < 4; i++ {
		h, _ := r.UpsertNode("Hypothesis", map[string]any{
			"project_id": "p", "subject_id": "bf1", "claim": "This function validates input",
		}, map[string]any{"hypothesis_id": "h" + strconv.Itoa(i)})
		r.Link("SUSPECTED_AS", bf.ID, h.ID, nil)
	}
	svc := NewAntiLoopDetector(r)
	res, err := svc.CheckInterception("p", "bf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.LoopDetected {
		t.Fatal("expected loop detection")
	}
}

func TestCheckInterception_Contradictions(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	h1, _ := r.UpsertNode("Hypothesis", map[string]any{
		"project_id": "p", "subject_id": "bf1", "claim": "This function validates input",
		"confidence": 0.9,
	}, nil)
	h2, _ := r.UpsertNode("Hypothesis", map[string]any{
		"project_id": "p", "subject_id": "bf1", "claim": "This function does not validate input",
		"confidence": 0.8,
	}, nil)
	r.Link("SUSPECTED_AS", "bf1", h1.ID, nil)
	r.Link("SUSPECTED_AS", "bf1", h2.ID, nil)
	svc := NewAntiLoopDetector(r)
	res, err := svc.CheckInterception("p", "bf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.HallucinationRisk {
		t.Fatal("expected hallucination risk detection")
	}
}

func TestCheckInterception_HighConfidenceUnverified(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	_, _ = r.UpsertNode("Hypothesis", map[string]any{
		"project_id": "p", "subject_id": "bf1", "claim": "Does something",
	}, nil)
	r.Link("SUSPECTED_AS", "bf1", "h1", nil)
	ev, _ := r.UpsertNode("Evidence", map[string]any{
		"project_id":  "p", "subject_id": "bf1",
		"description": "High conf unverified evidence",
		"source":      "manual",
		"method":      "manual",
	}, map[string]any{"confidence": 0.95})
	r.Link("REFERENCES", "bf1", ev.ID, nil)
	svc := NewAntiLoopDetector(r)
	res, err := svc.CheckInterception("p", "bf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.HallucinationRisk {
		t.Fatal("expected hallucination risk for high-confidence unverified evidence")
	}
}

func TestCheckInterception_MissingEvidence(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	h, _ := r.UpsertNode("Hypothesis", map[string]any{
		"project_id": "p", "subject_id": "bf1", "claim": "Does something",
	}, nil)
	r.Link("SUSPECTED_AS", "bf1", h.ID, nil)
	svc := NewAntiLoopDetector(r)
	res, err := svc.CheckInterception("p", "bf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.HallucinationRisk {
		t.Fatal("expected hallucination risk for hypotheses without evidence")
	}
}

func TestCheckInterception_StaleClaims(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 7; i++ {
		h, _ := r.UpsertNode("Hypothesis", map[string]any{
			"project_id": "p", "subject_id": "bf1",
			"claim": "Hypothesis " + strconv.Itoa(i),
			"timestamp": now,
		}, nil)
		r.Link("SUSPECTED_AS", "bf1", h.ID, nil)
	}
	svc := NewAntiLoopDetector(r)
	res, err := svc.CheckInterception("p", "bf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.LoopDetected {
		t.Fatal("expected stale claims detection")
	}
}

func TestCheckInterceptionByAddress_Found(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	_, _ = r.UpsertNode("BinaryFunction", map[string]any{
		"id": "bf1", "project_id": "p", "binary_id": "bin1", "address": "0x401000",
	}, nil)
	svc := NewAntiLoopDetector(r)
	res, err := svc.CheckInterceptionByAddress("p", "bin1", "0x401000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestCheckInterceptionByAddress_NotFound(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewAntiLoopDetector(r)
	_, err := svc.CheckInterceptionByAddress("p", "bin1", "0x401000")
	if err == nil {
		t.Fatal("expected error for nonexistent function")
	}
}

func TestListInterceptedSubjects(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	bf, _ := r.UpsertNode("BinaryFunction", map[string]any{"id": "bf1", "project_id": "p"}, nil)
	h, _ := r.UpsertNode("Hypothesis", map[string]any{
		"project_id": "p", "subject_id": bf.ID, "claim": "Does something",
	}, nil)
	r.Link("SUSPECTED_AS", bf.ID, h.ID, nil)
	svc := NewAntiLoopDetector(r)
	issues, err := svc.ListInterceptedSubjects("p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 intercepted subject, got %d", len(issues))
	}
}

func TestListInterceptedSubjects_NoHypotheses(t *testing.T) {
	r := graph.NewMemoryGraphRepository()
	svc := NewAntiLoopDetector(r)
	issues, err := svc.ListInterceptedSubjects("p")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues, got %d", len(issues))
	}
}
