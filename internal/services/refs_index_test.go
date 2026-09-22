package services_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"codergag/internal/graph"
	"codergag/internal/services"
)

func newReferenceTestRepo(t *testing.T) (graph.GraphRepository, *graph.GraphQueryIndex, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("0123456789\n", 35)
	for _, rel := range []string{"src/a.go", "src/b.go"} {
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	repo := graph.NewMemoryGraphRepository()
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "proj1", "project_id": "proj1"},
		map[string]any{"path": root}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "proj2", "project_id": "proj2"},
		map[string]any{"path": root}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("SourceFile",
		map[string]any{"stable_id": "file:src/a.go", "project_id": "proj1"},
		map[string]any{"name": "a.go", "rel_path": "src/a.go", "path": filepath.Join(root, "src/a.go"), "content_hash": "a-hash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("SourceFile",
		map[string]any{"stable_id": "file:src/b.go", "project_id": "proj1"},
		map[string]any{"name": "b.go", "rel_path": "src/b.go", "path": filepath.Join(root, "src/b.go"), "content_hash": "b-hash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:src/a.go:Parse", "project_id": "proj1"},
		map[string]any{"name": "Parse", "qualified_name": "pkg.Parse", "rel_path": "src/a.go", "path": filepath.Join(root, "src/a.go"), "line_start": 10, "line_end": 30, "content_hash": "parse-hash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:src/b.go:Emit", "project_id": "proj1"},
		map[string]any{"name": "Emit", "qualified_name": "pkg.Emit", "rel_path": "src/b.go", "path": filepath.Join(root, "src/b.go"), "line_start": 5, "line_end": 20, "content_hash": "emit-hash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Class",
		map[string]any{"stable_id": "class:src/a.go:Model", "project_id": "proj1"},
		map[string]any{"name": "Model", "qualified_name": "pkg.Model", "rel_path": "src/a.go", "path": filepath.Join(root, "src/a.go"), "line_start": 1, "line_end": 8, "content_hash": "model-hash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Struct",
		map[string]any{"stable_id": "struct:src/b.go:State", "project_id": "proj1"},
		map[string]any{"name": "State", "qualified_name": "pkg.State", "rel_path": "src/b.go", "path": filepath.Join(root, "src/b.go"), "line_start": 1, "line_end": 4, "content_hash": "state-hash"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:src/a.go:Parse", "project_id": "proj2"},
		map[string]any{"name": "Parse", "qualified_name": "other.Parse", "rel_path": "src/a.go", "path": filepath.Join(root, "src/a.go"), "line_start": 10, "line_end": 30, "content_hash": "other-parse-hash"}); err != nil {
		t.Fatal(err)
	}

	idx := graph.NewGraphQueryIndex(repo, "proj1")
	if err := idx.Rebuild(); err != nil {
		t.Fatal(err)
	}
	return repo, idx, root
}

func TestLookup_IndexVsAuthoritative(t *testing.T) {
	repo, idx, _ := newReferenceTestRepo(t)
	auth := services.NewReferenceResolver(repo)
	indexed := services.NewReferenceResolverWithIndex(repo, idx)
	const id = "func:src/a.go:Parse"

	authNode, authCands := auth.Lookup("proj1", id)
	idxNode, idxCands := indexed.Lookup("proj1", id)
	if authNode == nil || idxNode == nil {
		t.Fatalf("lookup missed: auth=%v idx=%v", authNode, idxNode)
	}
	if authNode.ID != idxNode.ID || authNode.Kind != idxNode.Kind || !reflect.DeepEqual(authNode.Properties, idxNode.Properties) {
		t.Fatalf("lookup node mismatch: auth=%+v idx=%+v", authNode, idxNode)
	}
	if !reflect.DeepEqual(authCands, idxCands) {
		t.Fatalf("lookup suggestions mismatch: auth=%v idx=%v", authCands, idxCands)
	}
}

func TestLookup_MissingID_IndexVsAuthoritative(t *testing.T) {
	repo, idx, _ := newReferenceTestRepo(t)
	auth := services.NewReferenceResolver(repo)
	indexed := services.NewReferenceResolverWithIndex(repo, idx)
	const id = "func:src/a.go:Parsx"

	authNode, authCands := auth.Lookup("proj1", id)
	idxNode, idxCands := indexed.Lookup("proj1", id)
	if authNode != nil || idxNode != nil {
		t.Fatalf("missing ID resolved: auth=%v idx=%v", authNode, idxNode)
	}
	if !reflect.DeepEqual(authCands, idxCands) || len(authCands) == 0 {
		t.Fatalf("missing ID suggestions mismatch: auth=%v idx=%v", authCands, idxCands)
	}
}

func TestResolveSymbol_IndexVsAuthoritative(t *testing.T) {
	repo, idx, _ := newReferenceTestRepo(t)
	auth := services.NewReferenceResolver(repo)
	indexed := services.NewReferenceResolverWithIndex(repo, idx)

	for _, name := range []string{"Parse", "Parsx", "pkg.Model"} {
		authMatches, authSugg := auth.ResolveSymbol("proj1", name)
		idxMatches, idxSugg := indexed.ResolveSymbol("proj1", name)
		if !reflect.DeepEqual(authMatches, idxMatches) {
			t.Fatalf("ResolveSymbol(%q) matches mismatch: auth=%+v idx=%+v", name, authMatches, idxMatches)
		}
		if !reflect.DeepEqual(authSugg, idxSugg) {
			t.Fatalf("ResolveSymbol(%q) suggestions mismatch: auth=%v idx=%v", name, authSugg, idxSugg)
		}
	}
}

func TestResolveSourceSpan_IndexVsAuthoritative(t *testing.T) {
	repo, idx, root := newReferenceTestRepo(t)
	auth := services.NewReferenceResolver(repo)
	indexed := services.NewReferenceResolverWithIndex(repo, idx)
	file := filepath.Join(root, "src/a.go")

	authSpan, authSym, authErr := auth.ResolveSourceSpan("proj1", file, 12, 12)
	idxSpan, idxSym, idxErr := indexed.ResolveSourceSpan("proj1", file, 12, 12)
	if authErr != nil || idxErr != nil {
		t.Fatalf("ResolveSourceSpan error: auth=%v idx=%v", authErr, idxErr)
	}
	if authSym != idxSym || !reflect.DeepEqual(authSpan, idxSpan) {
		t.Fatalf("ResolveSourceSpan mismatch: auth=(%q,%+v) idx=(%q,%+v)", authSym, authSpan, idxSym, idxSpan)
	}
}

func TestReferenceResolverIndexProjectIsolationAndFallback(t *testing.T) {
	repo, idx, _ := newReferenceTestRepo(t)
	auth := services.NewReferenceResolver(repo)
	indexed := services.NewReferenceResolverWithIndex(repo, idx)
	const id = "func:src/a.go:Parse"

	authProj1, _ := auth.Lookup("proj1", id)
	idxProj1, _ := indexed.Lookup("proj1", id)
	authProj2, _ := auth.Lookup("proj2", id)
	idxProj2, _ := indexed.Lookup("proj2", id)
	if authProj1 == nil || idxProj1 == nil || authProj2 == nil || idxProj2 == nil {
		t.Fatalf("project lookup missed: proj1=%v/%v proj2=%v/%v", authProj1, idxProj1, authProj2, idxProj2)
	}
	if authProj1.Properties["project_id"] != "proj1" || idxProj1.Properties["project_id"] != "proj1" ||
		authProj2.Properties["project_id"] != "proj2" || idxProj2.Properties["project_id"] != "proj2" {
		t.Fatalf("project scope leaked: proj1=%v/%v proj2=%v/%v", authProj1, idxProj1, authProj2, idxProj2)
	}

	idx.Invalidate()
	if _, err := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:src/a.go:Fresh", "project_id": "proj1"},
		map[string]any{"name": "Fresh", "qualified_name": "pkg.Fresh", "rel_path": "src/a.go", "line_start": 31, "line_end": 35}); err != nil {
		t.Fatal(err)
	}
	authFresh, _ := auth.Lookup("proj1", "func:src/a.go:Fresh")
	idxFresh, _ := indexed.Lookup("proj1", "func:src/a.go:Fresh")
	if authFresh == nil || idxFresh == nil || authFresh.ID != idxFresh.ID {
		t.Fatalf("stale index did not fall back: auth=%v idx=%v", authFresh, idxFresh)
	}

	nilIndexed := services.NewReferenceResolverWithIndex(repo, nil)
	nilNode, nilCands := nilIndexed.Lookup("proj1", id)
	authNode, authCands := auth.Lookup("proj1", id)
	if nilNode == nil || nilNode.ID != authNode.ID || !reflect.DeepEqual(nilCands, authCands) {
		t.Fatalf("nil index fallback mismatch: auth=%v/%v nil=%v/%v", authNode, authCands, nilNode, nilCands)
	}
}
