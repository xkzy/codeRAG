package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func indexSample(t *testing.T) (*Application, string) {
	t.Helper()
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString("package x\n\ntype Base struct{}\ntype Derived struct{ Base }\n\n")
	words := []string{"parse packet", "encode frame", "decode header", "read stream", "write buffer", "flush queue", "open socket", "close socket"}
	for i, w := range words {
		parts := strings.Fields(w)
		name := strings.Title(parts[0]) + strings.Title(parts[1])
		next := strings.Title(strings.Fields(words[(i+1)%len(words)])[0]) + strings.Title(strings.Fields(words[(i+1)%len(words)])[1])
		fmt.Fprintf(&src, "func %s() { %s(); external() }\n", name, next)
	}
	if err := os.WriteFile(filepath.Join(dir, "s.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil); err != nil {
		t.Fatal(err)
	}
	return app, dir
}

func metric(rep *EvalReport, name string) Metric {
	for _, m := range rep.Metrics {
		if m.Name == name {
			return m
		}
	}
	return Metric{}
}

func TestEvalScoresAHealthyIndexHighAndExplainsMetrics(t *testing.T) {
	app, _ := indexSample(t)
	rep, err := app.Eval("p")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Grade != "good" || rep.Overall < 90 {
		t.Fatalf("healthy index should score good, got %d %s: %+v", rep.Overall, rep.Grade, rep.Metrics)
	}
	for _, name := range []string{"retrieval", "call_resolution", "inheritance", "freshness"} {
		if m := metric(rep, name); m.NA || m.Score < 0.9 || m.Detail == "" {
			t.Fatalf("%s: %+v", name, m)
		}
	}
	if !metric(rep, "memory_retention").NA {
		t.Fatal("memory retention is n/a until something has been compacted")
	}
	if _, err := app.Eval("missing"); err == nil {
		t.Fatal("unindexed project must error")
	}
}

func TestEvalDetectsStaleIndexAndBrokenEdges(t *testing.T) {
	app, dir := indexSample(t)
	// The file changes on disk after indexing.
	if err := os.WriteFile(filepath.Join(dir, "s.go"), []byte("package x\nfunc Other() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, _ := app.Eval("p")
	if m := metric(rep, "freshness"); m.Score != 0 {
		t.Fatalf("changed file must score 0 freshness: %+v", m)
	}
	found := false
	for _, a := range rep.Advice {
		if strings.Contains(a, "index_repository") {
			found = true
		}
	}
	if !found || rep.Overall >= 90 {
		t.Fatalf("stale index should lower the grade and advise re-indexing: %d %v", rep.Overall, rep.Advice)
	}

	// Sabotage: delete all CALLS edges; call resolution must drop.
	app2, _ := indexSample(t)
	fns, _ := app2.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
	for _, f := range fns {
		nbrs, _ := app2.Graph.Neighbors(f.ID, "CALLS", "out")
		var ids []string
		for _, en := range nbrs {
			ids = append(ids, en.Edge.ID)
		}
		app2.Graph.RemoveEdges(ids)
	}
	rep2, _ := app2.Eval("p")
	if m := metric(rep2, "call_resolution"); m.Score != 0 {
		t.Fatalf("missing edges must score 0: %+v", m)
	}
}

func TestEvalMemoryRetentionAfterCompaction(t *testing.T) {
	app, _ := indexSample(t)
	off := map[string]any{"auto_compact": false}
	app.Memory.Store("p", "m1", "ParsePacket reads a header.\nIt rejects zero.", off)
	app.Memory.Store("p", "m2", "ParsePacket reads a header.\nIt is called often.", off)
	app.Memory.Compact("p", 2)
	rep, _ := app.Eval("p")
	if m := metric(rep, "memory_retention"); m.NA || m.Score != 1 {
		t.Fatalf("lossless compaction must retain 100%%: %+v", m)
	}
	// Corrupt a summary: retention must fall below 100%.
	sums, _ := app.Graph.FindNodes("Memory", map[string]any{"project_id": "p", "kind": compactedKind})
	sums[0].Properties["content"] = "## truncated"
	rep, _ = app.Eval("p")
	if m := metric(rep, "memory_retention"); m.Score >= 1 {
		t.Fatalf("lost lines must be detected: %+v", m)
	}
}

func TestUsageRecordingRollupAndStatus(t *testing.T) {
	app, _ := indexSample(t)
	rec := NewUsageRecorder(app.Graph)
	rec.Record("find_function", 4*time.Millisecond, nil, 1000, 300, false)
	rec.Record("find_function", 6*time.Millisecond, nil, 1000, 300, true)
	rec.Record("find_function", 5*time.Millisecond, fmt.Errorf("boom"), 0, 0, false)
	if err := rec.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := rec.Flush(); err != nil { // idempotent: same session node
		t.Fatal(err)
	}
	sum, _ := SummarizeUsage(app.Graph)
	u := sum.ByTool["find_function"]
	if sum.Sessions != 1 || u.Calls != 3 || u.Errors != 1 || sum.TokensSaved() != 1400 || u.Truncated != 1 {
		t.Fatalf("unexpected usage summary: %+v %+v", sum, u)
	}

	// Many sessions roll up into a total without losing counts.
	for i := 0; i < 30; i++ {
		r := NewUsageRecorder(app.Graph)
		r.sessionID = fmt.Sprintf("s%02d", i)
		r.started = r.started.Add(time.Duration(i) * time.Second)
		r.Record("get_function", time.Millisecond, nil, 10, 5, false)
		r.Flush()
	}
	rolled, err := rollUpUsage(app.Graph, keepUsageSessions)
	if err != nil || rolled != 11 {
		t.Fatalf("rolled=%d err=%v", rolled, err)
	}
	sum, _ = SummarizeUsage(app.Graph)
	if sum.ByTool["get_function"].Calls != 30 || sum.ByTool["find_function"].Calls != 3 || sum.Sessions != keepUsageSessions {
		t.Fatalf("rollup lost data: sessions=%d %+v", sum.Sessions, sum.ByTool)
	}

	st, err := app.Status()
	if err != nil {
		t.Fatal(err)
	}
	usage := st["usage"].(map[string]any)
	if usage["tool_calls"] != 33 || usage["token_savings_pct"].(float64) < 50 {
		t.Fatalf("status usage: %+v", usage)
	}
	projects := st["projects"].([]map[string]any)
	if len(projects) != 1 || projects[0]["functions"] != 8 {
		t.Fatalf("status projects: %+v", projects)
	}
}

func TestMaintenanceCompactsPurgesAndRecords(t *testing.T) {
	app, _ := indexSample(t)
	off := map[string]any{"auto_compact": false}
	app.Memory.Store("p", "m1", "ParsePacket reads a header.", off)
	app.Memory.Store("p", "m2", "ParsePacket also validates.", off)
	app.Graph.UpsertNode("CacheEntry", map[string]any{"project_id": "p", "cache_key": "old"}, map[string]any{"freshness": "STALE"})
	app.Graph.UpsertNode("CacheEntry", map[string]any{"project_id": "p", "cache_key": "ok"}, map[string]any{"freshness": "VALID"})

	rep, err := app.RunMaintenance()
	if err != nil {
		t.Fatal(err)
	}
	if rep["memories_archived"] != 2 || rep["cache_entries_purged"] != 1 {
		t.Fatalf("unexpected maintenance report: %v", rep)
	}
	left, _ := app.Graph.FindNodes("CacheEntry", nil)
	if len(left) != 1 || left[0].Properties["cache_key"] != "ok" {
		t.Fatalf("only the stale cache entry may be purged: %d", len(left))
	}
	st, _ := app.Status()
	if st["last_maintenance"] == nil {
		t.Fatal("status should show the last maintenance run")
	}
	// Second run is a no-op.
	rep2, _ := app.RunMaintenance()
	if rep2["memories_archived"] != 0 || rep2["cache_entries_purged"] != 0 {
		t.Fatalf("maintenance must be idempotent: %v", rep2)
	}
}
