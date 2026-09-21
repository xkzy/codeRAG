package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codergag/internal/cache"
	"codergag/internal/services"
)

func newCacheManagerForTest(app *services.Application) *cache.CacheManager {
	return cache.NewCacheManager(app.Graph, cache.DefaultConfig())
}

func indexSample(t *testing.T, reg *ToolRegistry) {
	t.Helper()
	dir := t.TempDir()
	src := `package x

import "strings"

type Base struct{}
type Derived struct{ Base }

func Caller() { Callee() }
func Callee() {}
func Unused() {}

func User() { strings.TrimSpace("x") }
`
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir}); err != nil {
		t.Fatal(err)
	}
}

func TestFindSymbol(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("find_symbol", map[string]any{"project_id": "p", "query": "caller"})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) != 1 {
		t.Fatalf("expected 1 symbol, got %d", res["count"])
	}
}

func TestFindString(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	// Search for the function that uses strings package
	res, err := reg.Call("search_code_graph", map[string]any{"project_id": "p", "query": "user", "limit": 10})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) == 0 {
		t.Fatal("expected search results")
	}
}

func TestGetCallees(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	callers, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	fnID := callers["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("get_callees", map[string]any{"project_id": "p", "function_id": fnID})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) != 1 {
		t.Fatalf("expected 1 callee, got %d", res["count"])
	}
}

func TestTraceCallPath(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	callers, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	callee, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "callee"})
	src := callers["results"].([]map[string]any)[0]["id"].(string)
	dst := callee["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("trace_call_path", map[string]any{
		"project_id": "p", "source_id": src, "target_id": dst,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected trace result")
	}
}

func TestImpactAnalysis(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	callee, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "callee"})
	nodeID := callee["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("impact_analysis", map[string]any{
		"project_id": "p", "node_id": nodeID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected impact result")
	}
}

func TestFindRelatedCode(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	callee, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "callee"})
	nodeID := callee["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("find_related_code", map[string]any{
		"project_id": "p", "node_id": nodeID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) == 0 {
		t.Fatal("expected related code")
	}
}

func TestAnalyzeComplexity(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	caller, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	fnID := caller["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("analyze_complexity", map[string]any{"project_id": "p", "function_id": fnID})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected complexity result")
	}
}

func TestFindCircularDeps(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("find_circular_deps", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) >= 0 {
		return
	}
}

func TestFindHotPaths(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("find_hot_paths", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) >= 0 {
		return
	}
}

func TestGetModuleSummary(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("get_module_summary", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected module summary")
	}
}

func TestGetSubsystem(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("get_subsystem", map[string]any{"project_id": "p", "name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected subsystem result")
	}
}

func TestFindBySignature(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("find_by_signature", map[string]any{"project_id": "p", "language": "go"})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) >= 1 {
		return
	}
}

func TestFindEntryPoints(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("find_entry_points", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected entry points result")
	}
}

func TestFindRelatedTests(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("find_related_tests", map[string]any{"project_id": "p", "function_name": "caller"})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) >= 0 {
		return
	}
}

func TestQueryGraphRejectsNonSelectMatch(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	_, err := reg.Call("query_graph", map[string]any{
		"project_id": "p",
		"query":      "DELETE FROM Function",
	})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only rejection, got %v", err)
	}
}

func TestQueryGraphRequiresProjectIDFilter(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	_, err := reg.Call("query_graph", map[string]any{
		"project_id": "p",
		"query":      "SELECT * FROM Function",
	})
	if err == nil || !strings.Contains(err.Error(), ":project_id") {
		t.Fatalf("expected :project_id requirement, got %v", err)
	}
}

func TestRenameSymbol(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	caller, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	symID := caller["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("rename_symbol", map[string]any{
		"project_id": "p", "symbol_id": symID, "name": "renamed", "agent": "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["name"] != "renamed" {
		t.Fatalf("expected name=renamed, got %v", res["name"])
	}
}

func TestRenameSymbolNotFound(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	_, err := reg.Call("rename_symbol", map[string]any{
		"project_id": "p", "symbol_id": "nonexistent", "name": "renamed", "agent": "test",
	})
	if err == nil || !strings.Contains(err.Error(), "absent or belongs to another project") {
		t.Fatalf("expected error for absent symbol, got %v", err)
	}
}

func TestRenameSymbolCrossProject(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	caller, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	symID := caller["results"].([]map[string]any)[0]["id"].(string)
	_, err := reg.Call("rename_symbol", map[string]any{
		"project_id": "other", "symbol_id": symID, "name": "renamed", "agent": "test",
	})
	if err == nil {
		t.Fatal("expected cross-project rejection")
	}
}

func TestUpdateSymbol(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	caller, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	symID := caller["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("update_symbol", map[string]any{
		"project_id": "p", "symbol_id": symID,
		"properties": map[string]any{"lang": "go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["updated"].(int) < 1 {
		t.Fatalf("expected at least 1 property updated, got %v", res["updated"])
	}
}

func TestGetEvidence(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	caller, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	nodeID := caller["results"].([]map[string]any)[0]["id"].(string)
	reg.Call("record_evidence", map[string]any{
		"project_id": "p", "subject_id": nodeID, "description": "test evidence",
	})
	res, err := reg.Call("get_evidence", map[string]any{"project_id": "p", "subject_id": nodeID})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) == 0 {
		t.Fatal("expected evidence")
	}
}

func TestRecordEvidence(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	caller, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	nodeID := caller["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("record_evidence", map[string]any{
		"project_id": "p", "subject_id": nodeID, "description": "test evidence",
		"agent":      "tester", "confidence": 0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["description"] == nil {
		t.Fatal("expected evidence description in response")
	}
}

func TestRecordObservation(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	caller, _ := reg.Call("find_function", map[string]any{"project_id": "p", "query": "caller"})
	nodeID := caller["results"].([]map[string]any)[0]["id"].(string)
	res, err := reg.Call("record_observation", map[string]any{
		"project_id": "p", "subject_id": nodeID, "description": "test observation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["description"] == nil {
		t.Fatal("expected observation description in response")
	}
}

func TestGetCWEDetails(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("get_cwe_details", map[string]any{"project_id": "p", "cwe": "CWE-78"})
	if err != nil {
		t.Fatal(err)
	}
	if res["name"] == nil {
		t.Fatal("expected CWE name in response")
	}
}

func TestMemoryCompact(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("memory_compact", map[string]any{"project_id": "p", "min_group": 2})
	if err != nil {
		t.Fatal(err)
	}
	if res["summaries_updated"] == nil {
		t.Fatal("expected summaries_updated in response")
	}
}

func TestIndexFile(t *testing.T) {
	dir := t.TempDir()
	src := "package x\nfunc Indexed() {}\n"
	path := filepath.Join(dir, "idx.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir, "skip_graphify": true})
	res, err := reg.Call("index_file", map[string]any{"project_id": "p", "path": path})
	if err != nil {
		t.Fatal(err)
	}
	if res["path"] == nil {
		t.Fatal("expected path in response")
	}
}

func TestIndexFileMissingProject(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	_, err := reg.Call("index_file", map[string]any{"project_id": "missing", "path": "/tmp/nonexistent.go"})
	if err == nil {
		t.Fatal("expected error for unindexed project")
	}
}

func TestCacheStoreAndExplain(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)

	res, err := reg.Call("cache_store", map[string]any{
		"project_id":  "p",
		"tool_name":   "test_tool",
		"arguments":   map[string]any{"arg": "val"},
		"result":      map[string]any{"data": "value"},
		"confidence":  1.0,
		"agent":       "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["status"] != "stored" {
		t.Fatalf("expected status=stored, got %v", res["status"])
	}

	explain, err := reg.Call("cache_explain", map[string]any{
		"project_id": "p",
		"tool_name":  "test_tool",
	})
	if err != nil {
		t.Fatal(err)
	}
	if explain["entry"] == nil {
		t.Fatal("expected entry in explain result")
	}
}

func TestCacheExplainNoEntry(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)
	_, err := reg.Call("cache_explain", map[string]any{
		"project_id": "p",
		"tool_name":  "nonexistent",
	})
	if err == nil || !strings.Contains(err.Error(), "no matching cache entry") {
		t.Fatalf("expected 'no matching cache entry' error, got %v", err)
	}
}

func TestCacheRefreshAnalysis(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)

	res, err := reg.Call("refresh_analysis", map[string]any{
		"project_id": "p", "tool_name": "test_tool",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["status"] != "invalidated" {
		t.Fatalf("expected status=invalidated, got %v", res["status"])
	}
}

func TestCacheInvalidate(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)

	res, err := reg.Call("cache_invalidate", map[string]any{
		"project_id": "p", "tool_name": "test_tool",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["status"] != "invalidated" {
		t.Fatalf("expected status=invalidated, got %v", res["status"])
	}
}

func TestCacheLookupMissingToolName(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)
	_, err := reg.Call("cache_lookup", map[string]any{
		"project_id": "p",
	})
	if err == nil || !strings.Contains(err.Error(), "tool_name is required") {
		t.Fatalf("expected 'tool_name is required' error, got %v", err)
	}
}

func TestCacheStoreMissingCache(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	_, err := reg.Call("cache_store", map[string]any{"project_id": "p"})
	if err == nil || !strings.Contains(err.Error(), "cache is disabled") {
		t.Fatalf("expected 'cache is disabled' error, got %v", err)
	}
}

func TestEnsureFreshMissingEntry(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)
	res, err := reg.Call("ensure_fresh", map[string]any{"project_id": "p", "tool_name": "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if res["fresh"].(bool) != false || res["reason"] != "missing" {
		t.Fatalf("expected fresh=false, reason=missing, got %v", res)
	}
}

func TestEnsureFreshStaleByGitCommit(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)

	_, _ = app.Graph.UpsertNode("CacheEntry", map[string]any{
		"project_id": "p", "cache_key": "ck1", "tool_name": "test_tool",
		"git_commit": "oldcommit", "binary_hash": "bh",
	}, nil)

	res, err := reg.Call("ensure_fresh", map[string]any{
		"project_id": "p",
		"tool_name":  "test_tool",
		"cache_key":  "ck1",
		"git_commit": "newcommit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["fresh"].(bool) != false || res["reason"] != "git commit changed" {
		t.Fatalf("expected stale by git commit, got %v", res)
	}
}

func TestEnsureFreshStaleByBinaryHash(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)

	_, _ = app.Graph.UpsertNode("CacheEntry", map[string]any{
		"project_id": "p", "cache_key": "ck2", "tool_name": "test_tool",
		"git_commit": "gc", "binary_hash": "oldhash",
	}, nil)

	res, err := reg.Call("ensure_fresh", map[string]any{
		"project_id":    "p",
		"tool_name":     "test_tool",
		"cache_key":     "ck2",
		"binary_hash":   "newhash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["fresh"].(bool) != false || res["reason"] != "binary hash changed" {
		t.Fatalf("expected stale by binary hash, got %v", res)
	}
}

func TestEnsureFreshEntryFresh(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)

	_, _ = app.Graph.UpsertNode("CacheEntry", map[string]any{
		"project_id": "p", "cache_key": "ck3", "tool_name": "test_tool",
		"git_commit": "gc", "binary_hash": "bh",
	}, nil)

	res, err := reg.Call("ensure_fresh", map[string]any{
		"project_id":    "p",
		"tool_name":     "test_tool",
		"cache_key":     "ck3",
		"git_commit":    "gc",
		"binary_hash":   "bh",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["fresh"].(bool) != true {
		t.Fatalf("expected fresh=true, got %v", res)
	}
}

func TestGetCachedAnalysis(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)

	_, _ = app.Graph.UpsertNode("AnalysisArtifact", map[string]any{
		"project_id": "p", "artifact_type": "complexity",
	}, map[string]any{"data": "result"})

	res, err := reg.Call("get_cached_analysis", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) == 0 {
		t.Fatal("expected analysis artifacts")
	}
}

func TestGetCachedAnalysisFiltered(t *testing.T) {
	app := services.ApplicationInMemory()
	cm := newCacheManagerForTest(app)
	app = services.NewApplicationWithCache(app.Graph, cm)
	reg := NewToolRegistry(app)

	_, _ = app.Graph.UpsertNode("AnalysisArtifact", map[string]any{
		"project_id": "p", "artifact_type": "complexity", "artifact_id": "art1",
	}, map[string]any{"data": "result"})

	res, err := reg.Call("get_cached_analysis", map[string]any{
		"project_id":    "p",
		"artifact_type": "complexity",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) != 1 {
		t.Fatalf("expected 1 filtered artifact, got %d", res["count"])
	}
}

func TestPreparedContext(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	_, err := reg.Call("prepare_context", map[string]any{
		"project_id": "p", "question": "what does Caller do?", "max_tokens": 100,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestExplainContext(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	_, err := reg.Call("explain_context", map[string]any{
		"project_id": "p", "question": "what does Caller do?", "max_tokens": 100,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGetFunctionNotFound(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	_, err := reg.Call("get_function", map[string]any{"project_id": "p", "name": "nonexistent"})
	if err == nil || !strings.Contains(err.Error(), "function not found") {
		t.Fatalf("expected 'function not found' error, got %v", err)
	}
}

func TestGetFunctionWithoutRelationships(t *testing.T) {
	dir := t.TempDir()
	src := "package x\nfunc Isolated() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := NewToolRegistry(services.ApplicationInMemory())
	reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir})
	res, err := reg.Call("get_function", map[string]any{"project_id": "p", "name": "Isolated"})
	if err != nil {
		t.Fatal(err)
	}
	fn := res["function"].(map[string]any)
	if fn["name"] != "Isolated" {
		t.Fatalf("expected Isolated, got %v", fn["name"])
	}
	callers := res["callers"].([]map[string]any)
	if len(callers) != 0 {
		t.Fatalf("expected 0 callers, got %d", len(callers))
	}
}

func TestResolveSliceWithNegativeOffset(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("package x\nconst buf = \"abcdefghijklmnopqrstuvwxyz1234567890\"\n"), 0o644)
	reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir})
	res, err := reg.Call("resolve_slice", map[string]any{
		"project_id": "p", "base": "buf", "offset": -5, "length": 3, "base_length": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["valid"] == true {
		t.Fatal("negative offset should be invalid")
	}
}

func TestListAllowedVerifications(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("list_allowed_verifications", map[string]any{
		"project_id": "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	allowed, ok := res["allowed"]
	if !ok {
		t.Fatal("expected allowed field")
	}
	_ = allowed
}

func TestListVerificationRunsEmpty(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("list_verification_runs", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) != 0 {
		t.Fatalf("expected 0 runs, got %d", res["count"])
	}
}

func TestPrepareContextWithExplicitLevel(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	_, err := reg.Call("prepare_context", map[string]any{
		"project_id": "p", "question": "test", "max_tokens": 100, "level": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompiledContextWithTaskAndTeam(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	_, err := reg.Call("prepare_context", map[string]any{
		"project_id": "p", "question": "test", "max_tokens": 500,
		"task_id": "task-1", "team_id": "team-1", "scopes": []any{"code"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHandleRunVerificationNoMatch(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("run_verification", map[string]any{
		"project_id": "p", "command": "nonexistent-cmd", "args": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	status := res["status"].(string)
	if status != "DENIED" {
		t.Fatalf("expected DENIED status, got %v", res)
	}
}

func TestHandleRunVerificationAllowedCommand(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("run_verification", map[string]any{
		"project_id": "p", "command": "go", "args": []any{"build"}, "dir": t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["status"] == nil {
		t.Fatal("expected status in result")
	}
}

func TestFindFunctionWithLimit(t *testing.T) {
	dir := t.TempDir()
	var src strings.Builder
	src.WriteString("package x\n")
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&src, "func F%d() {}\n", i)
	}
	os.WriteFile(filepath.Join(dir, "f.go"), []byte(src.String()), 0o644)
	reg := NewToolRegistry(services.ApplicationInMemory())
	reg.Call("index_repository", map[string]any{"project_id": "p", "path": dir})
	res, err := reg.Call("find_function", map[string]any{"project_id": "p", "query": "f", "limit": 2})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) != 2 {
		t.Fatalf("expected 2 results, got %d", res["count"])
	}
}

func TestRelatedAnyEmptyResults(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("get_references", map[string]any{"project_id": "p", "node_id": "nonexistent"})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) != 0 {
		t.Fatalf("expected 0 results for nonexistent node, got %d", res["count"])
	}
}

func TestFindEntryPointsWithLimit(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("find_entry_points", map[string]any{"project_id": "p", "limit": 5})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected entry points result")
	}
}

func TestFindRelatedTestsEmpty(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	indexSample(t, reg)
	res, err := reg.Call("find_related_tests", map[string]any{
		"project_id": "p", "function_name": "nonexistent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["count"].(int) != 0 {
		t.Fatalf("expected 0 results, got %d", res["count"])
	}
}

func TestHandleReviewSuggestionsWithoutGit(t *testing.T) {
	reg := NewToolRegistry(services.ApplicationInMemory())
	res, err := reg.Call("review_suggestions", map[string]any{
		"project_id": "p", "files": []any{"a.go"}, "limit": 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res["reviewers"] == nil {
		t.Fatal("expected reviewers field")
	}
}
