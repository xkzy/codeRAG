# Coverage Improvement Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Raise overall test coverage from ~65% toward ≥80% across all packages.

**Architecture:** Pure test additions — no source changes. All tests use the existing in-memory graph (`graph.NewMemoryGraphRepository()`) and real package APIs. No mocks.

**Tech Stack:** Go stdlib `testing`, existing package APIs.

## Global Constraints

- Go 1.25+, module `codergag`
- All tests in `package <pkg>` (white-box) unless the package is unexported-only; then use `package <pkg>_test`
- `go test ./... -count=1` must pass after every task
- `go vet ./...` must be clean after every task
- No changes to source files — only add or modify `*_test.go` files
- Commit after each task: `test(<pkg>): <what>`

---

## Baseline (2026-09-21)

| Package | Current | Target | Key gaps |
|---|---|---|---|
| `adapters/lsp` | 9% | ≥40% | All methods — require live server; cover constructor + Name/Client |
| `cmd/codergag` | 48% | ≥58% | `splitList`, `orDash`, `applyMemoryLimit`, `homeDir`, `fail` |
| `internal/graph` | 56% | ≥80% | `Link`, `Neighbors`, `EdgesOfKind`, `RemoveEdges`, `Counts`, `QueryReadonly`, `NodeCount`, `EdgeCount`, `Close`, `UpsertNode`, `GetNode` |
| `internal/cache` | 57% | ≥75% | `Invalidate`, `InvalidateByCommit`, `InvalidateByBinaryHash`, `ExactLimit`, TTL expiry, `SemanticCache.Search/Clear`, `JobRegistry` |
| `internal/mcp` | 59% | ≥73% | `handleFindSymbol`, `handleFindString`, `handleGetCallees`, `handleTraceCallPath`, `handlePrepareContext`, `handleExplainContext`, `handleCacheExplain`, `handleGetCachedAnalysis`, `handleRefreshAnalysis`, `handleEnsureFresh`, `handleRunVerification`, `handleListVerificationRuns`, `handleListAllowedVerifications` |
| `adapters/gdb` | 70% | ≥90% | `Convert` edge cases: nil, empty, malformed |
| `adapters/lldb` | 70% | ≥90% | Same as gdb |
| `internal/services` | 70% | ≥76% | Branch paths only (0 functions at 0%); run coverage profile to identify |

---

## Task 1 — `internal/graph`: MemoryRepository full coverage

**Files:**
- Create: `internal/graph/memory_test.go`

**Interfaces:**
- Consumes: `NewMemoryGraphRepository()` → `GraphRepository`
- Produces: `Link`, `Neighbors`, `EdgesOfKind`, `Counts`, `RemoveEdges`, `QueryReadonly`, `NodeCount`, `EdgeCount`, `Close`, `UpsertNode`, `GetNode`

- [ ] **Step 1: Create the test file**

```go
package graph

import "testing"

func TestMemoryUpsertAndGetNode(t *testing.T) {
    g := NewMemoryGraphRepository()
    node, err := g.UpsertNode("Function", map[string]any{"name": "foo"}, map[string]any{"lang": "go"})
    if err != nil || node.ID == "" {
        t.Fatalf("upsert: %v %v", node, err)
    }
    got, err := g.GetNode(node.ID)
    if err != nil || got == nil || got.ID != node.ID {
        t.Fatalf("get: %v %v", got, err)
    }
    // Unknown ID returns nil, nil
    missing, err := g.GetNode("__nope__")
    if err != nil || missing != nil {
        t.Fatalf("missing should return nil,nil: %v %v", missing, err)
    }
}

func TestMemoryLinkAndNeighbors(t *testing.T) {
    g := NewMemoryGraphRepository()
    a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
    b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
    if err := g.Link(a.ID, b.ID, "CALLS", nil); err != nil {
        t.Fatal(err)
    }
    nbrs, err := g.Neighbors(a.ID, "CALLS", "out")
    if err != nil || len(nbrs) != 1 || nbrs[0].ID != b.ID {
        t.Fatalf("neighbors out: %v %v", nbrs, err)
    }
    nbrsIn, err := g.Neighbors(b.ID, "CALLS", "in")
    if err != nil || len(nbrsIn) != 1 {
        t.Fatalf("neighbors in: %v %v", nbrsIn, err)
    }
    // "both" direction
    _, err = g.Neighbors(a.ID, "CALLS", "both")
    if err != nil {
        t.Fatal(err)
    }
}

func TestMemoryEdgesOfKind(t *testing.T) {
    g := NewMemoryGraphRepository()
    a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
    b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
    g.Link(a.ID, b.ID, "CALLS", nil)
    g.Link(a.ID, b.ID, "USES", nil)

    calls, err := g.EdgesOfKind("CALLS")
    if err != nil || len(calls) == 0 {
        t.Fatalf("EdgesOfKind CALLS: %v %v", calls, err)
    }
    uses, err := g.EdgesOfKind("USES")
    if err != nil || len(uses) == 0 {
        t.Fatalf("EdgesOfKind USES: %v %v", uses, err)
    }
    none, err := g.EdgesOfKind("NOOP")
    if err != nil || len(none) != 0 {
        t.Fatalf("EdgesOfKind empty kind: %v %v", none, err)
    }
}

func TestMemoryRemoveEdges(t *testing.T) {
    g := NewMemoryGraphRepository()
    a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
    b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
    g.Link(a.ID, b.ID, "CALLS", nil)
    if err := g.RemoveEdges(a.ID, b.ID, "CALLS"); err != nil {
        t.Fatal(err)
    }
    nbrs, _ := g.Neighbors(a.ID, "CALLS", "out")
    if len(nbrs) != 0 {
        t.Fatal("edge should be removed")
    }
}

func TestMemoryCounts(t *testing.T) {
    g := NewMemoryGraphRepository()
    g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
    g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
    nc, ec, err := g.Counts()
    if err != nil || nc < 2 {
        t.Fatalf("Counts: %d %d %v", nc, ec, err)
    }
}

func TestMemoryNodeAndEdgeCount(t *testing.T) {
    g := NewMemoryGraphRepository()
    a, _ := g.UpsertNode("Function", map[string]any{"name": "x"}, nil)
    b, _ := g.UpsertNode("Function", map[string]any{"name": "y"}, nil)
    g.Link(a.ID, b.ID, "CALLS", nil)
    if n := g.NodeCount(); n < 2 {
        t.Fatalf("NodeCount: %d", n)
    }
    if e := g.EdgeCount(); e < 1 {
        t.Fatalf("EdgeCount: %d", e)
    }
}

func TestMemoryQueryReadonly(t *testing.T) {
    g := NewMemoryGraphRepository()
    g.UpsertNode("Function", map[string]any{"name": "alpha", "project_id": "p"}, nil)
    g.UpsertNode("Function", map[string]any{"name": "beta",  "project_id": "p"}, nil)
    results, err := g.QueryReadonly(
        "SELECT * FROM Function WHERE project_id = :project_id LIMIT 10",
        map[string]any{"project_id": "p"},
    )
    if err != nil {
        t.Fatal(err)
    }
    if len(results) == 0 {
        t.Fatal("expected results")
    }
}

func TestMemoryClose(t *testing.T) {
    g := NewMemoryGraphRepository()
    if err := g.Close(); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Run**
```bash
cd /home/khing/Desktop/codeintel/codeRAG
go test ./internal/graph -count=1 -v 2>&1 | grep -E "PASS|FAIL|ok"
go test ./internal/graph -cover -count=1 2>&1 | grep coverage
```
Expected: all pass, coverage ≥75%

- [ ] **Step 3: Commit**
```bash
git add internal/graph/memory_test.go
git commit -m "test(graph): MemoryRepository Link, Neighbors, EdgesOfKind, RemoveEdges, Counts, QueryReadonly"
```

---

## Task 2 — `internal/cache`: Invalidation, TTL, SemanticCache, JobRegistry

**Files:**
- Create: `internal/cache/invalidate_test.go`

**Interfaces:**
- Consumes: `NewCacheManager`, `NewSemanticCache`, `SemanticConfig`, `InvalidateArgs` (check exact type at `cache.go:454`), `Invalidate`, `InvalidateByCommit`, `InvalidateByBinaryHash`, `ExactLimit`, `SemanticCache.Index/Search/Clear`, `NewJobRegistry`, `Acquire`, `Release`, `Get`

- [ ] **Step 1: Read signatures**

Check `internal/cache/cache.go` lines 454–540 and `internal/cache/semantic.go` lines 26–199 for exact parameter types before writing.

- [ ] **Step 2: Write the test file**

```go
package cache

import (
    "testing"

    "codergag/internal/graph"
)

// ── Invalidation ────────────────────────────────────────────────────────────

func TestInvalidateByToolName(t *testing.T) {
    cm := newManager(t)
    args := map[string]any{"f": "main.go"}
    cm.StoreExactCache("p", "r", "abc", "", "tool", args, map[string]any{"v": 1.0}, 1.0, "a")

    e, _ := cm.CheckExactCache("p", "r", "abc", "", "tool", args)
    if e == nil {
        t.Fatal("expected hit before invalidate")
    }

    // Check exact Invalidate signature — may be Invalidate(InvalidateArgs{...}) or Invalidate(projectID, toolName)
    if err := cm.Invalidate(InvalidateArgs{ProjectID: "p", ToolName: "tool"}); err != nil {
        t.Fatal(err)
    }
    e2, _ := cm.CheckExactCache("p", "r", "abc", "", "tool", args)
    if e2 != nil {
        t.Fatal("expected miss after invalidate")
    }
}

func TestInvalidateByCommit(t *testing.T) {
    cm := newManager(t)
    args := map[string]any{"x": "1"}
    cm.StoreExactCache("p", "r", "commitX", "", "tool", args, map[string]any{}, 1.0, "a")
    if err := cm.InvalidateByCommit("p", "commitX"); err != nil {
        t.Fatal(err)
    }
    e, _ := cm.CheckExactCache("p", "r", "commitX", "", "tool", args)
    if e != nil {
        t.Fatal("expected miss after commit invalidation")
    }
}

func TestInvalidateByBinaryHash(t *testing.T) {
    cm := newManager(t)
    args := map[string]any{"x": "1"}
    cm.StoreExactCache("p", "r", "", "hashABC", "tool", args, map[string]any{}, 1.0, "a")
    if err := cm.InvalidateByBinaryHash("p", "hashABC"); err != nil {
        t.Fatal(err)
    }
    e, _ := cm.CheckExactCache("p", "r", "", "hashABC", "tool", args)
    if e != nil {
        t.Fatal("expected miss after binary hash invalidation")
    }
}

func TestExactLimit(t *testing.T) {
    cm := newManager(t)
    if limit := cm.ExactLimit(); limit != 100 {
        t.Fatalf("ExactLimit: got %d, want 100", limit)
    }
}

// ── TTL / isFresh ────────────────────────────────────────────────────────────

func TestTTLZeroSecondsIsStale(t *testing.T) {
    g := graph.NewMemoryGraphRepository()
    cfg := CacheConfig{
        Enabled: true,
        Exact:   LevelConfig{Enabled: true, MaxEntries: 100},
        TTL:     TTLConfig{Enabled: true, Seconds: 0}, // 0s = immediately stale
    }
    cm := NewCacheManager(g, cfg)
    args := map[string]any{"k": "v"}
    cm.StoreExactCache("p", "r", "c", "", "t", args, map[string]any{"ok": true}, 1.0, "a")
    e, _ := cm.CheckExactCache("p", "r", "c", "", "t", args)
    // 0-second TTL means immediately stale — should be nil or stale
    if e != nil && e.Freshness != FreshnessStale {
        t.Fatalf("expected nil or stale with 0s TTL, got: %+v", e)
    }
}

// ── SemanticCache ────────────────────────────────────────────────────────────

func TestSemanticCacheSearchAndClear(t *testing.T) {
    cfg := SemanticConfig{
        Enabled:    true,
        Threshold:  0.0, // accept any similarity
        MaxResults: 5,
        MaxEntries: 100,
    }
    sc := NewSemanticCache(cfg)

    entry := &CacheEntry{
        ProjectID: "p",
        ToolName:  "search",
        Result:    map[string]any{"hits": 1.0},
        Freshness: FreshnessValid,
    }
    sc.Index("find the parser function", entry)

    result := sc.Search("parser function", "p", "", "", "", 5)
    if result == nil {
        t.Fatal("expected semantic hit for overlapping query")
    }

    sc.Clear()
    after := sc.Search("parser function", "p", "", "", "", 5)
    if after != nil {
        t.Fatal("expected miss after Clear()")
    }
}

func TestSemanticCacheProjectIsolation(t *testing.T) {
    cfg := SemanticConfig{Enabled: true, Threshold: 0.0, MaxResults: 5, MaxEntries: 100}
    sc := NewSemanticCache(cfg)
    entry := &CacheEntry{ProjectID: "proj-A", ToolName: "t", Result: map[string]any{}, Freshness: FreshnessValid}
    sc.Index("database connection pool", entry)

    result := sc.Search("database connection pool", "proj-B", "", "", "", 5)
    if result != nil {
        t.Fatal("semantic cache must not cross project boundaries")
    }
}

// ── JobRegistry ──────────────────────────────────────────────────────────────

func TestJobRegistryAcquireReleaseGet(t *testing.T) {
    jr := NewJobRegistry()
    key := "proj:tool:abc"

    job1, fresh := jr.Acquire(key)
    if !fresh || job1 == nil {
        t.Fatal("first acquire should be fresh and non-nil")
    }

    // Second acquire: same job, not fresh
    job2, fresh2 := jr.Acquire(key)
    if fresh2 || job2 != job1 {
        t.Fatal("second acquire should not be fresh and should return same job")
    }

    if got := jr.Get(key); got != job1 {
        t.Fatal("Get should return the active job")
    }

    jr.Release(key)

    // After release, next acquire is fresh
    _, fresh3 := jr.Acquire(key)
    if !fresh3 {
        t.Fatal("after release, acquire should be fresh")
    }
}

func TestJobRegistryGetMissing(t *testing.T) {
    jr := NewJobRegistry()
    if got := jr.Get("nope"); got != nil {
        t.Fatalf("Get on missing key should return nil, got %v", got)
    }
}
```

- [ ] **Step 3: Run**
```bash
cd /home/khing/Desktop/codeintel/codeRAG
go test ./internal/cache -count=1 -v 2>&1 | grep -E "PASS|FAIL|ok"
go test ./internal/cache -cover -count=1 2>&1 | grep coverage
```
Expected: all pass, coverage ≥70%

- [ ] **Step 4: Commit**
```bash
git add internal/cache/invalidate_test.go
git commit -m "test(cache): Invalidate, InvalidateByCommit/BinaryHash, TTL expiry, SemanticCache search/clear, JobRegistry"
```

---

## Task 3 — `internal/mcp`: Uncovered handlers

**Files:**
- Modify: `internal/mcp/tools_test.go` (append new `Test*` functions)

**Interfaces:**
- Consumes: existing `newTestRouter(t)` and `callTool(t, r, name, args)` helpers; `testProjectID` constant — read the top of `tools_test.go` to confirm exact names
- Produces: coverage for `handleFindSymbol`, `handleFindString`, `handleGetCallees`, `handleTraceCallPath`, `handlePrepareContext`, `handleExplainContext`, `handleCacheExplain`, `handleGetCachedAnalysis`, `handleRefreshAnalysis`, `handleEnsureFresh`, `handleRunVerification`, `handleListVerificationRuns`, `handleListAllowedVerifications`

- [ ] **Step 1: Read top of `internal/mcp/tools_test.go`**

Confirm the names of `newTestRouter`, `callTool`, `testProjectID` (or equivalent) before writing.

- [ ] **Step 2: Append to `tools_test.go`**

```go
func TestFindSymbolAndString(t *testing.T) {
    r := newTestRouter(t)
    resp := callTool(t, r, "find_symbol", map[string]any{
        "project_id": testProjectID,
        "name":       "DoesNotExist",
    })
    if resp["error"] != nil {
        t.Fatalf("find_symbol error: %v", resp)
    }
    resp2 := callTool(t, r, "find_string", map[string]any{
        "project_id": testProjectID,
        "query":      "hello",
    })
    if resp2["error"] != nil {
        t.Fatalf("find_string error: %v", resp2)
    }
}

func TestGetCalleesAndTraceCallPath(t *testing.T) {
    r := newTestRouter(t)
    resp := callTool(t, r, "get_callees", map[string]any{
        "project_id": testProjectID,
        "name":       "main",
    })
    if resp["error"] != nil {
        t.Fatalf("get_callees: %v", resp)
    }
    resp2 := callTool(t, r, "trace_call_path", map[string]any{
        "project_id": testProjectID,
        "from":       "main",
        "to":         "fmt.Println",
    })
    if resp2["error"] != nil {
        t.Fatalf("trace_call_path: %v", resp2)
    }
}

func TestPrepareAndExplainContext(t *testing.T) {
    r := newTestRouter(t)
    resp := callTool(t, r, "prepare_context", map[string]any{
        "project_id": testProjectID,
        "query":      "indexing pipeline",
        "level":      float64(0),
        "max_tokens": float64(500),
    })
    if resp["error"] != nil {
        t.Fatalf("prepare_context: %v", resp)
    }
    resp2 := callTool(t, r, "explain_context", map[string]any{
        "project_id": testProjectID,
        "query":      "indexing pipeline",
        "max_tokens": float64(500),
    })
    if resp2["error"] != nil {
        t.Fatalf("explain_context: %v", resp2)
    }
}

func TestCacheExplainAndGetCachedAnalysis(t *testing.T) {
    r := newTestRouter(t)
    resp := callTool(t, r, "cache_explain", map[string]any{
        "project_id": testProjectID,
        "tool_name":  "find_function",
        "arguments":  map[string]any{"name": "foo"},
    })
    if resp["error"] != nil {
        t.Fatalf("cache_explain: %v", resp)
    }
    resp2 := callTool(t, r, "get_cached_analysis", map[string]any{
        "project_id": testProjectID,
        "tool_name":  "find_function",
        "arguments":  map[string]any{"name": "foo"},
    })
    if resp2["error"] != nil {
        t.Fatalf("get_cached_analysis: %v", resp2)
    }
}

func TestRefreshAnalysisAndEnsureFresh(t *testing.T) {
    r := newTestRouter(t)
    resp := callTool(t, r, "refresh_analysis", map[string]any{
        "project_id": testProjectID,
        "tool_name":  "find_function",
    })
    if resp["error"] != nil {
        t.Fatalf("refresh_analysis: %v", resp)
    }
    resp2 := callTool(t, r, "ensure_fresh", map[string]any{
        "project_id": testProjectID,
        "tool_name":  "find_function",
        "arguments":  map[string]any{},
    })
    if resp2["error"] != nil {
        t.Fatalf("ensure_fresh: %v", resp2)
    }
}

func TestListAllowedVerifications(t *testing.T) {
    r := newTestRouter(t)
    resp := callTool(t, r, "list_allowed_verifications", map[string]any{})
    if resp["error"] != nil {
        t.Fatalf("list_allowed_verifications: %v", resp)
    }
}

func TestListVerificationRuns(t *testing.T) {
    r := newTestRouter(t)
    resp := callTool(t, r, "list_verification_runs", map[string]any{
        "project_id": testProjectID,
    })
    if resp["error"] != nil {
        t.Fatalf("list_verification_runs: %v", resp)
    }
}
```

- [ ] **Step 3: Run**
```bash
cd /home/khing/Desktop/codeintel/codeRAG
go test ./internal/mcp -count=1 -v 2>&1 | grep -E "PASS|FAIL|ok"
go test ./internal/mcp -cover -count=1 2>&1 | grep coverage
```
Expected: all pass, coverage ≥70%

- [ ] **Step 4: Commit**
```bash
git add internal/mcp/tools_test.go
git commit -m "test(mcp): find_symbol, get_callees, prepare/explain_context, cache_explain, verification handlers"
```

---

## Task 4 — `cmd/codergag`: CLI helper functions

**Files:**
- Create: `cmd/codergag/helpers_test.go`

**Interfaces:**
- Consumes: `splitList` (index.go), `firstProject` (inject.go), `homeDir` (config.go), `orDash` (report.go), `applyMemoryLimit` (main.go)

- [ ] **Step 1: Verify signatures**

```bash
grep -n "^func splitList\|^func firstProject\|^func homeDir\|^func orDash\|^func applyMemoryLimit" \
  /home/khing/Desktop/codeintel/codeRAG/cmd/codergag/*.go
```

- [ ] **Step 2: Write the test file**

```go
package main

import "testing"

func TestSplitList(t *testing.T) {
    cases := []struct {
        in   string
        want int
    }{
        {"a,b,c", 3},
        {"single", 1},
        {"", 0},
        {" a , b ", 2},
    }
    for _, tc := range cases {
        got := splitList(tc.in)
        if len(got) != tc.want {
            t.Errorf("splitList(%q) = %v (len %d), want len %d", tc.in, got, len(got), tc.want)
        }
    }
}

func TestOrDash(t *testing.T) {
    if orDash("") != "-" {
        t.Fatal("empty string should return '-'")
    }
    if orDash("hello") != "hello" {
        t.Fatal("non-empty should pass through")
    }
}

func TestApplyMemoryLimit(t *testing.T) {
    // Must not panic for 0 (disabled), normal, and large values
    applyMemoryLimit(0)
    applyMemoryLimit(256)
    applyMemoryLimit(1024)
}

func TestHomeDirNotEmpty(t *testing.T) {
    if d := homeDir(); d == "" {
        t.Fatal("homeDir() must not be empty")
    }
}
```

- [ ] **Step 3: Run**
```bash
cd /home/khing/Desktop/codeintel/codeRAG
go test ./cmd/codergag -count=1 -run "TestSplitList|TestOrDash|TestApplyMemoryLimit|TestHomeDirNotEmpty" -v
go test ./cmd/codergag -cover -count=1 2>&1 | grep coverage
```
Expected: all pass, coverage ≥55%

- [ ] **Step 4: Commit**
```bash
git add cmd/codergag/helpers_test.go
git commit -m "test(cmd): splitList, orDash, applyMemoryLimit, homeDir helpers"
```

---

## Task 5 — `adapters/gdb` + `adapters/lldb`: Convert edge cases

**Files:**
- Modify: `adapters/gdb/adapter_test.go`
- Modify: `adapters/lldb/adapter_test.go`

**Interfaces:**
- Consumes: `Convert` in each adapter (currently 68.4% — read line 50 of each `adapter.go` for signature)

- [ ] **Step 1: Read Convert signature**
```bash
sed -n '45,60p' /home/khing/Desktop/codeintel/codeRAG/adapters/gdb/adapter.go
```

- [ ] **Step 2: Add edge-case tests** (append to each `adapter_test.go`)

Adjust the `Convert` call to match the real signature. Common patterns:

```go
// If Convert takes []byte:
func TestConvertEdgeCases(t *testing.T) {
    if got := Convert(nil); got != nil && len(got) != 0 {
        t.Fatalf("nil input: %v", got)
    }
    if got := Convert([]byte{}); got != nil && len(got) != 0 {
        t.Fatalf("empty input: %v", got)
    }
    // Malformed — must not panic
    _ = Convert([]byte("not valid gdb output\x00\xff"))
}

// If Convert takes string:
func TestConvertEdgeCases(t *testing.T) {
    _ = Convert("")
    _ = Convert("not valid output")
}
```

- [ ] **Step 3: Run**
```bash
cd /home/khing/Desktop/codeintel/codeRAG
go test ./adapters/gdb ./adapters/lldb -count=1 -v 2>&1 | grep -E "PASS|FAIL|ok"
go test ./adapters/gdb ./adapters/lldb -cover -count=1 2>&1 | grep coverage
```
Expected: ≥90% for both

- [ ] **Step 4: Commit**
```bash
git add adapters/gdb/adapter_test.go adapters/lldb/adapter_test.go
git commit -m "test(adapters): gdb/lldb Convert edge cases — nil, empty, malformed input"
```

---

## Task 6 — `adapters/lsp`: Constructor coverage

**Files:**
- Create: `adapters/lsp/lsp_test.go`

> [!NOTE]
> Most LSP methods require a live language server and cannot be tested in CI without one. Cover what works without a server: constructor, `Name()`, `Client()` with nil connection.

- [ ] **Step 1: Read the adapter interface**
```bash
sed -n '45,145p' /home/khing/Desktop/codeintel/codeRAG/adapters/lsp/adapter.go
```

- [ ] **Step 2: Write the test**

```go
package lsp_test

import (
    "testing"

    "codergag/adapters/lsp"
)

func TestNewLSPAdapterAndName(t *testing.T) {
    adapter := lsp.NewLSPAdapter("vscode", nil)
    if adapter == nil {
        t.Fatal("adapter must not be nil")
    }
    if adapter.Name() == "" {
        t.Fatal("Name() must not be empty")
    }
}

func TestLSPAdapterClientWithNilConn(t *testing.T) {
    adapter := lsp.NewLSPAdapter("vscode", nil)
    // Must not panic; returns nil client with nil conn
    _ = adapter.Client()
}
```

Adjust if `NewLSPAdapter` takes different arguments (e.g., no `"vscode"` string — read the signature first).

- [ ] **Step 3: Run**
```bash
cd /home/khing/Desktop/codeintel/codeRAG
go test ./adapters/lsp -count=1 -v 2>&1 | grep -E "PASS|FAIL|ok"
go test ./adapters/lsp -cover -count=1 2>&1 | grep coverage
```

- [ ] **Step 4: Commit**
```bash
git add adapters/lsp/lsp_test.go
git commit -m "test(adapters/lsp): NewLSPAdapter constructor, Name, Client without live server"
```

---

## Task 7 — `internal/services`: Branch path coverage

**Files:**
- Identify target functions via profile, then add to the relevant `*_test.go`

- [ ] **Step 1: Generate the coverage profile and find the lowest functions**

```bash
cd /home/khing/Desktop/codeintel/codeRAG
go test ./internal/services -coverprofile=/tmp/svc.out -count=1
go tool cover -func=/tmp/svc.out | grep -v "100.0%" | sort -t% -k1 -n | head -30
```

- [ ] **Step 2: Write targeted tests** for the 10–15 lowest-coverage functions. Focus on:
  - **Error branches**: unknown project ID, missing file, empty query string, nil args
  - **Boundary conditions**: 0 token budget, single-node graph, missing commit hash
  - **Return paths**: functions that return early on a specific condition

Example pattern for an error branch:

```go
func TestSomeServiceErrorPath(t *testing.T) {
    app := newTestApp(t)
    // Use an unknown project to exercise the "not found" branch
    _, err := app.SomeMethod("__unknown_project__", "query")
    if err != nil {
        t.Fatal(err) // should return empty, not error
    }
}
```

- [ ] **Step 3: Run**
```bash
go test ./internal/services -cover -count=1 2>&1 | grep coverage
```
Target: ≥75%

- [ ] **Step 4: Commit**
```bash
git add internal/services/*_test.go
git commit -m "test(services): branch coverage — error paths, empty inputs, boundary conditions"
```

---

## Final verification

```bash
cd /home/khing/Desktop/codeintel/codeRAG
go build ./...
go vet ./...
go test ./... -count=1
go test ./... -cover -count=1 2>&1 | grep -E "coverage:|no test"
```

All packages must pass with zero failures.

---

## Expected coverage after all tasks

| Package | Before | After |
|---|---|---|
| `internal/graph` | 56% | ≥80% |
| `internal/cache` | 57% | ≥75% |
| `internal/mcp` | 59% | ≥73% |
| `cmd/codergag` | 48% | ≥58% |
| `adapters/lsp` | 9% | ≥40% |
| `adapters/gdb` | 70% | ≥90% |
| `adapters/lldb` | 70% | ≥90% |
| `internal/services` | 70% | ≥76% |
| **Overall** | **~65%** | **≥75%** |
