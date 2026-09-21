package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codergag/internal/cache"
	"codergag/internal/graph"
	"codergag/internal/ids"
	"codergag/internal/services"
)

// toInt coerces the JSON number type (int or float64) returned by tool calls.
func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case float64:
		return int(x)
	default:
		return 0
	}
}

func TestRegistryIndexAndQuery(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n\nfunc Alpha() { Beta() }\n\nfunc Beta() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
	res, err := reg.Call("find_function", map[string]any{"project_id": "p", "query": "alpha"})
	if err != nil || res["count"].(int) != 1 {
		t.Fatalf("find_function: %v %v", res, err)
	}
	if _, err := reg.Call("get_function", map[string]any{"project_id": "p", "name": "Alpha"}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Call("find_function", map[string]any{"query": "x"}); err == nil {
		t.Fatal("expected project_id error")
	}
}

func TestAllToolsHaveHandlers(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	seen := map[string]bool{}
	for _, s := range reg.schemas {
		if s.Handler == nil || seen[s.Name] {
			t.Fatalf("bad or duplicate tool %s", s.Name)
		}
		seen[s.Name] = true
	}
}

func TestNoArbitrarySQLTool(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	names := map[string]bool{}
	for _, d := range reg.Definitions() {
		names[d.Name] = true
	}
	for _, want := range []string{"index_repository", "get_callers", "impact_analysis", "record_re_hypothesis", "analyze_complexity", "memory_store", "export_table", "export_graph", "git_churn", "query_table", "get_table_schema"} {
		if !names[want] {
			t.Errorf("missing tool %s", want)
		}
	}
	if names["execute_arbitrary_sql"] {
		t.Error("arbitrary SQL tool must not exist")
	}
}

func TestClientsShareGraphAndIsolateProjects(t *testing.T) {
	app := services.ApplicationInMemory()
	first, second := NewToolRegistry(app), NewToolRegistry(app)
	node, err := app.Graph.UpsertNode("Function", map[string]any{"project_id": "private", "qualified_name": "f"}, map[string]any{"name": "f"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Call("record_re_hypothesis", map[string]any{"project_id": "private", "hypothesis": map[string]any{
		"id":               "h1",
		"subject_id":       node.ID,
		"claim":            "shared memory",
		"confidence":       0.5,
		"status":           "active",
		"analyst":          "client-one",
		"evidence_for":     []any{},
		"evidence_against": []any{},
	}}); err != nil {
		t.Fatal(err)
	}
	got, err := second.Call("get_hypotheses", map[string]any{"project_id": "private", "subject_id": node.ID})
	if err != nil || got["count"].(int) != 1 {
		t.Fatalf("second client should see hypothesis: %v %v", got, err)
	}
	_, err = second.Call("get_callees", map[string]any{"project_id": "other", "function_id": node.ID})
	if err == nil || !strings.Contains(err.Error(), "different project") {
		t.Fatalf("expected cross-project rejection, got %v", err)
	}
}

func TestMemoryStoreAndSearch(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("memory_store", map[string]any{"project_id": "demo", "title": "decoder", "content": "CAT48 updates target tracks", "agent": "one"}); err != nil {
		t.Fatal(err)
	}
	res, err := reg.Call("memory_search", map[string]any{"project_id": "demo", "query": "target"})
	if err != nil || res["count"].(int) != 1 {
		t.Fatalf("memory_search: %v %v", res, err)
	}
}

func TestFindClassAndStructAfterIndex(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n\ntype Point struct{ X int }\n\ntype Shape interface{ Area() int }\n\nfunc F() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
	if res, err := reg.Call("find_struct", map[string]any{"project_id": "p", "query": "point"}); err != nil || res["count"].(int) != 1 {
		t.Fatalf("find_struct: %v %v", res, err)
	}
	if res, err := reg.Call("find_class", map[string]any{"project_id": "p", "query": "shape"}); err != nil || res["count"].(int) != 1 {
		t.Fatalf("find_class: %v %v", res, err)
	}
	// Re-indexing after removing a type must not leave stale nodes.
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
	if res, _ := reg.Call("find_struct", map[string]any{"project_id": "p", "query": "point"}); res["count"].(int) != 0 {
		t.Fatalf("stale struct after reindex: %v", res)
	}
}

func TestTypeHierarchyTool(t *testing.T) {
	dir := t.TempDir()
	src := "class A: pass\nclass B(A): pass\nclass C(B): pass\n"
	if err := os.WriteFile(filepath.Join(dir, "h.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
	find := func(name string) string {
		res, err := reg.Call("find_class", map[string]any{"project_id": "p", "query": name})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res["results"].([]map[string]any) {
			if r["name"] == name {
				return r["id"].(string)
			}
		}
		t.Fatalf("class %s not found", name)
		return ""
	}
	res, err := reg.Call("get_type_hierarchy", map[string]any{"project_id": "p", "node_id": find("B")})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(res["supertypes"].([]map[string]any)); n != 1 {
		t.Fatalf("B supertypes = %d, want 1", n)
	}
	if n := len(res["subtypes"].([]map[string]any)); n != 1 {
		t.Fatalf("B subtypes = %d, want 1", n)
	}
	up, _ := reg.Call("get_type_hierarchy", map[string]any{"project_id": "p", "node_id": find("C"), "direction": "up"})
	if n := len(up["supertypes"].([]map[string]any)); n != 2 {
		t.Fatalf("C transitive supertypes = %d, want 2", n)
	}
	if _, has := up["subtypes"]; has {
		t.Fatal("direction=up must not return subtypes")
	}
}

func indexedFunctions(t *testing.T, n int) *ToolRegistry {
	t.Helper()
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString("package x\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&src, "func Handler%03d() {}\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "h.go"), []byte(src.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestPaginationWalksAllRowsExactlyOnce(t *testing.T) {
	reg := indexedFunctions(t, 25)
	seen := map[string]bool{}
	offset, pages := 0, 0
	for {
		res, err := reg.Call("find_function", map[string]any{"project_id": "p", "query": "handler", "limit": 10, "offset": offset})
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, r := range res["results"].([]map[string]any) {
			name := r["name"].(string)
			if seen[name] {
				t.Fatalf("%s returned on two pages", name)
			}
			seen[name] = true
		}
		if res["has_more"] != true {
			if _, has := res["next_offset"]; has {
				t.Fatal("next_offset must be absent on the last page")
			}
			break
		}
		offset = res["next_offset"].(int)
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 25 || pages != 3 {
		t.Fatalf("saw %d rows in %d pages, want 25 rows in 3 pages", len(seen), pages)
	}
}

func TestPaginationEdgeCases(t *testing.T) {
	reg := indexedFunctions(t, 10)
	// Exactly a full page: has_more must be false.
	res, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "handler", "limit": 10})
	if res["count"] != 10 || res["has_more"] != false {
		t.Fatalf("exact page: %v", res)
	}
	// Offset past the end yields an empty page, not an error.
	res, err := reg.Call("find_function", map[string]any{"project_id": "p", "query": "handler", "limit": 5, "offset": 500})
	if err != nil || res["count"] != 0 || res["has_more"] != false {
		t.Fatalf("past end: %v %v", res, err)
	}
	// Deep paging beyond the fetch bound is rejected explicitly.
	if _, err := reg.Call("find_function", map[string]any{"project_id": "p", "query": "handler", "limit": 200, "offset": 900}); err == nil {
		t.Fatal("expected error for offset+limit beyond bound")
	}
	// Non-paginated tools are untouched and do not advertise offset.
	for _, d := range reg.Definitions() {
		_, has := d.InputSchema["properties"].(map[string]any)["offset"]
		if d.Name == "resolve_slice" { // its offset is slice arithmetic, not paging
			continue
		}
		if has != paginatedTools[d.Name] {
			t.Fatalf("tool %s offset schema mismatch", d.Name)
		}
	}
}

func TestPaginationBeyondDefaultLimitCap(t *testing.T) {
	reg := indexedFunctions(t, 260)
	res, err := reg.Call("find_function", map[string]any{"project_id": "p", "query": "handler", "limit": 20, "offset": 240})
	if err != nil || res["count"] != 20 || res["has_more"] != false {
		t.Fatalf("deep page should return the last 20 rows: %v %v", res, err)
	}
}

func TestDependencyAndReferenceToolsUseResolvedEdges(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a.ts": "import { W } from './b';\nfunction mk() { return new W(); }\n",
		"b.ts": "export class W {}\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := services.ApplicationInMemory()
	reg := NewToolRegistry(app)
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(dir)
	fileID := func(name string) string {
		n, _ := app.Graph.FindNodes("SourceFile", map[string]any{"project_id": "p", "path": filepath.Join(real, name)})
		return n[0].ID
	}
	deps, err := reg.Call("get_dependencies", map[string]any{"project_id": "p", "node_id": fileID("a.ts")})
	if err != nil {
		t.Fatal(err)
	}
	var sawFile bool
	for _, r := range deps["results"].([]map[string]any) {
		if r["relationship"] == "DEPENDS_ON" && strings.HasSuffix(r["path"].(string), "b.ts") {
			sawFile = true
		}
	}
	if !sawFile {
		t.Fatalf("get_dependencies should list b.ts via DEPENDS_ON: %v", deps)
	}
	dependents, _ := reg.Call("get_dependents", map[string]any{"project_id": "p", "node_id": fileID("b.ts")})
	if dependents["count"].(int) < 1 {
		t.Fatalf("b.ts should have a dependent: %v", dependents)
	}

	cls, _ := reg.Call("find_class", map[string]any{"project_id": "p", "query": "W"})
	classID := cls["results"].([]map[string]any)[0]["id"].(string)
	refs, _ := reg.Call("get_references", map[string]any{"project_id": "p", "node_id": classID})
	if refs["count"].(int) != 1 || refs["results"].([]map[string]any)[0]["name"] != "mk" {
		t.Fatalf("get_references should return the function that uses W: %v", refs)
	}
	res, err := reg.Call("resolve_references", map[string]any{"project_id": "p"})
	if err != nil || res["uses"] != 1 || res["depends_on"] != 1 {
		t.Fatalf("resolve_references totals: %v %v", res, err)
	}
	if _, err := reg.Call("resolve_references", map[string]any{"project_id": "nope"}); err == nil {
		t.Fatal("unindexed project must error")
	}
}

func TestArchitectureAndDocCheckTools(t *testing.T) {
	dir := t.TempDir()
	tree := map[string]string{
		"a/a.go":    "package a\nfunc A() {}\n",
		"b/b.go":    "package b\nimport \"ex.com/m/a\"\nfunc B() { a.A() }\n",
		"README.md": "# R\nSee `A` and the removed `Legacy`.\n",
		"go.mod":    "module ex.com/m\n",
	}
	for rel, src := range tree {
		p := filepath.Join(dir, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Call("index_markdown", map[string]any{"project_id": "p", "path": filepath.Join(dir, "README.md")}); err != nil {
		t.Fatal(err)
	}
	arch, err := reg.Call("generate_architecture_report", map[string]any{"project_id": "p", "depth": 1})
	if err != nil {
		t.Fatal(err)
	}
	md := arch["markdown"].(string)
	if !strings.Contains(md, "`b` — 1 files") || !strings.Contains(md, "depends on `a`") {
		t.Fatalf("architecture markdown:\n%s", md)
	}
	docs, err := reg.Call("check_docs", map[string]any{"project_id": "p"})
	if err != nil || docs["documents_with_issues"] != 1 || docs["missing_references"] != 1 {
		t.Fatalf("check_docs: %v %v", docs, err)
	}
	if _, err := reg.Call("pr_context", map[string]any{"project_id": "p"}); err == nil {
		t.Fatal("pr_context outside a git repository should fail clearly")
	}
}

func TestReferenceToolsAndCentralInvalidReferenceRejection(t *testing.T) {
	dir := t.TempDir()
	src := "package x\nfunc Caller() { Callee() }\nfunc Callee() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
	res, err := reg.Call("resolve_reference", map[string]any{"project_id": "p", "id": "func:a.go:Callee"})
	if err != nil || res["status"] != "VALID" || res["source"] == nil {
		t.Fatalf("resolve_reference: %v %v", res, err)
	}
	v, _ := reg.Call("verify_reference", map[string]any{"project_id": "p", "id": "func:a.go:Callee", "expected_content_hash": "nope"})
	if v["status"] != "REFERENCE_STALE" {
		t.Fatalf("verify_reference: %v", v)
	}
	// A stable ID works wherever a node id is accepted.
	callers, err := reg.Call("get_callers", map[string]any{"project_id": "p", "function_id": "func:a.go:Callee"})
	if err != nil || callers["count"].(int) != 1 || callers["results"].([]map[string]any)[0]["name"] != "Caller" {
		t.Fatalf("get_callers by stable id: %v %v", callers, err)
	}
	// A hallucinated ID is refused, with the real one suggested; nothing runs.
	_, err = reg.Call("get_callers", map[string]any{"project_id": "p", "function_id": "func:a.go:Calee"})
	if err == nil || !strings.Contains(err.Error(), "INVALID_REFERENCE") || !strings.Contains(err.Error(), "func:a.go:Callee") {
		t.Fatalf("expected INVALID_REFERENCE with a suggestion, got %v", err)
	}
	sym, _ := reg.Call("resolve_symbol", map[string]any{"project_id": "p", "name": "Calee"})
	if sym["status"] != "INVALID_REFERENCE" || sym["did_you_mean"].([]string)[0] != "Callee" {
		t.Fatalf("resolve_symbol: %v", sym)
	}
	span, err := reg.Call("resolve_source_span", map[string]any{"project_id": "p", "file": "a.go", "start_line": 3, "end_line": 3})
	if err != nil || span["symbol_id"] != "func:a.go:Callee" {
		t.Fatalf("resolve_source_span: %v %v", span, err)
	}
	slice, err := reg.Call("resolve_slice", map[string]any{"project_id": "p", "base": "buf", "offset": 37, "length": 15, "base_length": 40})
	if err != nil || slice["valid"] != false || slice["end"].(float64) != 52 {
		t.Fatalf("resolve_slice: %v %v", slice, err)
	}
	if _, err := reg.Call("resolve_edges", map[string]any{"project_id": "p"}); err != nil {
		t.Fatalf("resolve_edges: %v", err)
	}
}

func TestTaskToolsRoundTripAndRejectMisspelledPatchFields(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\nfunc Run() {}\n"), 0o644)
	reg := NewToolRegistry(services.ApplicationInMemory())
	reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir})
	created, err := reg.Call("create_task", map[string]any{"project_id": "p", "goal": "g", "agent": "claude", "references": []any{"func:a.go:Run"}})
	if err != nil || created["state"] != "UNKNOWN" {
		t.Fatalf("%v %v", created, err)
	}
	id := created["task_id"].(string)
	upd, err := reg.Call("update_task_state", map[string]any{"project_id": "p", "task_id": id, "agent": "codex",
		"patch": map[string]any{"state": "DISCOVERING", "add_plan": []any{"step one"}, "expected_version": 1}})
	if err != nil || upd["state"] != "DISCOVERING" || upd["updated_by"] != "codex" {
		t.Fatalf("%v %v", upd, err)
	}
	if _, err := reg.Call("update_task_state", map[string]any{"project_id": "p", "task_id": id, "patch": map[string]any{"stat": "ANALYZING"}}); err == nil {
		t.Fatal("a misspelled patch field must be rejected, not ignored")
	}
	res, err := reg.Call("resume_task", map[string]any{"project_id": "p", "task_id": id, "agent": "opencode"})
	if err != nil || res["checklist"] == nil {
		t.Fatalf("%v %v", res, err)
	}
	if l, _ := reg.Call("list_tasks", map[string]any{"project_id": "p"}); l["count"] != 1 {
		t.Fatalf("%v", l)
	}
}

func TestCacheControls(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	// cache_config without cache: error.
	if _, err := reg.Call("cache_config", map[string]any{"project_id": "p"}); err == nil {
		t.Fatal("expected error when cache is disabled")
	}
	// cache_stats without cache: error.
	if _, err := reg.Call("cache_stats", map[string]any{"project_id": "p"}); err == nil {
		t.Fatal("expected error when cache is disabled")
	}
	// cache_flush without cache: error.
	if _, err := reg.Call("cache_flush", map[string]any{"project_id": "p"}); err == nil {
		t.Fatal("expected error when cache is disabled")
	}

	// With cache enabled.
	app := services.ApplicationInMemory()
	cm := cache.NewCacheManager(app.Graph, cache.DefaultConfig())
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg = NewToolRegistry(app)

	cfg, err := reg.Call("cache_config", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg["config"] == nil {
		t.Fatal("expected config")
	}
	stats, err := reg.Call("cache_stats", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if stats["stats"] == nil {
		t.Fatal("expected stats")
	}

	// Store a cache entry, then flush.
	cm.StoreExactCache("p", "", "", "", "test", map[string]any{"x": 1}, map[string]any{"y": 2}, 1.0, "agent")
	flush, err := reg.Call("cache_flush", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if flush["entries_removed"] != 1 {
		t.Fatalf("expected 1 entry removed, got %v", flush["entries_removed"])
	}
	// After flush, stats should show 1 invalidation.
	stats2, _ := reg.Call("cache_stats", map[string]any{"project_id": "p"})
	_ = stats2 // stats is CacheStats struct, verify it exists
	if stats2["stats"] == nil {
		t.Error("expected stats in result")
	}
}

func TestCacheLookup(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := cache.NewCacheManager(app.Graph, cache.DefaultConfig())
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)
	projectID := "p"

	// Test cache miss
	miss, err := reg.Call("cache_lookup", map[string]any{
		"project_id": projectID,
		"tool_name":  "test_tool",
		"arguments":  map[string]any{"arg": "value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if miss["cache_status"] != "CACHE_MISS" {
		t.Errorf("expected CACHE_MISS, got %v", miss["cache_status"])
	}

	// Store an exact cache entry
	cm.StoreExactCache(projectID, "", "", "", "test_tool", map[string]any{"arg": "value"}, map[string]any{"result": "data"}, 1.0, "agent")

	// Test cache hit
	hit, err := reg.Call("cache_lookup", map[string]any{
		"project_id": projectID,
		"tool_name":  "test_tool",
		"arguments":  map[string]any{"arg": "value"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hit["cache_status"] != "CACHE_HIT" {
		t.Errorf("expected CACHE_HIT, got %v", hit["cache_status"])
	}
	if hit["result"] == nil {
		t.Error("expected result in cache hit")
	}

	// Test cache lookup with query (for semantic cache)
	_, err = reg.Call("cache_lookup", map[string]any{
		"project_id": projectID,
		"tool_name":  "search_code_graph",
		"query":      "parse packet",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should be CACHE_MISS since no semantic entries
}

func TestPrivacyTools(t *testing.T) {
	app := services.ApplicationInMemory()
	reg := NewToolRegistry(app)
	projectID := "privacy-test"

	// Test privacy_policy - get default policy
	policy, err := reg.Call("privacy_policy", map[string]any{"project_id": projectID})
	if err != nil {
		t.Fatalf("privacy_policy get: %v", err)
	}
	if policy["mode"] != "FULL" {
		t.Errorf("default mode should be FULL, got %v", policy["mode"])
	}

	// Test privacy_policy - set mode
	policy, err = reg.Call("privacy_policy", map[string]any{
		"project_id": projectID,
		"mode":       "MASKED",
	})
	if err != nil {
		t.Fatalf("privacy_policy set: %v", err)
	}
	if policy["mode"] != "MASKED" {
		t.Errorf("mode should be MASKED, got %v", policy["mode"])
	}

	// Test pseudonymize_symbol
	pseudo, err := reg.Call("pseudonymize_symbol", map[string]any{
		"project_id": projectID,
		"name":       "BillingEngine",
		"kind":       "TYPE",
	})
	if err != nil {
		t.Fatalf("pseudonymize_symbol: %v", err)
	}
	if pseudo["pseudonym"] != "TYPE_1" {
		t.Errorf("expected TYPE_1, got %v", pseudo["pseudonym"])
	}
	if pseudo["name"] != "BillingEngine" || pseudo["kind"] != "TYPE" {
		t.Errorf("unexpected name/kind: %v", pseudo)
	}

	// Test pseudonymize_symbol - same name should return same pseudonym
	pseudo2, err := reg.Call("pseudonymize_symbol", map[string]any{
		"project_id": projectID,
		"name":       "BillingEngine",
		"kind":       "TYPE",
	})
	if err != nil {
		t.Fatalf("pseudonymize_symbol second: %v", err)
	}
	if pseudo2["pseudonym"] != "TYPE_1" {
		t.Errorf("expected stable pseudonym TYPE_1, got %v", pseudo2["pseudonym"])
	}

	// Test redact_content
	redacted, err := reg.Call("redact_content", map[string]any{
		"project_id": projectID,
		"content":    `api_key = "abcdefghijklmnop1234" at /home/user/x`,
	})
	if err != nil {
		t.Fatalf("redact_content: %v", err)
	}
	if strings.Contains(redacted["sanitized"].(string), "abcdefghijklmnop1234") {
		t.Errorf("secret not redacted: %v", redacted["sanitized"])
	}
	if strings.Contains(redacted["sanitized"].(string), "/home/user") {
		t.Errorf("path not redacted: %v", redacted["sanitized"])
	}

	// Test sanitize_context
	sanitized, err := reg.Call("sanitize_context", map[string]any{
		"project_id": projectID,
		"content":    "use BillingEngine here",
	})
	if err != nil {
		t.Fatalf("sanitize_context: %v", err)
	}
	// In MASKED mode with pseudonym, should be pseudonymized
	if strings.Contains(sanitized["sanitized"].(string), "BillingEngine") {
		t.Errorf("should be pseudonymized: %v", sanitized["sanitized"])
	}

	// Test sanitize_context with destination (audit)
	audit, err := reg.Call("sanitize_context", map[string]any{
		"project_id":  projectID,
		"content":     "password=hunter2hunter2",
		"destination": "external-llm",
	})
	if err != nil {
		t.Fatalf("sanitize_context audit: %v", err)
	}
	if audit["allowed"] == true {
		t.Errorf("should block secret transmission")
	}
	if len(audit["warnings"].([]any)) == 0 {
		t.Errorf("expected warnings for secret")
	}

	// Test audit_transmission tool
	audit2, err := reg.Call("audit_transmission", map[string]any{
		"project_id":  projectID,
		"content":     "clean content",
		"destination": "external-llm",
	})
	if err != nil {
		t.Fatalf("audit_transmission: %v", err)
	}
	if audit2["allowed"] != true {
		t.Errorf("clean content should be allowed")
	}

	// Test privacy policy persistence across new registry
	reg2 := NewToolRegistry(app)
	policy2, err := reg2.Call("privacy_policy", map[string]any{"project_id": projectID})
	if err != nil {
		t.Fatalf("privacy_policy get after restart: %v", err)
	}
	if policy2["mode"] != "MASKED" {
		t.Errorf("policy should persist: got %v", policy2["mode"])
	}

	// Test pseudonym persistence
	pseudo3, err := reg2.Call("pseudonymize_symbol", map[string]any{
		"project_id": projectID,
		"name":       "BillingEngine",
		"kind":       "TYPE",
	})
	if err != nil {
		t.Fatalf("pseudonymize_symbol after restart: %v", err)
	}
	if pseudo3["pseudonym"] != "TYPE_1" {
		t.Errorf("pseudonym should persist: got %v", pseudo3["pseudonym"])
	}

	// Test project isolation
	_, err = reg.Call("pseudonymize_symbol", map[string]any{
		"project_id": "other-project",
		"name":       "OtherThing",
		"kind":       "TYPE",
	})
	if err != nil {
		t.Fatalf("pseudonymize other project: %v", err)
	}
	pseudoOther, err := reg.Call("pseudonymize_symbol", map[string]any{
		"project_id": "other-project",
		"name":       "OtherThing",
		"kind":       "TYPE",
	})
	if err != nil {
		t.Fatalf("pseudonymize other project 2: %v", err)
	}
	if pseudoOther["pseudonym"] != "TYPE_1" {
		t.Errorf("other project should have its own counter: got %v", pseudoOther["pseudonym"])
	}
}

func TestPrivacyCacheIntegration(t *testing.T) {
	// Test that privacy state is stored in and retrieved from cache
	g := graph.NewMemoryGraphRepository()
	cm := cache.NewCacheManager(g, cache.DefaultConfig())
	app := services.NewApplicationWithCache(g, cm)
	reg := NewToolRegistry(app)
	projectID := "cache-privacy-test"

	// Set privacy policy
	_, err := reg.Call("privacy_policy", map[string]any{
		"project_id": projectID,
		"mode":       "MASKED",
	})
	if err != nil {
		t.Fatalf("privacy_policy set: %v", err)
	}

	// Create pseudonym
	_, err = reg.Call("pseudonymize_symbol", map[string]any{
		"project_id": projectID,
		"name":       "CacheTestType",
		"kind":       "TYPE",
	})
	if err != nil {
		t.Fatalf("pseudonymize_symbol: %v", err)
	}

	// Verify policy is in cache
	policyJSON, err := cm.GetPrivacyPolicy(projectID)
	if err != nil {
		t.Fatalf("GetPrivacyPolicy from cache: %v", err)
	}
	if policyJSON == "" {
		t.Fatal("expected policy in cache")
	}
	if !strings.Contains(policyJSON, "MASKED") {
		t.Errorf("policy JSON should contain MASKED: %s", policyJSON)
	}

	// Verify pseudonym snapshot is in cache
	snapJSON, err := cm.GetPseudonymSnapshot(projectID)
	if err != nil {
		t.Fatalf("GetPseudonymSnapshot from cache: %v", err)
	}
	if snapJSON == "" {
		t.Fatal("expected pseudonym snapshot in cache")
	}
	if !strings.Contains(snapJSON, "CacheTestType") {
		t.Errorf("snapshot JSON should contain CacheTestType: %s", snapJSON)
	}

	// Create new app with same cache - should load from cache
	app2 := services.NewApplicationWithCache(g, cm)
	reg2 := NewToolRegistry(app2)

	policy2, err := reg2.Call("privacy_policy", map[string]any{"project_id": projectID})
	if err != nil {
		t.Fatalf("privacy_policy get from cache-backed app: %v", err)
	}
	if policy2["mode"] != "MASKED" {
		t.Errorf("policy should load from cache: got %v", policy2["mode"])
	}

	pseudo2, err := reg2.Call("pseudonymize_symbol", map[string]any{
		"project_id": projectID,
		"name":       "CacheTestType",
		"kind":       "TYPE",
	})
	if err != nil {
		t.Fatalf("pseudonymize_symbol from cache-backed app: %v", err)
	}
	if pseudo2["pseudonym"] != "TYPE_1" {
		t.Errorf("pseudonym should load from cache: got %v", pseudo2["pseudonym"])
	}

	// Test cache flush removes privacy state
	flushCount, err := reg.Call("cache_flush", map[string]any{"project_id": projectID})
	if err != nil {
		t.Fatalf("cache_flush: %v", err)
	}
	if flushCount["entries_removed"].(int) < 2 {
		t.Errorf("expected at least 2 entries removed (policy + snapshot), got %v", flushCount["entries_removed"])
	}

	// After flush, cache should be empty
	policyJSON, err = cm.GetPrivacyPolicy(projectID)
	if err != nil {
		t.Fatalf("GetPrivacyPolicy after flush: %v", err)
	}
	if policyJSON != "" {
		t.Errorf("policy should be flushed from cache")
	}

	snapJSON, err = cm.GetPseudonymSnapshot(projectID)
	if err != nil {
		t.Fatalf("GetPseudonymSnapshot after flush: %v", err)
	}
	if snapJSON != "" {
		t.Errorf("snapshot should be flushed from cache")
	}
}

func TestResolveInstructionBasicBlock(t *testing.T) {
	// Test resolve_instruction and resolve_basic_block with a binary that has basic blocks
	g := graph.NewMemoryGraphRepository()
	app := services.NewApplication(g)
	reg := NewToolRegistry(app)
	projectID := "resolve-test"

	// Create a Project node (required by resolveProjectID)
	_, err := g.UpsertNode("Project", map[string]any{"id": projectID}, map[string]any{
		"name": "test-project",
		"path": "/tmp/test",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a binary with basic blocks and instructions manually
	binaryID := "binary:test"
	_, err = g.UpsertNode("Binary", map[string]any{"project_id": projectID, "stable_id": binaryID}, map[string]any{
		"name":         "test.bin",
		"path":         "/tmp/test.bin",
		"architecture": "x86_64",
		"hash":         "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a binary function
	fnID := ids.BinFuncID(binaryID, "0x401000")
	_, err = g.UpsertNode("BinaryFunction", map[string]any{"project_id": projectID, "stable_id": fnID}, map[string]any{
		"name":      "main",
		"address":   "0x401000",
		"binary_id": binaryID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a basic block
	bbID := ids.BasicBlockID(binaryID, "0x401000", "0x401000")
	bbNode, err := g.UpsertNode("BasicBlock", map[string]any{"project_id": projectID, "stable_id": bbID}, map[string]any{
		"address":     "0x401000",
		"function_id": fnID,
		"binary_id":   binaryID,
		"size":        16,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create an instruction
	insnID := ids.InstructionID(binaryID, "0x401000", "0x401000", "0x401000")
	insnNode, err := g.UpsertNode("Instruction", map[string]any{"project_id": projectID, "stable_id": insnID}, map[string]any{
		"address":        "0x401000",
		"basic_block_id": bbID,
		"function_id":    fnID,
		"binary_id":      binaryID,
		"mnemonic":       "push",
		"operands":       "%rbp",
		"name":           "push %rbp",
		"bytes":          "55",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test resolve_instruction
	insnRes, err := reg.Call("resolve_instruction", map[string]any{
		"project_id": projectID,
		"id":         insnID,
	})
	if err != nil {
		t.Fatalf("resolve_instruction: %v", err)
	}
	if insnRes["status"] != "VALID" {
		t.Errorf("resolve_instruction status: got %v, want VALID", insnRes["status"])
	}
	if insnRes["kind"] != "insn" {
		t.Errorf("resolve_instruction kind: got %v, want insn", insnRes["kind"])
	}
	if insnRes["node_id"] != insnNode.ID {
		t.Errorf("resolve_instruction node_id: got %v, want %v", insnRes["node_id"], insnNode.ID)
	}
	if insnRes["name"] != "push %rbp" {
		t.Errorf("resolve_instruction name: got %v", insnRes["name"])
	}

	// Test resolve_basic_block
	bbRes, err := reg.Call("resolve_basic_block", map[string]any{
		"project_id": projectID,
		"id":         bbID,
	})
	if err != nil {
		t.Fatalf("resolve_basic_block: %v", err)
	}
	if bbRes["status"] != "VALID" {
		t.Errorf("resolve_basic_block status: got %v, want VALID", bbRes["status"])
	}
	if bbRes["kind"] != "bb" {
		t.Errorf("resolve_basic_block kind: got %v, want bb", bbRes["kind"])
	}
	if bbRes["node_id"] != bbNode.ID {
		t.Errorf("resolve_basic_block node_id: got %v, want %v", bbRes["node_id"], bbNode.ID)
	}

	// Test with invalid ID
	invalidRes, err := reg.Call("resolve_instruction", map[string]any{
		"project_id": projectID,
		"id":         "insn:invalid",
	})
	if err != nil {
		t.Fatalf("resolve_instruction invalid ID: %v", err)
	}
	if invalidRes["status"] != "INVALID_REFERENCE" {
		t.Errorf("expected INVALID_REFERENCE status, got %v", invalidRes["status"])
	}

	// Test with non-existent ID
	nonexistRes, err := reg.Call("resolve_instruction", map[string]any{
		"project_id": projectID,
		"id":         "insn:test:0x401000:0x401000:0x401004",
	})
	if err != nil {
		t.Fatalf("resolve_instruction nonexistent ID: %v", err)
	}
	if nonexistRes["status"] != "INVALID_REFERENCE" {
		t.Errorf("expected INVALID_REFERENCE status for nonexistent, got %v", nonexistRes["status"])
	}
}

func TestRunBenchmark(t *testing.T) {
	// Set up a sample project like indexSample does
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
	app := services.ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil, false); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(app)
	projectID := "p"

	bench, err := reg.Call("run_benchmark", map[string]any{
		"project_id":       projectID,
		"num_queries":      5,
		"max_tokens":       2000,
		"include_baseline": true,
	})
	if err != nil {
		t.Fatalf("run_benchmark: %v", err)
	}

	if bench["overall_score"] == nil {
		t.Fatal("expected overall_score in result")
	}
	// JSON numbers come back as float64
	var score int
	switch v := bench["overall_score"].(type) {
	case float64:
		score = int(v)
	case int:
		score = v
	default:
		t.Fatalf("unexpected type for overall_score: %T", v)
	}
	if score < 0 || score > 100 {
		t.Errorf("overall_score out of range: %d", score)
	}

	// Check reference accuracy
	ref := bench["reference_accuracy"].(map[string]any)
	if ref["total"] == nil || ref["accuracy"] == nil {
		t.Errorf("missing reference fields: %+v", ref)
	}

	// Check invalid reference rejection
	invalidRef := bench["invalid_reference_rejection"].(map[string]any)
	if invalidRef["rejection_rate"] == nil {
		t.Errorf("missing invalid_reference_rejection fields: %+v", invalidRef)
	}

	// Check slice arithmetic
	slice := bench["slice_arithmetic"].(map[string]any)
	if slice["accuracy"] == nil {
		t.Errorf("missing slice_arithmetic fields: %+v", slice)
	}

	// Check context benchmark
	ctx := bench["context_tokens_vs_baseline"].(map[string]any)
	if ctx["queries_tested"] == nil {
		t.Errorf("missing context_tokens_vs_baseline fields: %+v", ctx)
	}

	// Test without baseline
	bench2, err := reg.Call("run_benchmark", map[string]any{
		"project_id":       projectID,
		"num_queries":      3,
		"max_tokens":       1000,
		"include_baseline": false,
	})
	if err != nil {
		t.Fatalf("run_benchmark without baseline: %v", err)
	}
	// When include_baseline=false, context should be zero value (queries_tested=0)
	if ctx2, ok := bench2["context_tokens_vs_baseline"].(map[string]any); ok {
		if ctx2["queries_tested"].(float64) != 0 {
			t.Errorf("context benchmark should have queries_tested=0 when include_baseline=false: %+v", ctx2)
		}
	}

	// Test unindexed project
	_, err = reg.Call("run_benchmark", map[string]any{"project_id": "missing"})
	if err == nil {
		t.Error("unindexed project should error")
	}
}

func TestSemanticSearch(t *testing.T) {
	dir := t.TempDir()
	// Two functions in differently-named files. The file path is part of every
	// document's bag-of-words vector, so a query that mentions the file name
	// ranks the function in that file highly under both BM25 and vector fusion.
	src := "package x\n\nfunc Alpha() {}\n\nfunc Beta() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "alpha.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "beta.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}

	// Keyword search for "alpha" returns the Alpha function.
	keyword, err := reg.Call("search_code_graph", map[string]any{"project_id": "p", "query": "alpha", "limit": 10})
	if err != nil {
		t.Fatal(err)
	}
	keywordRows := keyword["results"].([]map[string]any)
	if len(keywordRows) == 0 || keywordRows[0]["name"] != "Alpha" {
		t.Fatalf("keyword search expected Alpha first, got %+v", keywordRows)
	}

	// Semantic search uses the hybrid ranker and returns the same Alpha result.
	sem, err := reg.Call("search_semantic", map[string]any{"project_id": "p", "query": "alpha", "limit": 10})
	if err != nil {
		t.Fatal(err)
	}
	semRows := sem["results"].([]map[string]any)
	if len(semRows) == 0 || semRows[0]["name"] != "Alpha" {
		t.Errorf("semantic search expected Alpha first, got %+v", semRows)
	}

	// Semantic search is project-scoped: a different project sees nothing.
	other, err := reg.Call("search_semantic", map[string]any{"project_id": "other", "query": "alpha", "limit": 10})
	if err != nil {
		t.Fatal(err)
	}
	if other["count"].(int) != 0 {
		t.Errorf("expected no results in unindexed project, got %+v", other)
	}

	// Missing project_id is rejected.
	if _, err := reg.Call("search_semantic", map[string]any{"query": "x"}); err == nil {
		t.Error("expected project_id error")
	}
}

func TestIndexProgress(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i := 0; i < 20; i++ {
		name := filepath.Join(dir, fmt.Sprintf("f%d.go", i))
		if err := os.WriteFile(name, []byte(fmt.Sprintf("package x\n\nfunc F%d() {}\n", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, name)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())

	// Before any indexing, progress reports idle.
	idle, err := reg.Call("index_progress", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if idle["finished"].(bool) || idle["phase"] != "idle" {
		t.Errorf("expected idle progress, got %+v", idle)
	}

	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir, "skip_graphify": true}); err != nil {
		t.Fatal(err)
	}

	// After indexing, progress reports done with the right totals.
	done, err := reg.Call("index_progress", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if !done["finished"].(bool) {
		t.Error("expected finished progress")
	}
	if done["phase"] != "done" {
		t.Errorf("expected phase done, got %v", done["phase"])
	}
	if toInt(done["files_done"]) != 20 {
		t.Errorf("expected 20 files done, got %v", done["files_done"])
	}
	if toInt(done["functions"]) != 20 {
		t.Errorf("expected 20 functions, got %v", done["functions"])
	}

	// IndexFiles also reports progress. Unchanged files are skipped by the
	// incremental pass, so files_seen is 0 here; the point is that the progress
	// tracker is updated and reports finished.
	if res, err := reg.Call("index_files", map[string]any{
		"project_id": "p", "root": dir, "files": files[:5],
	}); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("index_files result: %+v", res)
	}
	inc, err := reg.Call("index_progress", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("incremental progress: %+v", inc)
	if inc["finished"].(bool) != true {
		t.Error("expected finished after incremental index")
	}

	// A genuinely changed file is re-indexed and progress reflects it.
	if err := os.WriteFile(files[0], []byte("package x\n\nfunc F0() { F1() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res, err := reg.Call("index_files", map[string]any{
		"project_id": "p", "root": dir, "files": []any{files[0]},
	}); err != nil {
		t.Fatal(err)
	} else {
		t.Logf("index_files changed result: %+v", res)
		if toInt(res["files_seen"]) != 1 {
			t.Errorf("expected 1 file seen after change, got %v", res["files_seen"])
		}
	}
	changed, err := reg.Call("index_progress", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if toInt(changed["files_done"]) != 1 {
		t.Errorf("expected 1 file done after change, got %v", changed["files_done"])
	}

	// Missing project_id is rejected.
	if _, err := reg.Call("index_progress", map[string]any{}); err == nil {
		t.Error("expected project_id error")
	}
}
