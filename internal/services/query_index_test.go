package services_test

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"codergag/internal/graph"
	"codergag/internal/services"
)

type authoritativeOnlyRepo struct {
	graph.GraphRepository
}

func buildTraversalRepo(t *testing.T) (graph.GraphRepository, string, string, string) {
	t.Helper()
	repo := graph.NewMemoryGraphRepository()
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "tp", "project_id": "tp"},
		map[string]any{"path": "/tmp/tp"}); err != nil {
		t.Fatal(err)
	}
	a, err := repo.UpsertNode("Function",
		map[string]any{"id": "a", "stable_id": "func:a.go:A", "project_id": "tp"},
		map[string]any{"name": "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.UpsertNode("Function",
		map[string]any{"id": "b", "stable_id": "func:b.go:B", "project_id": "tp"},
		map[string]any{"name": "B"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := repo.UpsertNode("Function",
		map[string]any{"id": "c", "stable_id": "func:c.go:C", "project_id": "tp"},
		map[string]any{"name": "C"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Link("CALLS", a.ID, b.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Link("CALLS", b.ID, c.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Link("DATA_FLOW", a.ID, c.ID, nil); err != nil {
		t.Fatal(err)
	}
	return repo, a.ID, b.ID, c.ID
}

func pathIDs(result map[string]any) []string {
	rows, _ := result["path"].([]map[string]any)
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i], _ = row["id"].(string)
	}
	return ids
}

func TestTrace_IndexVsAuthoritative(t *testing.T) {
	repo, aID, _, cID := buildTraversalRepo(t)
	indexed := services.NewCodeGraphService(repo)
	authoritative := services.NewCodeGraphService(authoritativeOnlyRepo{repo})

	indexedResult, err := indexed.Trace(aID, cID, 5)
	if err != nil {
		t.Fatal(err)
	}
	authoritativeResult, err := authoritative.Trace(aID, cID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if indexedResult["found"] != authoritativeResult["found"] {
		t.Fatalf("found mismatch: indexed=%v authoritative=%v", indexedResult["found"], authoritativeResult["found"])
	}
	if !reflect.DeepEqual(pathIDs(indexedResult), pathIDs(authoritativeResult)) {
		t.Fatalf("path mismatch: indexed=%v authoritative=%v", pathIDs(indexedResult), pathIDs(authoritativeResult))
	}
	if indexed.IndexFor("tp") == nil {
		t.Fatal("expected traversal to create a project index")
	}
}

func TestTraceDataFlow_IndexVsAuthoritative(t *testing.T) {
	repo, aID, _, cID := buildTraversalRepo(t)
	indexed := services.NewCodeGraphService(repo)
	authoritative := services.NewCodeGraphService(authoritativeOnlyRepo{repo})

	indexedResult, err := indexed.TraceDataFlow(aID, cID, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	authoritativeResult, err := authoritative.TraceDataFlow(aID, cID, 5, false)
	if err != nil {
		t.Fatal(err)
	}
	if indexedResult["found"] != authoritativeResult["found"] {
		t.Fatalf("found mismatch: indexed=%v authoritative=%v", indexedResult["found"], authoritativeResult["found"])
	}
	if !reflect.DeepEqual(pathIDs(indexedResult), pathIDs(authoritativeResult)) {
		t.Fatalf("path mismatch: indexed=%v authoritative=%v", pathIDs(indexedResult), pathIDs(authoritativeResult))
	}
}

func TestHierarchy_IndexVsAuthoritative(t *testing.T) {
	repo := graph.NewMemoryGraphRepository()
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "hp", "project_id": "hp"},
		map[string]any{"path": "/tmp/hp"}); err != nil {
		t.Fatal(err)
	}
	base, err := repo.UpsertNode("Class",
		map[string]any{"id": "base", "stable_id": "class:a.go:Base", "project_id": "hp"},
		map[string]any{"name": "Base"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := repo.UpsertNode("Class",
		map[string]any{"id": "child", "stable_id": "class:b.go:Child", "project_id": "hp"},
		map[string]any{"name": "Child"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Link("EXTENDS", child.ID, base.ID, nil); err != nil {
		t.Fatal(err)
	}

	indexed := services.NewCodeGraphService(repo)
	authoritative := services.NewCodeGraphService(authoritativeOnlyRepo{repo})
	indexedResult, err := indexed.Hierarchy(base.ID, "both", 3)
	if err != nil {
		t.Fatal(err)
	}
	authoritativeResult, err := authoritative.Hierarchy(base.ID, "both", 3)
	if err != nil {
		t.Fatal(err)
	}
	indexedSubtypes, _ := indexedResult["subtypes"].([]map[string]any)
	authoritativeSubtypes, _ := authoritativeResult["subtypes"].([]map[string]any)
	if len(indexedSubtypes) != len(authoritativeSubtypes) || len(indexedSubtypes) != 1 {
		t.Fatalf("subtypes mismatch: indexed=%v authoritative=%v", indexedSubtypes, authoritativeSubtypes)
	}
	if indexedSubtypes[0]["id"] != child.ID || authoritativeSubtypes[0]["id"] != child.ID {
		t.Fatalf("unexpected subtype: indexed=%v authoritative=%v", indexedSubtypes[0]["id"], authoritativeSubtypes[0]["id"])
	}
}

func TestIndexFor_ConcurrentSafe(t *testing.T) {
	repo := graph.NewMemoryGraphRepository()
	if _, err := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:F", "project_id": "cp"},
		map[string]any{"name": "F"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "cp", "project_id": "cp"},
		map[string]any{"path": "/tmp/cp"}); err != nil {
		t.Fatal(err)
	}
	svc := services.NewCodeGraphService(repo)
	errs := make(chan error, 20)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nodes, err := svc.Search("cp", "F", 10, false)
			if err != nil {
				errs <- err
				return
			}
			if len(nodes) == 0 {
				errs <- fmt.Errorf("search returned no nodes")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndexFor_RefreshesAfterMutation(t *testing.T) {
	repo := graph.NewMemoryGraphRepository()
	if _, err := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:F", "project_id": "rp"},
		map[string]any{"name": "F"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "rp", "project_id": "rp"},
		map[string]any{"path": "/tmp/rp"}); err != nil {
		t.Fatal(err)
	}
	svc := services.NewCodeGraphService(repo)
	idx := svc.IndexFor("rp")
	if idx == nil {
		t.Fatal("expected index")
	}
	if _, err := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:Fresh", "project_id": "rp"},
		map[string]any{"name": "Fresh"}); err != nil {
		t.Fatal(err)
	}
	svc.RefreshIndexAsync("rp")
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("index did not refresh after mutation")
		case <-ticker.C:
			nodes, err := svc.Search("rp", "Fresh", 10, false)
			if err == nil && len(nodes) == 1 {
				return
			}
		}
	}
}

func TestFind_IndexVsAuthoritative(t *testing.T) {
	repo := graph.NewMemoryGraphRepository()
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "fd", "project_id": "fd"},
		map[string]any{"path": "/tmp/fd"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("Func%d", i)
		if _, err := repo.UpsertNode("Function",
			map[string]any{"id": name, "stable_id": "func:a.go:" + name, "project_id": "fd"},
			map[string]any{"name": name, "qualified_name": "pkg." + name}); err != nil {
			t.Fatal(err)
		}
	}
	indexed := services.NewCodeGraphService(repo)
	authoritative := services.NewCodeGraphService(authoritativeOnlyRepo{repo})

	for _, name := range []string{"Func0", "func", "func2"} {
		indexedRows, err := indexed.Find("fd", "Function", name, 20)
		if err != nil {
			t.Fatalf("indexed Find(%q): %v", name, err)
		}
		authoritativeRows, err := authoritative.Find("fd", "Function", name, 20)
		if err != nil {
			t.Fatalf("authoritative Find(%q): %v", name, err)
		}
		if !reflect.DeepEqual(indexedRows, authoritativeRows) {
			t.Fatalf("Find(%q) mismatch:\n indexed=%v\n authoritative=%v", name, indexedRows, authoritativeRows)
		}
	}
	if indexed.IndexFor("fd") == nil {
		t.Fatal("expected Find to create a project index")
	}
}

func TestFunction_IndexVsAuthoritative(t *testing.T) {
	repo := graph.NewMemoryGraphRepository()
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "fn", "project_id": "fn"},
		map[string]any{"path": "/tmp/fn"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Function",
		map[string]any{"id": "target", "stable_id": "func:a.go:Target", "project_id": "fn"},
		map[string]any{"name": "Target", "qualified_name": "pkg.Target"}); err != nil {
		t.Fatal(err)
	}
	indexed := services.NewCodeGraphService(repo)
	authoritative := services.NewCodeGraphService(authoritativeOnlyRepo{repo})

	indexedFn, err := indexed.Function("fn", "Target")
	if err != nil {
		t.Fatal(err)
	}
	authoritativeFn, err := authoritative.Function("fn", "Target")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(indexedFn, authoritativeFn) {
		t.Fatalf("Function mismatch:\n indexed=%v\n authoritative=%v", indexedFn, authoritativeFn)
	}
}
