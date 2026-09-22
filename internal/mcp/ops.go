package mcp

import (
	"bytes"
	"codergag/internal/services"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

func (r *ToolRegistry) flushUsageLocked() {
	if err := r.usage.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "usage flush:", err)
	}
	r.lastFlush = time.Now()
}

// FlushUsage persists this process's usage counters (call before exit).
func (r *ToolRegistry) FlushUsage() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flushUsageLocked()
	if sg, ok := r.app.Graph.(syncedGraph); ok {
		sg.Save()
	}
}

func (r *ToolRegistry) handleServerStatus(args map[string]any) (map[string]any, error) {
	r.flushUsageLocked() // include this process's live counters
	return r.app.Status()
}

func (r *ToolRegistry) handleRunMaintenance(args map[string]any) (map[string]any, error) {
	return r.app.RunMaintenance()
}

func (r *ToolRegistry) handleRunEval(args map[string]any) (map[string]any, error) {
	rep, err := r.app.Eval(getString(args, "project_id"))
	if err != nil {
		return nil, err
	}
	metrics := make([]map[string]any, 0, len(rep.Metrics))
	for _, m := range rep.Metrics {
		row := map[string]any{"metric": m.Name, "detail": m.Detail}
		if m.NA {
			row["score"] = "n/a"
		} else {
			row["score"] = float64(int(m.Score*100+0.5)) / 100
		}
		metrics = append(metrics, row)
	}
	out := map[string]any{"overall": rep.Overall, "grade": rep.Grade, "metrics": metrics}
	if len(rep.Advice) > 0 {
		out["advice"] = rep.Advice
	}
	return out, nil
}

func (r *ToolRegistry) handleRunBenchmark(args map[string]any) (map[string]any, error) {
	cfg := services.BenchmarkConfig{
		NumQueries:      getInt(args, "num_queries", 10),
		MaxTokens:       getInt(args, "max_tokens", 2000),
		IncludeBaseline: true,
	}
	if v, ok := args["include_baseline"].(bool); ok {
		cfg.IncludeBaseline = v
	}
	res, err := r.app.RunBenchmark(getString(args, "project_id"), cfg)
	if err != nil {
		return nil, err
	}
	return structToMap(res)
}

// StartMaintenance runs the optimization pass every interval until the returned
// stop func is called. It shares the registry lock with tool calls, so it never
// interleaves with a running tool. interval <= 0 disables scheduling.
func (r *ToolRegistry) StartMaintenance(interval time.Duration) (stop func()) {
	if interval <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				r.RunMaintenanceNow()
			}
		}
	}()
	return func() { close(done) }
}

// RunMaintenanceNow performs one scheduled pass under the registry lock.
func (r *ToolRegistry) RunMaintenanceNow() {
	r.mu.Lock()
	defer r.mu.Unlock()
	sg, shared := r.app.Graph.(syncedGraph)
	if shared {
		sg.Refresh()
	}
	if _, err := r.app.RunMaintenance(); err != nil {
		fmt.Fprintln(os.Stderr, "maintenance:", err)
	}
	r.flushUsageLocked()
	if shared {
		if err := sg.Save(); err != nil {
			fmt.Fprintln(os.Stderr, "maintenance save:", err)
		}
	}
}

func (r *ToolRegistry) resolverFor(projectID string) *services.ReferenceResolver {
	return r.app.ResolverFor(projectID)
}

func (r *ToolRegistry) handleResolveReference(args map[string]any) (map[string]any, error) {
	return structToMap(r.resolverFor(getString(args, "project_id")).Resolve(getString(args, "project_id"), getString(args, "id")))
}

func (r *ToolRegistry) handleVerifyReference(args map[string]any) (map[string]any, error) {
	projectID := getString(args, "project_id")
	return structToMap(r.resolverFor(projectID).Verify(projectID, services.VerifyRequest{
		ID: getString(args, "id"), ExpectedContentHash: getString(args, "expected_content_hash"),
		ExpectedRevision: getString(args, "expected_revision"),
	}))
}

func (r *ToolRegistry) handleResolveSourceSpan(args map[string]any) (map[string]any, error) {
	projectID := getString(args, "project_id")
	span, symbol, err := r.resolverFor(projectID).ResolveSourceSpan(projectID, getString(args, "file"),
		getInt(args, "start_line", 1), getInt(args, "end_line", 0))
	if err != nil {
		return nil, err
	}
	out, err := structToMap(span)
	if err != nil {
		return nil, err
	}
	out["status"] = services.RefValid
	if symbol == "" {
		out["note"] = "no indexed symbol contains this range (or the index is stale)"
	}
	return out, nil
}

func (r *ToolRegistry) handleResolveSymbol(args map[string]any) (map[string]any, error) {
	projectID := getString(args, "project_id")
	matches, sug := r.resolverFor(projectID).ResolveSymbol(projectID, getString(args, "name"))
	if len(matches) == 0 {
		return map[string]any{"status": services.RefInvalid, "name": getString(args, "name"), "did_you_mean": sug}, nil
	}
	rows := make([]map[string]any, len(matches))
	for i, m := range matches {
		rows[i] = map[string]any{"id": m.ID, "kind": m.Kind, "name": m.Name, "path": m.Path, "line": m.Line}
	}
	return map[string]any{"status": "FOUND", "matches": rows, "ambiguous": len(matches) > 1}, nil
}

func (r *ToolRegistry) handleResolveSlice(args map[string]any) (map[string]any, error) {
	req := services.SliceRequest{
		Base: getString(args, "base"), Offset: getInt(args, "offset", 0), ElementSize: getInt(args, "element_size", 1),
		ElementType: getString(args, "element_type"), SourceReference: getString(args, "source_reference"),
		Expression: getString(args, "source_expression"),
	}
	opt := func(key string) *int {
		if _, ok := args[key]; !ok {
			return nil
		}
		v := getInt(args, key, 0)
		return &v
	}
	req.Length, req.End, req.BaseLength = opt("length"), opt("end"), opt("base_length")
	return structToMap(services.ComputeSlice(req))
}

// structToMap converts a typed result to the generic map tool results use.
func structToMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

func (r *ToolRegistry) handleCreateTask(args map[string]any) (map[string]any, error) {
	ts, err := r.app.Tasks.Create(getString(args, "project_id"), getString(args, "goal"), getString(args, "mode"),
		getString(args, "agent"), getStringSlice(args, "constraints"), getStringSlice(args, "references"))
	if err != nil {
		return nil, err
	}
	return structToMap(ts)
}

func (r *ToolRegistry) handleGetTaskState(args map[string]any) (map[string]any, error) {
	ts, err := r.app.Tasks.Get(getString(args, "project_id"), getString(args, "task_id"))
	if err != nil {
		return nil, err
	}
	return structToMap(ts)
}

func (r *ToolRegistry) handleUpdateTaskState(args map[string]any) (map[string]any, error) {
	raw, _ := args["patch"].(map[string]any)
	blob, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var patch services.TaskPatch
	dec := json.NewDecoder(bytes.NewReader(blob))
	dec.DisallowUnknownFields() // a misspelled field must not be silently ignored
	if err := dec.Decode(&patch); err != nil {
		return nil, fmt.Errorf("invalid patch: %w", err)
	}
	ts, err := r.app.Tasks.Update(getString(args, "project_id"), getString(args, "task_id"), getString(args, "agent"), patch)
	if err != nil {
		return nil, err
	}
	return structToMap(ts)
}

func (r *ToolRegistry) handleResumeTask(args map[string]any) (map[string]any, error) {
	res, err := r.app.Tasks.Resume(getString(args, "project_id"), getString(args, "task_id"), getString(args, "agent"))
	if err != nil {
		return nil, err
	}
	return structToMap(res)
}

func (r *ToolRegistry) handleListTasks(args map[string]any) (map[string]any, error) {
	return rows("tasks", r.app.Tasks.List(getString(args, "project_id"), getString(args, "state"), limitOf(args))), nil
}
