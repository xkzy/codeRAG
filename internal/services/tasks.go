package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"codergag/internal/graph"
	"codergag/internal/ids"
	"codergag/internal/models"
)

// Task states. The LLM's context window is never the authority on where a task
// stands; this state, stored in the graph, is.
const (
	TaskUnknown       = "UNKNOWN"
	TaskDiscovering   = "DISCOVERING"
	TaskAnalyzing     = "ANALYZING"
	TaskHypothesising = "HYPOTHESIS"
	TaskImplementing  = "IMPLEMENTING"
	TaskVerifying     = "VERIFYING"
	TaskValidated     = "VALIDATED"
	TaskBlocked       = "BLOCKED"
	TaskStale         = "STALE"
)

// Hypothesis states. Promotion is gated on evidence; nothing becomes CONFIRMED
// by assertion.
const (
	HypUnknown      = "UNKNOWN"
	HypSuspected    = "SUSPECTED"
	HypSupported    = "SUPPORTED"
	HypPartial      = "PARTIAL"
	HypConfirmed    = "CONFIRMED"
	HypContradicted = "CONTRADICTED"
)

var taskTransitions = map[string][]string{
	TaskUnknown:       {TaskDiscovering, TaskBlocked},
	TaskDiscovering:   {TaskAnalyzing, TaskHypothesising, TaskBlocked, TaskStale},
	TaskAnalyzing:     {TaskDiscovering, TaskHypothesising, TaskImplementing, TaskBlocked, TaskStale},
	TaskHypothesising: {TaskDiscovering, TaskAnalyzing, TaskImplementing, TaskBlocked, TaskStale},
	TaskImplementing:  {TaskAnalyzing, TaskVerifying, TaskBlocked, TaskStale},
	TaskVerifying:     {TaskImplementing, TaskAnalyzing, TaskValidated, TaskBlocked, TaskStale},
	TaskValidated:     {TaskStale},
	TaskBlocked:       {TaskDiscovering, TaskAnalyzing, TaskHypothesising, TaskImplementing, TaskVerifying},
	TaskStale:         {TaskDiscovering, TaskAnalyzing, TaskBlocked},
}

var hypTransitions = map[string][]string{
	HypUnknown:      {HypSuspected},
	HypSuspected:    {HypSupported, HypPartial, HypContradicted},
	HypSupported:    {HypConfirmed, HypPartial, HypContradicted, HypSuspected},
	HypPartial:      {HypSupported, HypConfirmed, HypContradicted, HypSuspected},
	HypConfirmed:    {HypContradicted},
	HypContradicted: {HypSuspected},
}

func allowed(table map[string][]string, from, to string) bool {
	for _, t := range table[from] {
		if t == to {
			return true
		}
	}
	return false
}

// Fact kinds. FACT is reserved for statements with checkable provenance.
const (
	KindFact        = "FACT"
	KindObservation = "OBSERVATION"
	KindInference   = "INFERENCE"
)

type TaskFact struct {
	Text   string `json:"text"`
	Kind   string `json:"kind"`
	Source string `json:"source"`
	At     string `json:"at"`
	By     string `json:"by,omitempty"`
}

type TaskHypothesis struct {
	ID              string   `json:"id"`
	Claim           string   `json:"claim"`
	SubjectID       string   `json:"subject_id,omitempty"`
	Status          string   `json:"status"`
	Confidence      float64  `json:"confidence"`
	EvidenceFor     []string `json:"evidence_for,omitempty"`
	EvidenceAgainst []string `json:"evidence_against,omitempty"`
}

type ToolResult struct {
	Tool    string `json:"tool"`
	Summary string `json:"summary"`
	At      string `json:"at"`
	By      string `json:"by,omitempty"`
}

type ValidationEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"` // passed | failed
	Detail string `json:"detail,omitempty"`
	At     string `json:"at"`
}

type Transition struct {
	From   string `json:"from"`
	To     string `json:"to"`
	At     string `json:"at"`
	By     string `json:"by,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// TaskState is the persistent working memory of one task. It is stored as a Task
// node in the graph, so any agent can read and continue it.
type TaskState struct {
	TaskID      string            `json:"task_id"`
	ProjectID   string            `json:"project_id"`
	Goal        string            `json:"goal"`
	Mode        string            `json:"mode,omitempty"`
	State       string            `json:"state"`
	CurrentStep string            `json:"current_step,omitempty"`
	Plan        []string          `json:"plan,omitempty"`
	Completed   []string          `json:"completed,omitempty"`
	Pending     []string          `json:"pending,omitempty"`
	KnownFacts  []TaskFact        `json:"known_facts,omitempty"`
	Unknowns    []string          `json:"unknowns,omitempty"`
	Constraints []string          `json:"constraints,omitempty"`
	References  []string          `json:"references,omitempty"` // stable IDs
	Hypotheses  []TaskHypothesis  `json:"hypotheses,omitempty"`
	Evidence    []string          `json:"evidence,omitempty"`
	ToolResults []ToolResult      `json:"tool_results,omitempty"`
	Failures    []string          `json:"failures,omitempty"`
	Validation  []ValidationEntry `json:"validation,omitempty"`
	NextAction  string            `json:"next_action,omitempty"`
	CreatedBy   string            `json:"created_by"`
	UpdatedBy   string            `json:"updated_by"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	Revision    string            `json:"revision,omitempty"` // git HEAD when created
	Version     int               `json:"version"`
	History     []Transition      `json:"history,omitempty"`
}

// TaskPatch is a set of changes applied atomically. ExpectedVersion makes
// concurrent agents fail loudly instead of overwriting each other.
type TaskPatch struct {
	ExpectedVersion *int              `json:"expected_version,omitempty"`
	State           string            `json:"state,omitempty"`
	StateReason     string            `json:"state_reason,omitempty"`
	CurrentStep     *string           `json:"current_step,omitempty"`
	AddPlan         []string          `json:"add_plan,omitempty"`
	CompleteStep    []string          `json:"complete_steps,omitempty"`
	AddFacts        []TaskFact        `json:"add_facts,omitempty"`
	AddUnknowns     []string          `json:"add_unknowns,omitempty"`
	ResolveUnknowns []string          `json:"resolve_unknowns,omitempty"`
	AddConstraints  []string          `json:"add_constraints,omitempty"`
	AddReferences   []string          `json:"add_references,omitempty"`
	AddEvidence     []string          `json:"add_evidence,omitempty"`
	AddToolResults  []ToolResult      `json:"add_tool_results,omitempty"`
	AddFailures     []string          `json:"add_failures,omitempty"`
	ClearFailures   bool              `json:"clear_failures,omitempty"`
	AddValidation   []ValidationEntry `json:"add_validation,omitempty"`
	Hypotheses      []HypothesisOp    `json:"hypotheses,omitempty"`
	NextAction      *string           `json:"next_action,omitempty"`
}

// HypothesisOp creates a hypothesis (no ID) or moves an existing one.
type HypothesisOp struct {
	ID              string   `json:"id,omitempty"`
	Claim           string   `json:"claim,omitempty"`
	SubjectID       string   `json:"subject_id,omitempty"`
	Status          string   `json:"status,omitempty"`
	Confidence      *float64 `json:"confidence,omitempty"`
	EvidenceFor     []string `json:"evidence_for,omitempty"`
	EvidenceAgainst []string `json:"evidence_against,omitempty"`
}

// TaskService owns task working memory.
type TaskService struct {
	graph    graph.GraphRepository
	refs     *ReferenceResolver
	evidence *EvidenceService
	now      func() time.Time
}

func NewTaskService(g graph.GraphRepository, refs *ReferenceResolver, ev *EvidenceService) *TaskService {
	return &TaskService{graph: g, refs: refs, evidence: ev, now: time.Now}
}

func (s *TaskService) stamp() string { return s.now().UTC().Format(time.RFC3339) }

func (s *TaskService) node(projectID, taskID string) (*models.Node, error) {
	n, _ := s.graph.FindNodes("Task", map[string]any{"project_id": projectID, "task_id": taskID})
	if len(n) == 0 {
		return nil, &ServiceError{Message: fmt.Sprintf("task %q not found in project %s", taskID, projectID)}
	}
	return n[0], nil
}

func (s *TaskService) save(ts *TaskState) error {
	blob, err := json.Marshal(ts)
	if err != nil {
		return err
	}
	_, err = s.graph.UpsertNode("Task", map[string]any{"project_id": ts.ProjectID, "task_id": ts.TaskID}, map[string]any{
		"stable_id": ids.Task + ":" + ts.TaskID, "goal": ts.Goal, "state": ts.State, "mode": ts.Mode,
		"created_by": ts.CreatedBy, "updated_by": ts.UpdatedBy, "version": ts.Version, "updated_at": ts.UpdatedAt,
		"state_json": string(blob),
	})
	return err
}

func (s *TaskService) load(projectID, taskID string) (*TaskState, error) {
	n, err := s.node(projectID, taskID)
	if err != nil {
		return nil, err
	}
	var ts TaskState
	if err := json.Unmarshal([]byte(strProp(n, "state_json")), &ts); err != nil {
		return nil, fmt.Errorf("task %s is corrupted: %w", taskID, err)
	}
	return &ts, nil
}

var validModes = map[string]bool{"": true, "explore": true, "implement": true, "reverse_engineer": true, "validate": true, "autonomous": true}

// Create starts a task. References must already resolve, so a task never begins
// life pointing at objects that do not exist.
func (s *TaskService) Create(projectID, goal, mode, agent string, constraints, refs []string) (*TaskState, error) {
	if strings.TrimSpace(goal) == "" {
		return nil, &ServiceError{Message: "goal is required"}
	}
	if !validModes[mode] {
		return nil, &ServiceError{Message: "mode must be one of explore, implement, reverse_engineer, validate, autonomous"}
	}
	if ps, _ := s.graph.FindNodes("Project", map[string]any{"id": projectID}); len(ps) == 0 {
		return nil, &ServiceError{Message: "project must be indexed first"}
	}
	if err := s.checkRefs(projectID, refs); err != nil {
		return nil, err
	}
	now := s.stamp()
	ts := &TaskState{
		TaskID: "t-" + strings.ReplaceAll(models.NewID(), "-", "")[:12], ProjectID: projectID, Goal: goal, Mode: mode,
		State: TaskUnknown, Constraints: constraints, References: uniqueStrings(refs, ""),
		CreatedBy: agent, UpdatedBy: agent, CreatedAt: now, UpdatedAt: now, Version: 1,
		History: []Transition{{From: "", To: TaskUnknown, At: now, By: agent, Reason: "created"}},
	}
	root := s.refs.projectRoot(projectID)
	ts.Revision, _ = git(root, "rev-parse", "HEAD")
	return ts, s.save(ts)
}

func (s *TaskService) checkRefs(projectID string, refs []string) error {
	for _, ref := range refs {
		if r := s.refs.Resolve(projectID, ref); r.Status == RefInvalid {
			msg := fmt.Sprintf("%s: %s", RefInvalid, ref)
			if len(r.Candidates) > 0 {
				msg += "; did you mean: " + strings.Join(r.Candidates, ", ")
			}
			return &ServiceError{Message: msg}
		}
	}
	return nil
}

func (s *TaskService) Get(projectID, taskID string) (*TaskState, error) {
	return s.load(projectID, taskID)
}

// List returns task summaries, newest first, optionally filtered by state.
func (s *TaskService) List(projectID, state string, limit int) []map[string]any {
	nodes, _ := s.graph.FindNodes("Task", map[string]any{"project_id": projectID})
	sort.Slice(nodes, func(i, j int) bool { return strProp(nodes[i], "updated_at") > strProp(nodes[j], "updated_at") })
	var out []map[string]any
	for _, n := range nodes {
		if state != "" && strProp(n, "state") != state {
			continue
		}
		out = append(out, map[string]any{
			"task_id": strProp(n, "task_id"), "goal": strProp(n, "goal"), "state": strProp(n, "state"),
			"mode": strProp(n, "mode"), "updated_by": strProp(n, "updated_by"), "updated_at": strProp(n, "updated_at"),
		})
	}
	return capRows(out, limit)
}

// provenanceOK decides whether a fact's source is checkable. FACT requires it.
func (s *TaskService) provenanceOK(projectID, source string) bool {
	switch {
	case strings.HasPrefix(source, "evidence:"):
		n, err := s.graph.GetNode(strings.TrimPrefix(source, "evidence:"))
		return err == nil && n != nil && n.Properties["project_id"] == projectID
	case strings.HasPrefix(source, "graph:"), strings.HasPrefix(source, "tool:"):
		return len(source) > len("tool:")
	case ids.Looks(source):
		return s.refs.Resolve(projectID, source).Status != RefInvalid
	}
	return false
}

func (s *TaskService) evidenceExists(projectID, id string) bool {
	n, err := s.graph.GetNode(id)
	return err == nil && n != nil && n.Kind == "Evidence" && n.Properties["project_id"] == projectID
}

// Update applies a patch atomically: it either fully applies or returns an error
// and changes nothing. Agent identity is recorded for handoff provenance.
func (s *TaskService) Update(projectID, taskID, agent string, p TaskPatch) (*TaskState, error) {
	ts, err := s.load(projectID, taskID)
	if err != nil {
		return nil, err
	}
	if p.ExpectedVersion != nil && *p.ExpectedVersion != ts.Version {
		return nil, &ServiceError{Message: fmt.Sprintf("CONFLICT: task is at version %d (updated by %s), you edited version %d; re-read with get_task_state",
			ts.Version, ts.UpdatedBy, *p.ExpectedVersion)}
	}
	now := s.stamp()
	fail := func(f string, a ...any) (*TaskState, error) { return nil, &ServiceError{Message: fmt.Sprintf(f, a...)} }

	if err := s.checkRefs(projectID, p.AddReferences); err != nil {
		return nil, err
	}
	ts.References = uniqueStrings(append(ts.References, p.AddReferences...), "")

	for _, id := range p.AddEvidence {
		if !s.evidenceExists(projectID, id) {
			return fail("evidence %q does not exist in project %s (create it with record_evidence first)", id, projectID)
		}
	}
	ts.Evidence = uniqueStrings(append(ts.Evidence, p.AddEvidence...), "")

	for _, f := range p.AddFacts {
		if strings.TrimSpace(f.Text) == "" {
			return fail("fact text is required")
		}
		if f.Kind == "" {
			f.Kind = KindInference
		}
		switch f.Kind {
		case KindFact:
			if !s.provenanceOK(projectID, f.Source) {
				return fail("FACT %q needs checkable provenance (evidence:<id>, graph:<edge>, tool:<name>, or a resolvable stable ID); record it as INFERENCE or a hypothesis instead", f.Text)
			}
		case KindObservation:
			if f.Source == "" {
				return fail("OBSERVATION %q needs a source", f.Text)
			}
		case KindInference:
		default:
			return fail("fact kind must be FACT, OBSERVATION or INFERENCE")
		}
		f.At, f.By = now, agent
		ts.KnownFacts = append(ts.KnownFacts, f)
	}

	ts.Plan = append(ts.Plan, p.AddPlan...)
	for _, step := range p.AddPlan {
		ts.Pending = append(ts.Pending, step)
	}
	for _, step := range p.CompleteStep {
		i := indexOf(ts.Pending, step)
		if i < 0 {
			return fail("cannot complete %q: it is not a pending step", step)
		}
		ts.Pending = append(ts.Pending[:i], ts.Pending[i+1:]...)
		ts.Completed = append(ts.Completed, step)
	}
	ts.Unknowns = uniqueStrings(append(ts.Unknowns, p.AddUnknowns...), "")
	for _, u := range p.ResolveUnknowns {
		i := indexOf(ts.Unknowns, u)
		if i < 0 {
			return fail("unknown %q is not open", u)
		}
		ts.Unknowns = append(ts.Unknowns[:i], ts.Unknowns[i+1:]...)
	}
	ts.Constraints = uniqueStrings(append(ts.Constraints, p.AddConstraints...), "")
	for _, r := range p.AddToolResults {
		r.At, r.By = now, agent
		ts.ToolResults = append(ts.ToolResults, r)
	}
	if p.ClearFailures {
		ts.Failures = nil
	}
	ts.Failures = append(ts.Failures, p.AddFailures...)
	for _, v := range p.AddValidation {
		if v.Status != "passed" && v.Status != "failed" {
			return fail("validation status must be passed or failed")
		}
		v.At = now
		ts.Validation = append(ts.Validation, v)
	}
	if p.CurrentStep != nil {
		ts.CurrentStep = *p.CurrentStep
	}
	if p.NextAction != nil {
		ts.NextAction = *p.NextAction
	}

	for _, op := range p.Hypotheses {
		if err := s.applyHypothesis(ts, op, agent); err != nil {
			return nil, err
		}
	}

	if p.State != "" && p.State != ts.State {
		if !allowed(taskTransitions, ts.State, p.State) {
			return fail("illegal task transition %s -> %s (allowed: %s)", ts.State, p.State, strings.Join(taskTransitions[ts.State], ", "))
		}
		if p.State == TaskValidated {
			if err := validationGate(ts); err != nil {
				return nil, err
			}
		}
		ts.History = append(ts.History, Transition{From: ts.State, To: p.State, At: now, By: agent, Reason: p.StateReason})
		ts.State = p.State
	}

	ts.UpdatedBy, ts.UpdatedAt = agent, now
	ts.Version++
	return ts, s.save(ts)
}

// validationGate: a task is VALIDATED only when the latest result of every
// validation that was run is "passed", and at least one was run.
func validationGate(ts *TaskState) error {
	if len(ts.Validation) == 0 {
		return &ServiceError{Message: "cannot mark VALIDATED: no validation has been recorded"}
	}
	latest := map[string]ValidationEntry{}
	for _, v := range ts.Validation {
		latest[v.Name] = v
	}
	var failing []string
	for name, v := range latest {
		if v.Status != "passed" {
			failing = append(failing, name)
		}
	}
	if len(failing) > 0 {
		sort.Strings(failing)
		return &ServiceError{Message: "cannot mark VALIDATED: latest result failed for " + strings.Join(failing, ", ")}
	}
	return nil
}

func (s *TaskService) applyHypothesis(ts *TaskState, op HypothesisOp, agent string) error {
	fail := func(f string, a ...any) error { return &ServiceError{Message: fmt.Sprintf(f, a...)} }
	for _, id := range append(append([]string{}, op.EvidenceFor...), op.EvidenceAgainst...) {
		if !s.evidenceExists(ts.ProjectID, id) {
			return fail("hypothesis evidence %q does not exist in project %s", id, ts.ProjectID)
		}
	}
	var h *TaskHypothesis
	if op.ID == "" {
		if strings.TrimSpace(op.Claim) == "" {
			return fail("a new hypothesis needs a claim")
		}
		conf := 0.5
		if op.Confidence != nil {
			conf = *op.Confidence
		}
		if conf < 0 || conf > 1 {
			return fail("confidence must be between 0 and 1")
		}
		id := fmt.Sprintf("hyp-%d", len(ts.Hypotheses)+1)
		if op.SubjectID != "" {
			if r := s.refs.Resolve(ts.ProjectID, op.SubjectID); r.Status == RefInvalid && ids.Looks(op.SubjectID) {
				return fail("%s: %s", RefInvalid, op.SubjectID)
			}
			subject := op.SubjectID
			if n, _ := s.refs.Lookup(ts.ProjectID, op.SubjectID); n != nil {
				subject = n.ID
			}
			if node, err := s.evidence.RecordHypothesis(ts.ProjectID, subject, op.Claim, conf,
				map[string]any{"state": "HYPOTHESIS", "analyst": agent}); err == nil {
				id, _ = node["id"].(string)
			}
		}
		ts.Hypotheses = append(ts.Hypotheses, TaskHypothesis{ID: id, Claim: op.Claim, SubjectID: op.SubjectID, Status: HypSuspected, Confidence: conf})
		h = &ts.Hypotheses[len(ts.Hypotheses)-1]
	} else {
		for i := range ts.Hypotheses {
			if ts.Hypotheses[i].ID == op.ID {
				h = &ts.Hypotheses[i]
			}
		}
		if h == nil {
			return fail("hypothesis %q not found in task %s", op.ID, ts.TaskID)
		}
	}
	h.EvidenceFor = uniqueStrings(append(h.EvidenceFor, op.EvidenceFor...), "")
	h.EvidenceAgainst = uniqueStrings(append(h.EvidenceAgainst, op.EvidenceAgainst...), "")
	if op.Confidence != nil {
		if *op.Confidence < 0 || *op.Confidence > 1 {
			return fail("confidence must be between 0 and 1")
		}
		h.Confidence = *op.Confidence
	}
	if op.Status == "" || op.Status == h.Status {
		return nil
	}
	if !allowed(hypTransitions, h.Status, op.Status) {
		return fail("illegal hypothesis transition %s -> %s for %s (allowed: %s)", h.Status, op.Status, h.ID, strings.Join(hypTransitions[h.Status], ", "))
	}
	switch op.Status {
	case HypSupported, HypPartial:
		if len(h.EvidenceFor) == 0 {
			return fail("%s needs at least one supporting evidence id before it can be %s", h.ID, op.Status)
		}
	case HypConfirmed:
		if len(h.EvidenceFor) < 2 {
			return fail("%s needs at least two independent supporting evidence ids to be CONFIRMED (has %d)", h.ID, len(h.EvidenceFor))
		}
		if len(h.EvidenceAgainst) > 0 {
			return fail("%s has contradicting evidence; resolve it before confirming", h.ID)
		}
	case HypContradicted:
		if len(h.EvidenceAgainst) == 0 {
			return fail("%s needs contradicting evidence before it can be CONTRADICTED", h.ID)
		}
	}
	h.Status = op.Status
	return nil
}

// ReferenceHealth is the current status of one reference held by a task.
type ReferenceHealth struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

// Resume rebuilds everything a new agent needs from durable state alone: the
// task, the live status of every reference it holds, and a runtime-generated
// checklist. If any reference has gone stale or vanished the task is moved to
// STALE, so nothing proceeds on outdated ground.
func (s *TaskService) Resume(projectID, taskID, agent string) (map[string]any, error) {
	ts, err := s.load(projectID, taskID)
	if err != nil {
		return nil, err
	}
	var health []ReferenceHealth
	bad := 0
	for _, ref := range ts.References {
		r := s.refs.Resolve(projectID, ref)
		health = append(health, ReferenceHealth{ID: ref, Status: r.Status, Reason: r.Reason})
		if r.Status != RefValid {
			bad++
		}
	}
	root := s.refs.projectRoot(projectID)
	head, _ := git(root, "rev-parse", "HEAD")
	moved := ts.Revision != "" && head != "" && !sameRevision(ts.Revision, head)

	if bad > 0 && ts.State != TaskStale && ts.State != TaskValidated && allowed(taskTransitions, ts.State, TaskStale) {
		reason := fmt.Sprintf("%d reference(s) no longer valid on resume", bad)
		if updated, err := s.Update(projectID, taskID, agent, TaskPatch{State: TaskStale, StateReason: reason}); err == nil {
			ts = updated
		}
	} else if agent != "" && agent != ts.UpdatedBy {
		// Record the handoff without changing state.
		ts.History = append(ts.History, Transition{From: ts.State, To: ts.State, At: s.stamp(), By: agent, Reason: "resumed by " + agent})
		ts.UpdatedBy, ts.Version = agent, ts.Version+1
		s.save(ts)
	}

	checklist := []map[string]any{
		{"check": "task state loaded", "ok": true, "detail": ts.State},
		{"check": "all references valid", "ok": bad == 0, "detail": fmt.Sprintf("%d/%d valid", len(health)-bad, len(health))},
		{"check": "repository revision unchanged since task began", "ok": !moved, "detail": shortSHA(ts.Revision) + " -> " + shortSHA(head)},
		{"check": "no unresolved failures", "ok": len(ts.Failures) == 0, "detail": fmt.Sprintf("%d recorded", len(ts.Failures))},
		{"check": "evidence recorded for hypotheses", "ok": hypothesesHaveEvidence(ts), "detail": fmt.Sprintf("%d hypotheses", len(ts.Hypotheses))},
	}
	out := map[string]any{"task": ts, "reference_health": health, "checklist": checklist, "revision_moved": moved}
	if len(ts.History) > 0 {
		out["last_handoffs"] = capRows(reverseTransitions(ts.History), 5)
	}
	if bad > 0 {
		out["warning"] = "Some references are stale or missing: re-resolve them with resolve_symbol / verify_reference before modifying anything."
	}
	return out, nil
}

func hypothesesHaveEvidence(ts *TaskState) bool {
	for _, h := range ts.Hypotheses {
		if (h.Status == HypSupported || h.Status == HypConfirmed) && len(h.EvidenceFor) == 0 {
			return false
		}
	}
	return true
}

func reverseTransitions(in []Transition) []Transition {
	out := make([]Transition, len(in))
	for i, t := range in {
		out[len(in)-1-i] = t
	}
	return out
}
