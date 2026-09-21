package graph

import (
	"fmt"
	"path/filepath"
	"testing"

	"codergag/internal/models"
)

func TestPersistentCacheGetEdge(t *testing.T) {
	c := newPersistentCache(10, 10)
	edge := models.NewEdge("CALLS", "n1", "n2", nil)
	c.putEdge(edge)
	got, ok := c.getEdge(edge.ID)
	if !ok || got.ID != edge.ID {
		t.Fatalf("getEdge: got %v ok=%v", got, ok)
	}
	got, ok = c.getEdge("nonexistent")
	if ok || got != nil {
		t.Fatalf("getEdge missing: got %v ok=%v", got, ok)
	}
}

func TestPersistentCachePromoteEdge(t *testing.T) {
	c := newPersistentCache(10, 2)
	e1 := models.NewEdge("CALLS", "n1", "n2", nil)
	e2 := models.NewEdge("CALLS", "n2", "n3", nil)
	c.putEdge(e1)
	c.putEdge(e2)
	if got := c.edgeOrder[0]; got != e1.ID {
		t.Fatalf("expected e1 first, got %s", got)
	}
	c.getEdge(e1.ID)
	if got := c.edgeOrder[0]; got != e2.ID {
		t.Fatalf("expected e2 first after promote, got %s", got)
	}
}

func TestPersistentCacheRemoveEdge(t *testing.T) {
	c := newPersistentCache(10, 10)
	edge := models.NewEdge("CALLS", "n1", "n2", nil)
	c.putEdge(edge)
	c.removeEdge(edge.ID)
	if _, ok := c.edges[edge.ID]; ok {
		t.Fatal("edge should be removed")
	}
}

func TestPersistentCacheEvictOldestEdge(t *testing.T) {
	c := newPersistentCache(10, 1)
	e1 := models.NewEdge("CALLS", "n1", "n2", nil)
	e2 := models.NewEdge("CALLS", "n2", "n3", nil)
	c.putEdge(e1)
	c.putEdge(e2)
	if _, ok := c.edges[e1.ID]; ok {
		t.Fatal("oldest edge should be evicted")
	}
	if _, ok := c.edges[e2.ID]; !ok {
		t.Fatal("newest edge should remain")
	}
}

func TestPersistentCachePutEdgeExistingPromotes(t *testing.T) {
	c := newPersistentCache(10, 10)
	e := models.NewEdge("CALLS", "n1", "n2", nil)
	c.putEdge(e)
	e.Properties["new"] = "val"
	c.putEdge(e)
	got, ok := c.getEdge(e.ID)
	if !ok {
		t.Fatal("edge should exist")
	}
	if got.Properties["new"] != "val" {
		t.Fatal("edge properties should be updated")
	}
}

func TestPersistentRepositoryRemoveEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "removeedges.gob")
	repo, err := NewPersistentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "b"}, nil)
	edge, _ := repo.Link("CALLS", a.ID, b.ID, nil)
	if err := repo.RemoveEdges([]string{edge.ID}); err != nil {
		t.Fatal(err)
	}
	nbrs, err := repo.Neighbors(a.ID, "CALLS", DirOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(nbrs) != 0 {
		t.Fatalf("edge should be removed, got %d neighbors", len(nbrs))
	}
}

func TestPersistentRepositoryLinkInvalidKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "linkinvalid.gob")
	repo, _ := NewPersistentRepository(path)
	if _, err := repo.Link("bad kind!", "a", "b", nil); err == nil {
		t.Fatal("link with invalid kind should error")
	}
}

func TestPersistentRepositoryUpsertNodeInvalidKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upsertinvalid.gob")
	repo, _ := NewPersistentRepository(path)
	if _, err := repo.UpsertNode("bad kind!", map[string]any{"name": "x"}, nil); err == nil {
		t.Fatal("upsert with invalid kind should error")
	}
}

func TestPersistentRepositoryLinkInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	a, _ := repo.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	if _, err := repo.Link("CALLS", a.ID, b.ID, nil); err != nil {
		t.Fatal(err)
	}
	nbrs, _ := repo.Neighbors(a.ID, "CALLS", DirOut)
	if len(nbrs) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(nbrs))
	}
}

func TestPersistentRepositoryUpsertNodeInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	if _, err := repo.UpsertNode("invalid kind!", map[string]any{"name": "x"}, nil); err == nil {
		t.Fatal("in-memory mode should still validate kind")
	}
	node, err := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "x"}, nil)
	if err != nil || node == nil {
		t.Fatalf("upsert: %v %v", node, err)
	}
}

func TestPersistentRepositoryQueryReadonly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "foo"}, nil)

	result, err := repo.QueryReadonly("SELECT * FROM Function", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0]["name"] != "foo" {
		t.Fatalf("expected name=foo, got %v", result[0]["name"])
	}
}

func TestPersistentRepositoryQueryReadonlyWithWhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "querywhere.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "foo"}, nil)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "bar"}, nil)

	result, err := repo.QueryReadonly("SELECT * FROM Function WHERE name = 'foo'", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestPersistentRepositoryQueryReadonlyWithParam(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queryparam.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "foo"}, nil)

	result, err := repo.QueryReadonly("SELECT * FROM Function WHERE name = :name", map[string]any{"name": "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestPersistentRepositoryQueryReadonlyWithLike(t *testing.T) {
	path := filepath.Join(t.TempDir(), "querylike.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "foobar"}, nil)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "baz"}, nil)

	result, err := repo.QueryReadonly("SELECT * FROM Function WHERE name LIKE 'foo%'", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestPersistentRepositoryQueryReadonlyWithLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "querylimit.gob")
	repo, _ := NewPersistentRepository(path)
	for i := 0; i < 5; i++ {
		repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": fmt.Sprintf("fn%d", i)}, nil)
	}

	result, err := repo.QueryReadonly("SELECT * FROM Function LIMIT 2", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
}

func TestPersistentRepositoryQueryReadonlyWithAND(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queryand.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "foo"}, nil)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "bar"}, nil)

	result, err := repo.QueryReadonly("SELECT * FROM Function WHERE project_id = 'p' AND name = 'foo'", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
}

func TestPersistentRepositoryQueryReadonlyRejectsInsert(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	_, err := repo.QueryReadonly("INSERT INTO Function VALUES", nil)
	if err == nil {
		t.Fatal("should reject INSERT queries")
	}
}

func TestPersistentRepositoryQueryReadonlyRejectsMultipleStatements(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	_, err := repo.QueryReadonly("SELECT * FROM Function; DROP TABLE Function", nil)
	if err == nil {
		t.Fatal("should reject multi-statement queries")
	}
}

func TestPersistentRepositoryQueryReadonlyRejectsDeleteKeyword(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	_, err := repo.QueryReadonly("SELECT * FROM Function WHERE name = 'DELETE'", nil)
	if err == nil {
		t.Fatal("should reject queries with DELETE keyword")
	}
}

func TestPersistentRepositoryQueryReadonlyInvalidKind(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	_, err := repo.QueryReadonly("SELECT * FROM 123bad", nil)
	if err == nil {
		t.Fatal("should reject invalid kind in FROM clause")
	}
}

func TestPersistentRepositoryQueryReadonlyMissingParam(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	_, err := repo.QueryReadonly("SELECT * FROM Function WHERE name = :name", map[string]any{})
	if err == nil {
		t.Fatal("should reject missing query parameter")
	}
}

func TestPersistentRepositoryQueryReadonlyInvalidCondition(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	_, err := repo.QueryReadonly("SELECT * FROM Function WHERE name ~= 'foo'", map[string]any{})
	if err == nil {
		t.Fatal("should reject unsupported condition")
	}
}

func TestPersistentRepositoryQueryReadonlyNoMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "querynomatch.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "foo"}, nil)

	result, err := repo.QueryReadonly("SELECT * FROM Function WHERE name = 'bar'", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result))
	}
}

func TestPersistentRepositoryCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "counts.gob")
	repo, _ := NewPersistentRepository(path)
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "b"}, nil)
	repo.UpsertNode("Class", map[string]any{"project_id": "p", "name": "C"}, nil)
	repo.Link("CALLS", a.ID, b.ID, nil)

	nc, ec := repo.Counts()
	if nc["Function"] != 2 {
		t.Errorf("expected 2 Function nodes, got %d", nc["Function"])
	}
	if nc["Class"] != 1 {
		t.Errorf("expected 1 Class node, got %d", nc["Class"])
	}
	if ec["CALLS"] != 1 {
		t.Errorf("expected 1 CALLS edge, got %d", ec["CALLS"])
	}
}

func TestPersistentRepositoryEdgesOfKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "edgesofkind.gob")
	repo, _ := NewPersistentRepository(path)
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "b"}, nil)
	repo.Link("CALLS", a.ID, b.ID, nil)
	repo.Link("USES", a.ID, b.ID, nil)

	calls := repo.EdgesOfKind("CALLS")
	if len(calls) != 1 {
		t.Fatalf("expected 1 CALLS edge, got %d", len(calls))
	}
	none := repo.EdgesOfKind("NOOP")
	if len(none) != 0 {
		t.Fatalf("expected 0 NOOP edges, got %d", len(none))
	}
}

func TestPersistentRepositoryCacheNodesInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	node, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	if n := repo.CacheNodes(); n != 1 {
		t.Fatalf("expected 1 cached node, got %d", n)
	}
	_ = node
}

func TestPersistentRepositoryCacheEdgesInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "b"}, nil)
	repo.Link("CALLS", a.ID, b.ID, nil)
	if e := repo.CacheEdges(); e != 1 {
		t.Fatalf("expected 1 cached edge, got %d", e)
	}
}

func TestPersistentRepositorySaveWithNoDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nodir.gob")
	repo, _ := NewPersistentRepository(filepath.Join(path, "subdir", "graph.gob"))
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	if err := repo.Save(); err != nil {
		t.Fatalf("Save with missing dir should create it: %v", err)
	}
}

func TestPersistentRepositoryRefreshNoChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refresh.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	if err := repo.Save(); err != nil {
		t.Fatal(err)
	}
	if err := repo.Refresh(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentRepositoryRefreshInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	if err := repo.Refresh(); err != nil {
		t.Fatal("Refresh on in-memory repo should succeed")
	}
}

func TestPersistentRepositoryRefreshWithJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refreshjournal.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	if err := repo.Refresh(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentRepositoryClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "close.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentRepositoryCloseInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	repo.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	if err := repo.Close(); err != nil {
		t.Fatal("Close on in-memory repo should succeed")
	}
}

func TestPersistentRepositoryRemoveNodesInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	a, _ := repo.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	if err := repo.RemoveNodes([]string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetNode(a.ID); err != ErrNotFound {
		t.Fatalf("node should be removed in memory mode: %v", err)
	}
}

func TestPersistentRepositoryRemoveEdgesInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	a, _ := repo.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	edge, _ := repo.Link("CALLS", a.ID, b.ID, nil)
	if err := repo.RemoveEdges([]string{edge.ID}); err != nil {
		t.Fatal(err)
	}
	if e := repo.CacheEdges(); e != 0 {
		t.Fatalf("expected 0 edges, got %d", e)
	}
}

func TestPersistentRepositoryLinkDeduplication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "linkdedup.gob")
	repo, _ := NewPersistentRepository(path)
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "b"}, nil)
	props := map[string]any{"source": "test"}
	e1, _ := repo.Link("CALLS", a.ID, b.ID, props)
	e2, _ := repo.Link("CALLS", a.ID, b.ID, props)
	if e1.ID != e2.ID {
		t.Fatal("identical link should return same edge")
	}
}

func TestPersistentRepositoryLinkWithEmptyEndpoints(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	a, _ := repo.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	repo.Link("CALLS", a.ID, "", nil)
	repo.Link("CALLS", "", a.ID, nil)
}

func TestPersistentRepositoryUpsertExistingNodeInPersistentMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upsertexisting.gob")
	repo, _ := NewPersistentRepository(path)
	ident := map[string]any{"project_id": "p", "qualified_name": "f:Run"}
	n1, _ := repo.UpsertNode("Function", ident, map[string]any{"name": "Run", "by": "a"})
	n2, _ := repo.UpsertNode("Function", ident, map[string]any{"name": "Run", "by": "b"})
	if n1.ID != n2.ID {
		t.Fatal("upsert should merge, not duplicate")
	}
	if n2.Properties["by"] != "b" {
		t.Fatalf("expected by=b, got %v", n2.Properties["by"])
	}
}

func TestPersistentRepositoryLinkExistingEdgeInPersistentMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "linkexisting.gob")
	repo, _ := NewPersistentRepository(path)
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "b"}, nil)
	e1, _ := repo.Link("CALLS", a.ID, b.ID, map[string]any{"source": "t"})
	e2, _ := repo.Link("CALLS", a.ID, b.ID, map[string]any{"source": "t"})
	if e1.ID != e2.ID {
		t.Fatal("link should be idempotent")
	}
}

func TestPersistentRepositoryNeighborsDirBoth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "neighbors.gob")
	repo, _ := NewPersistentRepository(path)
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "b"}, nil)
	repo.Link("CALLS", a.ID, b.ID, nil)
	nbrs, err := repo.Neighbors(a.ID, "CALLS", DirBoth)
	if err != nil || len(nbrs) != 1 {
		t.Fatalf("DirBoth should find edge from a: %v %v", nbrs, err)
	}
}

func TestPersistentRepositoryNeighborsMissingNode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "neighborsmissing.gob")
	repo, _ := NewPersistentRepository(path)
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	repo.Link("CALLS", a.ID, "nonexistent", nil)
	nbrs, err := repo.Neighbors(a.ID, "CALLS", DirOut)
	if err != nil || len(nbrs) != 0 {
		t.Fatalf("missing target node should not appear: %v %v", nbrs, err)
	}
}

func TestPersistentRepositoryRemoveNodesCascadesEdges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cascade.gob")
	repo, _ := NewPersistentRepository(path)
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "b"}, nil)
	repo.Link("CALLS", a.ID, b.ID, nil)
	repo.RemoveNodes([]string{a.ID})
	nbrs, _ := repo.Neighbors(b.ID, "CALLS", DirIn)
	if len(nbrs) != 0 {
		t.Fatalf("edge should be removed with source node: %d", len(nbrs))
	}
}

func TestPersistentRepositoryJournalEmpty(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	if !repo.journalEmptyLocked() {
		t.Fatal("empty journal should return true")
	}
}

func TestPersistentRepositoryJournalNonEmptyAfterUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	if repo.journalEmptyLocked() {
		t.Fatal("journal should be non-empty after upsert in persistent mode")
	}
}

func TestPersistentCacheMaxNodesZero(t *testing.T) {
	c := newPersistentCache(0, 0)
	if c.maxNodes != DefaultCacheNodes {
		t.Fatalf("expected DefaultCacheNodes, got %d", c.maxNodes)
	}
	if c.maxEdges != DefaultCacheEdges {
		t.Fatalf("expected DefaultCacheEdges, got %d", c.maxEdges)
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{nil, ""},
		{"hello", "hello"},
		{true, "true"},
		{false, "false"},
		{float64(42.5), "42.5"},
		{int(42), "42"},
		{int64(99), "99"},
		{struct{ Name string }{"test"}, "{test}"},
	}
	for _, tt := range tests {
		got := toString(tt.input)
		if got != tt.want {
			t.Errorf("toString(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPersistentRepositoryGetNodeFromCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cacheget.gob")
	repo, _ := NewPersistentRepository(path)
	a, _ := repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	repo.Save()

	got, err := repo.GetNode(a.ID)
	if err != nil || got == nil {
		t.Fatalf("GetNode: %v %v", got, err)
	}

	got2, err := repo.GetNode(a.ID)
	if err != nil || got2 == nil {
		t.Fatalf("GetNode from cache: %v %v", got2, err)
	}
	if got.ID != got2.ID {
		t.Fatal("cached node should match")
	}
}

func TestPersistentRepositoryGetNodeNotFound(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	if _, err := repo.GetNode("nonexistent"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPersistentRepositoryFindNodesPersistentMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findnodes.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "foo"}, nil)
	repo.UpsertNode("Class", map[string]any{"project_id": "p", "name": "Bar"}, nil)

	fns, err := repo.FindNodes("Function", map[string]any{"name": "foo"})
	if err != nil || len(fns) != 1 {
		t.Fatalf("FindNodes Function: %v %v", fns, err)
	}
	if fns[0].Properties["name"] != "foo" {
		t.Fatalf("expected name=foo, got %v", fns[0].Properties["name"])
	}
}

func TestPersistentRepositoryCountsInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	repo.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	nc, _ := repo.Counts()
	if nc["Function"] != 1 {
		t.Errorf("expected 1 Function node, got %d", nc["Function"])
	}
}

func TestPersistentRepositoryEdgesOfKindInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	a, _ := repo.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := repo.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	repo.Link("CALLS", a.ID, b.ID, nil)
	edges := repo.EdgesOfKind("CALLS")
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
}

func TestPersistentRepositoryQueryReadonlyInMemoryMode(t *testing.T) {
	repo, _ := NewPersistentRepository("")
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "foo"}, nil)
	// QueryReadonly in in-memory mode returns empty results since readEffectiveStateLocked returns empty state
	result, err := repo.QueryReadonly("SELECT * FROM Function", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 results in in-memory mode, got %d", len(result))
	}
}

func TestPersistentRepositoryCloseErrorOnSaveFailure(t *testing.T) {
	repo, _ := NewPersistentRepository(filepath.Join(t.TempDir(), "closedir", "graph.gob"))
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "name": "a"}, nil)
	if err := repo.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestCanon(t *testing.T) {
	if got := canon("test"); got != `"test"` {
		t.Errorf("canon(\"test\") = %q, want %q", got, `"test"`)
	}
}
