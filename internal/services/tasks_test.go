package services

import (
	"path/filepath"
	"strings"
	"testing"

	"codergag/internal/graph"
)

func taskApp(t *testing.T) (*Application, string) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": cat21})
	app := ApplicationInMemory()
	app.Index.IndexRepository("p", dir, true, nil, false)
	return app, dir
}

func mustUpdate(t *testing.T, app *Application, id, agent string, p TaskPatch) *TaskState {
	t.Helper()
	ts, err := app.Tasks.Update("p", id, agent, p)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	return ts
}

func wantErr(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("expected error containing %q, got %v", substr, err)
	}
}

func newEvidence(t *testing.T, app *Application, subject string) string {
	t.Helper()
	n, _ := app.Refs.Lookup("p", subject)
	ev, err := app.Evidence.RecordEvidence("p", n.ID, "observed behaviour", map[string]any{"confidence": 0.9})
	if err != nil {
		t.Fatal(err)
	}
	return ev["id"].(string)
}

func TestTaskCreateValidatesReferencesAndMode(t *testing.T) {
	app, _ := taskApp(t)
	_, err := app.Tasks.Create("p", "port it", "implement", "claude", nil, []string{"func:a.go:parse_cat2l"})
	wantErr(t, err, "INVALID_REFERENCE")
	wantErr(t, err, "func:a.go:parse_cat21")
	_, err = app.Tasks.Create("p", "", "", "claude", nil, nil)
	wantErr(t, err, "goal is required")
	_, err = app.Tasks.Create("p", "x", "yolo", "claude", nil, nil)
	wantErr(t, err, "mode must be")
	ts, err := app.Tasks.Create("p", "port parse_cat21", "implement", "claude", []string{"no unsafe"}, []string{"func:a.go:parse_cat21"})
	if err != nil || ts.State != TaskUnknown || ts.Version != 1 || ts.CreatedBy != "claude" || len(ts.History) != 1 {
		t.Fatalf("%+v %v", ts, err)
	}
	got, _ := app.Tasks.Get("p", ts.TaskID)
	if got.Goal != "port parse_cat21" || got.References[0] != "func:a.go:parse_cat21" {
		t.Fatalf("round trip: %+v", got)
	}
	if _, err := app.Tasks.Get("other", ts.TaskID); err == nil {
		t.Fatal("tasks are project-scoped")
	}
}

func TestTaskStateMachineAndValidationGate(t *testing.T) {
	app, _ := taskApp(t)
	ts, _ := app.Tasks.Create("p", "g", "implement", "a", nil, nil)
	_, err := app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{State: TaskImplementing})
	wantErr(t, err, "illegal task transition UNKNOWN -> IMPLEMENTING")
	for _, st := range []string{TaskDiscovering, TaskAnalyzing, TaskImplementing, TaskVerifying} {
		mustUpdate(t, app, ts.TaskID, "a", TaskPatch{State: st})
	}
	_, err = app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{State: TaskValidated})
	wantErr(t, err, "no validation has been recorded")
	_, err = app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{State: TaskValidated,
		AddValidation: []ValidationEntry{{Name: "build", Status: "passed"}, {Name: "tests", Status: "failed"}}})
	wantErr(t, err, "latest result failed for tests")
	if cur, _ := app.Tasks.Get("p", ts.TaskID); cur.State != TaskVerifying || len(cur.Validation) != 0 {
		t.Fatalf("a rejected patch must change nothing: %+v", cur)
	}
	mustUpdate(t, app, ts.TaskID, "a", TaskPatch{AddValidation: []ValidationEntry{{Name: "build", Status: "passed"}, {Name: "tests", Status: "failed"}}})
	// A later passing run supersedes the earlier failure.
	done := mustUpdate(t, app, ts.TaskID, "a", TaskPatch{State: TaskValidated, AddValidation: []ValidationEntry{{Name: "tests", Status: "passed"}}})
	if done.State != TaskValidated {
		t.Fatalf("%+v", done)
	}
	// From VALIDATED the only way out is STALE.
	_, err = app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{State: TaskImplementing})
	wantErr(t, err, "illegal task transition")
}

func TestFactsNeedProvenanceAndInferencesStayInferences(t *testing.T) {
	app, _ := taskApp(t)
	ts, _ := app.Tasks.Create("p", "g", "", "a", nil, nil)
	_, err := app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{AddFacts: []TaskFact{{Text: "parse_cat21 is the entry", Kind: KindFact}}})
	wantErr(t, err, "needs checkable provenance")
	_, err = app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{AddFacts: []TaskFact{{Text: "x", Kind: KindFact, Source: "model"}}})
	wantErr(t, err, "needs checkable provenance")
	_, err = app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{AddFacts: []TaskFact{{Text: "x", Kind: KindFact, Source: "evidence:nope"}}})
	wantErr(t, err, "needs checkable provenance")
	ev := newEvidence(t, app, "func:a.go:parse_cat21")
	got := mustUpdate(t, app, ts.TaskID, "a", TaskPatch{AddFacts: []TaskFact{
		{Text: "parse_cat21 calls decode_item", Kind: KindFact, Source: "graph:CALLS func:a.go:parse_cat21->func:a.go:decode_item"},
		{Text: "matches trace", Kind: KindFact, Source: "evidence:" + ev},
		{Text: "probably handles malformed items", Source: "model"}, // defaults to INFERENCE
		{Text: "log line 7 says short read", Kind: KindObservation, Source: "tool:gdb"},
	}})
	kinds := []string{}
	for _, f := range got.KnownFacts {
		kinds = append(kinds, f.Kind)
	}
	if strings.Join(kinds, ",") != "FACT,FACT,INFERENCE,OBSERVATION" {
		t.Fatalf("kinds: %v", kinds)
	}
	_, err = app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{AddFacts: []TaskFact{{Text: "x", Kind: KindObservation}}})
	wantErr(t, err, "needs a source")
}

func TestHypothesisPromotionIsEvidenceGated(t *testing.T) {
	app, _ := taskApp(t)
	ts, _ := app.Tasks.Create("p", "g", "reverse_engineer", "a", nil, nil)
	ts = mustUpdate(t, app, ts.TaskID, "a", TaskPatch{Hypotheses: []HypothesisOp{
		{Claim: "0x401230 implements parse_cat21", SubjectID: "func:a.go:parse_cat21"}}})
	h := ts.Hypotheses[0]
	if h.Status != HypSuspected || h.Confidence != 0.5 || h.ID == "" {
		t.Fatalf("new hypotheses start SUSPECTED: %+v", h)
	}
	if n, _ := app.Graph.GetNode(h.ID); n == nil || n.Kind != "Hypothesis" {
		t.Fatalf("hypothesis should also be recorded in the graph (%s)", h.ID)
	}
	step := func(op HypothesisOp) error {
		op.ID = h.ID
		_, err := app.Tasks.Update("p", ts.TaskID, "a", TaskPatch{Hypotheses: []HypothesisOp{op}})
		return err
	}

	wantErr(t, step(HypothesisOp{Status: HypSupported}), "needs at least one supporting evidence")
	wantErr(t, step(HypothesisOp{Status: HypConfirmed}), "illegal hypothesis transition SUSPECTED -> CONFIRMED")
	wantErr(t, step(HypothesisOp{Status: HypContradicted}), "needs contradicting evidence")
	wantErr(t, step(HypothesisOp{EvidenceFor: []string{"ghost"}}), "does not exist")

	e1, e2 := newEvidence(t, app, "func:a.go:parse_cat21"), newEvidence(t, app, "func:a.go:decode_item")
	if err := step(HypothesisOp{Status: HypSupported, EvidenceFor: []string{e1}}); err != nil {
		t.Fatal(err)
	}
	wantErr(t, step(HypothesisOp{Status: HypConfirmed}), "at least two independent")
	if err := step(HypothesisOp{Status: HypConfirmed, EvidenceFor: []string{e2}}); err != nil {
		t.Fatal(err)
	}
	// Contradicting evidence overturns a confirmed claim, but only with evidence.
	e3 := newEvidence(t, app, "func:a.go:parse_cat21")
	if err := step(HypothesisOp{Status: HypContradicted, EvidenceAgainst: []string{e3}}); err != nil {
		t.Fatal(err)
	}
	// Competing hypotheses coexist.
	cur := mustUpdate(t, app, ts.TaskID, "a", TaskPatch{Hypotheses: []HypothesisOp{{Claim: "0x401230 is decode_item"}}})
	if len(cur.Hypotheses) != 2 || cur.Hypotheses[0].Status != HypContradicted || cur.Hypotheses[1].Status != HypSuspected {
		t.Fatalf("competing hypotheses: %+v", cur.Hypotheses)
	}
}

func TestConcurrentEditsAreDetected(t *testing.T) {
	app, _ := taskApp(t)
	ts, _ := app.Tasks.Create("p", "g", "", "a", nil, nil)
	v := ts.Version
	mustUpdate(t, app, ts.TaskID, "agent-a", TaskPatch{AddPlan: []string{"one"}, ExpectedVersion: &v})
	_, err := app.Tasks.Update("p", ts.TaskID, "agent-b", TaskPatch{AddPlan: []string{"two"}, ExpectedVersion: &v})
	wantErr(t, err, "CONFLICT")
	wantErr(t, err, "agent-a")
	// Steps and unknowns move between lists correctly.
	cur := mustUpdate(t, app, ts.TaskID, "agent-b", TaskPatch{CompleteStep: []string{"one"}, AddUnknowns: []string{"branch 5"}})
	if len(cur.Pending) != 0 || len(cur.Completed) != 1 || len(cur.Unknowns) != 1 {
		t.Fatalf("%+v", cur)
	}
	_, err = app.Tasks.Update("p", ts.TaskID, "b", TaskPatch{CompleteStep: []string{"never planned"}})
	wantErr(t, err, "not a pending step")
}

func TestResumeMarksTaskStaleWhenReferencedCodeChanges(t *testing.T) {
	app, dir := taskApp(t)
	ts, _ := app.Tasks.Create("p", "port", "implement", "claude", nil, []string{"func:a.go:parse_cat21", "func:a.go:decode_item"})
	mustUpdate(t, app, ts.TaskID, "claude", TaskPatch{State: TaskDiscovering})
	mustUpdate(t, app, ts.TaskID, "claude", TaskPatch{State: TaskAnalyzing, NextAction: &[]string{"inspect caller 3"}[0]})

	res, err := app.Tasks.Resume("p", ts.TaskID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	got := res["task"].(*TaskState)
	if got.State != TaskAnalyzing || got.NextAction != "inspect caller 3" || res["warning"] != nil {
		t.Fatalf("clean resume must keep state and next action: %+v", got)
	}
	for _, c := range res["checklist"].([]map[string]any) {
		if c["check"] == "all references valid" && c["ok"] != true {
			t.Fatalf("references should be valid: %v", c)
		}
	}

	// The code changes on disk before the next agent resumes.
	writeTree(t, dir, map[string]string{"a.go": strings.Replace(cat21, "return decode_item()", "return 0", 1)})
	res, _ = app.Tasks.Resume("p", ts.TaskID, "opencode")
	got = res["task"].(*TaskState)
	if got.State != TaskStale || res["warning"] == nil {
		t.Fatalf("stale reference must move the task to STALE: %+v", got)
	}
	stale := map[string]string{}
	for _, h := range res["reference_health"].([]ReferenceHealth) {
		stale[h.ID] = h.Status
	}
	if stale["func:a.go:parse_cat21"] != RefStale || stale["func:a.go:decode_item"] != RefValid {
		t.Fatalf("only parse_cat21 changed; decode_item merely shifted: %v", stale)
	}
	// The only ways out of STALE are re-discovery / re-analysis, not implementation.
	_, err = app.Tasks.Update("p", ts.TaskID, "opencode", TaskPatch{State: TaskImplementing})
	wantErr(t, err, "illegal task transition STALE -> IMPLEMENTING")
}

func TestAgentHandoffSurvivesPersistentReload(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"a.go": cat21})
	db := filepath.Join(t.TempDir(), "g.gob")
	open := func() (*Application, *graph.PersistentGraphRepository) {
		repo, err := graph.NewPersistentRepository(db)
		if err != nil {
			t.Fatal(err)
		}
		return NewApplication(repo), repo
	}

	// Agent A (e.g. Claude) reverse engineers, then its process exits.
	appA, repoA := open()
	appA.Index.IndexRepository("p", dir, true, nil, false)
	ts, err := appA.Tasks.Create("p", "port parse_cat21 and prove equivalence", "reverse_engineer", "claude", nil, []string{"func:a.go:parse_cat21"})
	if err != nil {
		t.Fatal(err)
	}
	ev := newEvidence(t, appA, "func:a.go:parse_cat21")
	mustUpdate(t, appA, ts.TaskID, "claude", TaskPatch{State: TaskDiscovering,
		AddPlan: []string{"recover structure", "implement rust"}, CompleteStep: nil,
		AddEvidence: []string{ev}, AddUnknowns: []string{"behavior of branch 5"},
		Hypotheses: []HypothesisOp{{Claim: "branch 5 handles malformed items", SubjectID: "func:a.go:parse_cat21", EvidenceFor: []string{ev}}}})
	repoA.Close()

	// Agent B (e.g. Codex) starts fresh from the file alone and continues.
	appB, repoB := open()
	res, err := appB.Tasks.Resume("p", ts.TaskID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	got := res["task"].(*TaskState)
	if got.Goal == "" || got.CreatedBy != "claude" || got.UpdatedBy != "codex" || len(got.Pending) != 2 ||
		len(got.Unknowns) != 1 || len(got.Hypotheses) != 1 || len(got.Evidence) != 1 {
		t.Fatalf("state must survive the process boundary: %+v", got)
	}
	mustUpdate(t, appB, ts.TaskID, "codex", TaskPatch{State: TaskAnalyzing, CompleteStep: []string{"recover structure"}})
	repoB.Close()

	// Agent C (e.g. OpenCode) sees both earlier agents in the provenance trail.
	appC, repoC := open()
	defer repoC.Close()
	final, _ := appC.Tasks.Get("p", ts.TaskID)
	who := map[string]bool{}
	for _, h := range final.History {
		who[h.By] = true
	}
	if !who["claude"] || !who["codex"] || final.UpdatedBy != "codex" || len(final.Completed) != 1 {
		t.Fatalf("handoff trail: %+v", final.History)
	}
	if list := appC.Tasks.List("p", "", 10); len(list) != 1 || list[0]["state"] != TaskAnalyzing {
		t.Fatalf("list: %v", list)
	}
}
