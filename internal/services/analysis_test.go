package services

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"codergag/internal/graph"
	"codergag/internal/models"
)

func setupAnalysisRepo(t *testing.T, setup func(r graph.GraphRepository)) graph.GraphRepository {
	t.Helper()
	r := graph.NewMemoryGraphRepository()
	setup(r)
	return r
}

func upsNode(t *testing.T, r graph.GraphRepository, kind, id string, props map[string]any) *graph.EdgeNode {
	t.Helper()
	identity := map[string]any{"id": id, "project_id": "p"}
	node, err := r.UpsertNode(kind, identity, props)
	if err != nil {
		t.Fatalf("UpsertNode %s: %v", id, err)
	}
	return &graph.EdgeNode{Node: node}
}

// ---- Complexity ----

func TestComplexity_FunctionAbsent(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {})
	svc := NewAnalysisService(r)
	got, err := svc.Complexity("p", "nope")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if got != nil {
		t.Fatalf("expected nil result, got %v", got)
	}
}

func TestComplexity_BelongsToAnotherProject(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "fn1", map[string]any{
			"name": "foo",
		})
	})
	svc := NewAnalysisService(r)
	got, err := svc.Complexity("other-project", "fn1")
	if err == nil {
		t.Fatal("expected error for cross-project")
	}
	if got != nil {
		t.Fatalf("expected nil result, got %v", got)
	}
}

func TestComplexity_NoPath(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "fn1", map[string]any{
			"name": "foo",
		})
	})
	svc := NewAnalysisService(r)
	got, err := svc.Complexity("p", "fn1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if score, ok := got["cyclomatic_complexity"]; !ok || score.(int) != 1 {
		t.Fatalf("expected score 1, got %v", score)
	}
}

func TestComplexity_WithPath(t *testing.T) {
	dir := t.TempDir()
	src := `package main
func main() {
	if x { }
	for i := 0; i < 10; i++ { }
	if a && b { }
	select { case <-ch: case <-ch2: }
}
func catchErr() {
	if e == nil { }
}
`
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "fn1", map[string]any{
			"name": "main",
			"path": path,
		})
	})
	svc := NewAnalysisService(r)
	got, err := svc.Complexity("p", "fn1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	score := got["cyclomatic_complexity"].(int)
	if score < 5 {
		t.Fatalf("expected score >= 5 (if + for + if + case + if), got %d", score)
	}
	breakdown := got["breakdown"].(map[string]int)
	if breakdown["if"] == 0 {
		t.Fatal("expected if count > 0")
	}
	if breakdown["for"] == 0 {
		t.Fatal("expected for count > 0")
	}
	if breakdown["case"] == 0 {
		t.Fatal("expected case count > 0")
	}
}

func TestComplexity_UnreadablePath(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "fn1", map[string]any{
			"name": "foo",
			"path": "/nonexistent/file/path.go",
		})
	})
	svc := NewAnalysisService(r)
	got, err := svc.Complexity("p", "fn1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	score := got["cyclomatic_complexity"].(int)
	if score != 1 {
		t.Fatalf("expected score 1, got %d", score)
	}
}

// ---- errOrVal ----

func TestErrOrVal_NilErrReturnsDefault(t *testing.T) {
	got := errOrVal(nil, "msg", "default")
	if got.Error() != "default" {
		t.Fatalf("expected 'default', got %q", got.Error())
	}
}

func TestErrOrVal_NonNilErrReturned(t *testing.T) {
	inner := errFoo2
	got := errOrVal(inner, "msg", "default")
	if got != inner {
		t.Fatalf("expected inner error, got %v", got)
	}
}

var errFoo2 error = &ServiceError{Message: "inner"}

func TestServiceError(t *testing.T) {
	e := &ServiceError{Message: "boom"}
	if e.Error() != "boom" {
		t.Fatalf("expected 'boom', got %q", e.Error())
	}
}

// ---- indexOf ----

func TestIndexOf_Found(t *testing.T) {
	if got := indexOf([]string{"a", "b", "c"}, "b"); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
}

func TestIndexOf_NotFound(t *testing.T) {
	if got := indexOf([]string{"a", "b", "c"}, "z"); got != -1 {
		t.Fatalf("expected -1, got %d", got)
	}
}

func TestIndexOf_EmptySlice(t *testing.T) {
	if got := indexOf([]string{}, "x"); got != -1 {
		t.Fatalf("expected -1, got %d", got)
	}
}

// ---- sliceEqual ----

func TestSliceEqual_Equal(t *testing.T) {
	if !sliceEqual([]string{"a", "b"}, []string{"a", "b"}) {
		t.Fatal("expected true")
	}
}

func TestSliceEqual_DifferentLength(t *testing.T) {
	if sliceEqual([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("expected false for different length")
	}
}

func TestSliceEqual_DifferentContent(t *testing.T) {
	if sliceEqual([]string{"a", "b"}, []string{"a", "c"}) {
		t.Fatal("expected false for different content")
	}
}

func TestSliceEqual_Empty(t *testing.T) {
	if !sliceEqual([]string{}, []string{}) {
		t.Fatal("expected true for empty slices")
	}
}

// ---- containsCycle ----

func TestContainsCycle_Exists(t *testing.T) {
	cycles := [][]string{{"a", "b"}, {"c", "d"}}
	if !containsCycle(cycles, []string{"a", "b"}) {
		t.Fatal("expected true")
	}
}

func TestContainsCycle_NotExists(t *testing.T) {
	cycles := [][]string{{"a", "b"}, {"c", "d"}}
	if containsCycle(cycles, []string{"a", "c"}) {
		t.Fatal("expected false")
	}
}

// ---- CircularDependencies ----

func TestCircularDependencies_NoNodes(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {})
	svc := NewAnalysisService(r)
	got, err := svc.CircularDependencies("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no cycles, got %v", got)
	}
}

func TestCircularDependencies_WithCycle(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		// a -> b -> c -> a (cycle)
		na := upsNode(t, r, "SourceFile", "a", map[string]any{"path": "a.go"})
		nb := upsNode(t, r, "SourceFile", "b", map[string]any{"path": "b.go"})
		nc := upsNode(t, r, "SourceFile", "c", map[string]any{"path": "c.go"})
		r.Link("IMPORTS", na.Node.ID, nb.Node.ID, nil)
		r.Link("IMPORTS", nb.Node.ID, nc.Node.ID, nil)
		r.Link("IMPORTS", nc.Node.ID, na.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.CircularDependencies("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cycle, got %d: %v", len(got), got)
	}
	if len(got[0]) != 3 {
		t.Fatalf("expected cycle length 3, got %d", len(got[0]))
	}
}

func TestCircularDependencies_NoCycle(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		na := upsNode(t, r, "SourceFile", "a", map[string]any{"path": "a.go"})
		nb := upsNode(t, r, "SourceFile", "b", map[string]any{"path": "b.go"})
		nc := upsNode(t, r, "SourceFile", "c", map[string]any{"path": "c.go"})
		r.Link("IMPORTS", na.Node.ID, nb.Node.ID, nil)
		r.Link("IMPORTS", nb.Node.ID, nc.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.CircularDependencies("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no cycles, got %v", got)
	}
}

func TestCircularDependencies_LimitReached(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		// Two independent cycles: a->a and b->b
		na := upsNode(t, r, "SourceFile", "a", map[string]any{"path": "a.go"})
		nb := upsNode(t, r, "SourceFile", "b", map[string]any{"path": "b.go"})
		r.Link("IMPORTS", na.Node.ID, na.Node.ID, nil)
		r.Link("IMPORTS", nb.Node.ID, nb.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.CircularDependencies("p", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cycle (limit=1), got %d: %v", len(got), got)
	}
}

func TestCircularDependencies_Deduplicates(t *testing.T) {
	// Multiple edges between same pair forming the same cycle path.
	// The dedup logic should not add duplicate cycles.
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		na := upsNode(t, r, "SourceFile", "a", map[string]any{"path": "a.go"})
		nb := upsNode(t, r, "SourceFile", "b", map[string]any{"path": "b.go"})
		r.Link("IMPORTS", na.Node.ID, nb.Node.ID, nil)
		r.Link("IMPORTS", nb.Node.ID, na.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.CircularDependencies("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 unique cycle, got %d: %v", len(got), got)
	}
}

// ---- HotPaths ----

func TestHotPaths_NoFunctions(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {})
	svc := NewAnalysisService(r)
	got, err := svc.HotPaths("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
	}
}

func TestHotPaths_SingleFunctionNoCallers(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "fn1", map[string]any{"name": "foo"})
	})
	svc := NewAnalysisService(r)
	got, err := svc.HotPaths("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results (no callers), got %d", len(got))
	}
}

func TestHotPaths_WithCallers(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		// caller -> callee (CALLS edge: caller is From, callee is To)
		// HotPaths counts transitive _in_ callers, so we need callers of callee
		// caller1 calls callee1
		callee := upsNode(t, r, "Function", "fn_cal", map[string]any{"name": "callee"})
		caller1 := upsNode(t, r, "Function", "fn_c1", map[string]any{"name": "caller1"})
		caller2 := upsNode(t, r, "Function", "fn_c2", map[string]any{"name": "caller2"})
		// caller1 -> callee, caller2 -> caller1 -> callee
		r.Link("CALLS", caller1.Node.ID, callee.Node.ID, nil)
		r.Link("CALLS", caller2.Node.ID, caller1.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.HotPaths("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// callee has 2 transitive in-callers (caller1 + caller2)
	found := false
	for _, item := range got {
		if item["transitive_callers"].(int) == 2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected callee to have 2 transitive callers, got %v", got)
	}
}

func TestHotPaths_LimitResults(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		// 5 callees, each with different number of callers.
		for i := 0; i < 5; i++ {
			callee := upsNode(t, r, "Function", "callee"+string(rune('A'+i)), map[string]any{
				"name": "callee" + string(rune('A'+i)),
			})
			caller := upsNode(t, r, "Function", "caller"+string(rune('A'+i)), map[string]any{
				"name": "caller" + string(rune('A'+i)),
			})
			r.Link("CALLS", caller.Node.ID, callee.Node.ID, nil)
		}
	})
	svc := NewAnalysisService(r)
	got, err := svc.HotPaths("p", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results (limit), got %d", len(got))
	}
}

// ---- DeadImports ----

func TestDeadImports_NoFiles(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {})
	svc := NewAnalysisService(r)
	got, err := svc.DeadImports("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
	}
}

func TestDeadImports_EmptyPath(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		f := upsNode(t, r, "SourceFile", "f1", map[string]any{"name": "main.go"})
		f2 := upsNode(t, r, "SourceFile", "f2", map[string]any{"name": "lib"})
		r.Link("IMPORTS", f.Node.ID, f2.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.DeadImports("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results (no path), got %d", len(got))
	}
}

func TestDeadImports_UnreadableFile(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		f := upsNode(t, r, "SourceFile", "f1", map[string]any{"path": "/nonexistent/main.go"})
		f2 := upsNode(t, r, "SourceFile", "f2", map[string]any{"name": "lib"})
		r.Link("IMPORTS", f.Node.ID, f2.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.DeadImports("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results (unreadable), got %d", len(got))
	}
}

func TestDeadImports_FoundDeadImport(t *testing.T) {
	dir := t.TempDir()
	src := "package main\nimport \"lib\"\n"
	path := filepath.Join(dir, "main.go")
	os.WriteFile(path, []byte(src), 0o644)
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		f := upsNode(t, r, "SourceFile", "f1", map[string]any{"path": path})
		f2 := upsNode(t, r, "SourceFile", "f2", map[string]any{"name": "lib"})
		r.Link("IMPORTS", f.Node.ID, f2.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.DeadImports("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, item := range got {
		if item["source"].(string) == path {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dead import to be found, got %v", got)
	}
}

func TestDeadImports_Limit(t *testing.T) {
	dir := t.TempDir()
	// Create a source file that only references itself (count=1)
	srcPath := filepath.Join(dir, "main.go")
	os.WriteFile(srcPath, []byte("package main\n"), 0o644)
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		src := upsNode(t, r, "SourceFile", "src", map[string]any{"path": srcPath})
		for i := 0; i < 5; i++ {
			m := upsNode(t, r, "SourceFile", "m"+string(rune('A'+i)), map[string]any{"name": "mod" + string(rune('A'+i))})
			r.Link("IMPORTS", src.Node.ID, m.Node.ID, nil)
		}
	})
	svc := NewAnalysisService(r)
	got, err := svc.DeadImports("p", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results (limit), got %d", len(got))
	}
}

// ---- ModuleSummary ----

func TestModuleSummary_NoFiles(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {})
	svc := NewAnalysisService(r)
	got, err := svc.ModuleSummary("p", "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["file_count"].(int) != 0 {
		t.Fatalf("expected 0 files, got %v", got["file_count"])
	}
}

func TestModuleSummary_WithFilesAndFilter(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		f1 := upsNode(t, r, "SourceFile", "f1", map[string]any{"path": "/proj/a.go", "language": "go"})
		f2 := upsNode(t, r, "SourceFile", "f2", map[string]any{"path": "/proj/b.py", "language": "python"})
		fn := upsNode(t, r, "Function", "fn1", map[string]any{})
		r.Link("DEFINES", f1.Node.ID, fn.Node.ID, nil)
		r.Link("DEFINES", f2.Node.ID, fn.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.ModuleSummary("p", "a.go", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	files := got["files"].([]map[string]any)
	if len(files) != 1 {
		t.Fatalf("expected 1 file (filtered), got %d", len(files))
	}
	if files[0]["path"].(string) != "/proj/a.go" {
		t.Fatalf("expected a.go, got %v", files[0]["path"])
	}
}

func TestModuleSummary_LimitBreaksEarly(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		for i := 0; i < 5; i++ {
			upsNode(t, r, "SourceFile", "f"+string(rune('A'+i)), map[string]any{
				"path":     "/proj/" + string(rune('A'+i)) + ".go",
				"language": "go",
			})
		}
	})
	svc := NewAnalysisService(r)
	got, err := svc.ModuleSummary("p", "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got["file_count"].(int) != 2 {
		t.Fatalf("expected 2 files (limit), got %v", got["file_count"])
	}
}

// ---- Signature ----

func TestSignature_NoFunctions(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {})
	svc := NewAnalysisService(r)
	got, err := svc.Signature("p", nil, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
	}
}

func TestSignature_WithParameterCountFilter(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "f1", map[string]any{"name": "foo", "parameter_count": 2})
		upsNode(t, r, "Function", "f2", map[string]any{"name": "bar", "parameter_count": 3})
	})
	svc := NewAnalysisService(r)
	pc := 2
	got, err := svc.Signature("p", &pc, "", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
}

func TestSignature_WithLanguageFilter(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "f1", map[string]any{"name": "foo", "parameter_count": 2, "language": "go"})
		upsNode(t, r, "Function", "f2", map[string]any{"name": "bar", "parameter_count": 2, "language": "py"})
	})
	svc := NewAnalysisService(r)
	got, err := svc.Signature("p", nil, "go", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d: %v", len(got), got)
	}
}

func TestSignature_Limit(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		for i := 0; i < 5; i++ {
			upsNode(t, r, "Function", "f"+string(rune('A'+i)), map[string]any{"name": string(rune('A' + i)), "parameter_count": 0})
		}
	})
	svc := NewAnalysisService(r)
	got, err := svc.Signature("p", nil, "", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results (limit), got %d", len(got))
	}
}

// ---- EntryPoints ----

func TestEntryPoints_NoFunctions(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {})
	svc := NewAnalysisService(r)
	got, err := svc.EntryPoints("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
	}
}

func TestEntryPoints_Found(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "f1", map[string]any{"name": "main"})
		upsNode(t, r, "Function", "f2", map[string]any{"name": "init"})
		upsNode(t, r, "Function", "f3", map[string]any{"name": "handler"})
		upsNode(t, r, "Function", "f4", map[string]any{"name": "handle_request"})
		upsNode(t, r, "Function", "f5", map[string]any{"name": "mainloop"})
		upsNode(t, r, "Function", "f6", map[string]any{"name": "on_click"})
		upsNode(t, r, "Function", "f7", map[string]any{"name": "processData"})
	})
	svc := NewAnalysisService(r)
	got, err := svc.EntryPoints("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := make([]string, 0, len(got))
	for _, item := range got {
		names = append(names, item["name"].(string))
	}
	sort.Strings(names)
	expected := []string{"handle_request", "handler", "init", "main", "mainloop", "on_click"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("expected %v, got %v", expected, names)
	}
}

func TestEntryPoints_Limit(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "f1", map[string]any{"name": "main"})
		upsNode(t, r, "Function", "f2", map[string]any{"name": "handler"})
		upsNode(t, r, "Function", "f3", map[string]any{"name": "init"})
	})
	svc := NewAnalysisService(r)
	got, err := svc.EntryPoints("p", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results (limit), got %d", len(got))
	}
}

// ---- RelatedTests ----

func TestRelatedTests_NoFunctions(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {})
	svc := NewAnalysisService(r)
	got, err := svc.RelatedTests("p", "foo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
	}
}

func TestRelatedTests_WithProject(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Project", "proj", map[string]any{"id": "p", "path": "/proj"})
		upsNode(t, r, "Function", "f1", map[string]any{
			"name": "test_foo", "path": "/proj/foo_test.go",
		})
		upsNode(t, r, "Function", "f2", map[string]any{
			"name": "bar", "path": "/proj/bar.go",
		})
	})
	svc := NewAnalysisService(r)
	got, err := svc.RelatedTests("p", "foo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, item := range got {
		if item["name"].(string) == "test_foo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected test_foo in results, got %v", got)
	}
}

func TestRelatedTests_NoProject(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "f1", map[string]any{
			"name": "test_foo", "path": "/proj/foo_test.go",
		})
	})
	svc := NewAnalysisService(r)
	got, err := svc.RelatedTests("p", "foo", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, item := range got {
		if item["name"].(string) == "test_foo" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected test_foo in results even without project, got %v", got)
	}
}

// ---- isTestPath ----

func TestIsTestPath(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		{"go_test", "foo_test.go", true},
		{"go_test_in_dir", "pkg/foo_test.go", true},
		{"py_test", "foo_test.py", true},
		{"py_test_prefix", "test_foo.py", true},
		{"dot_test", "foo.test.js", true},
		{"dot_spec", "foo.spec.js", true},
		{"java_test_suffix", "FooTest.java", true},
		{"java_tests_suffix", "FooTests.java", true},
		{"java_test_prefix", "TestFoo.java", true},
		{"java_test_in_name", "TestFoo.java", true},
		{"rs_test", "foo_test.rs", true},
		{"cc_test", "foo_test.cc", true},
		{"cpp_test", "foo_test.cpp", true},
		{"c_test", "foo_test.c", true},
		{"test_dir", "tests/foo.go", true},
		{"test_dir_single", "test/foo.go", true},
		{"spec_dir", "spec/foo.go", true},
		{"__tests__dir", "__tests__/foo.go", true},
		{"regular", "pkg/foo.go", false},
		{"regular_py", "pkg/foo.py", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isTestPath(c.path)
			if got != c.want {
				t.Errorf("isTestPath(%q) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

func TestRelatedTests_Limit(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Project", "proj", map[string]any{"id": "p", "path": "/proj"})
		for i := 0; i < 5; i++ {
			upsNode(t, r, "Function", "f"+string(rune('A'+i)), map[string]any{
				"name": "test_func" + string(rune('A'+i)),
				"path": "/proj/f" + string(rune('A'+i)) + "_test.go",
			})
		}
	})
	svc := NewAnalysisService(r)
	got, err := svc.RelatedTests("p", "func", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 results (limit), got %d", len(got))
	}
}

func TestRelatedTests_NamePrefixMatch(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		upsNode(t, r, "Function", "f1", map[string]any{
			"name": "test_myfeature", "path": "/proj/foo_test.go",
		})
	})
	svc := NewAnalysisService(r)
	// "myfeature" should match "test_myfeature"
	got, err := svc.RelatedTests("p", "myfeature", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, item := range got {
		if item["name"].(string) == "test_myfeature" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected test_myfeature in results for 'myfeature' query, got %v", got)
	}
}

// ---- Mock repository for error paths ----

type errGraphRepository struct {
	findErr     error
	neighborsOK bool
}

func (r *errGraphRepository) UpsertNode(string, map[string]any, map[string]any) (*models.Node, error) {
	return nil, nil
}
func (r *errGraphRepository) GetNode(string) (*models.Node, error) {
	return nil, nil
}
func (r *errGraphRepository) FindNodes(string, map[string]any) ([]*models.Node, error) {
	if r.findErr != nil {
		return nil, r.findErr
	}
	return nil, nil
}
func (r *errGraphRepository) Link(string, string, string, map[string]any) (*models.Edge, error) {
	return nil, nil
}
func (r *errGraphRepository) Neighbors(string, string, graph.Direction) ([]graph.EdgeNode, error) {
	if r.neighborsOK {
		return nil, nil
	}
	return nil, graph.ErrNotFound
}
func (r *errGraphRepository) RemoveNodes([]string) error    { return nil }
func (r *errGraphRepository) RemoveEdges([]string) error    { return nil }
func (r *errGraphRepository) QueryReadonly(string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}
func (r *errGraphRepository) Close() error { return nil }
func (r *errGraphRepository) UpsertNodesBatch(kind string, items []graph.NodeBatchItem) ([]*models.Node, error) {
	return nil, nil
}
func (r *errGraphRepository) LinkBatch(items []graph.EdgeBatchItem) ([]*models.Edge, error) {
	return nil, nil
}

func TestCircularDependencies_GraphError(t *testing.T) {
	svc := NewAnalysisService(&errGraphRepository{findErr: errFoo3})
	got, err := svc.CircularDependencies("p", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestHotPaths_GraphError(t *testing.T) {
	svc := NewAnalysisService(&errGraphRepository{findErr: errFoo3})
	got, err := svc.HotPaths("p", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestDeadImports_GraphError(t *testing.T) {
	svc := NewAnalysisService(&errGraphRepository{findErr: errFoo3})
	got, err := svc.DeadImports("p", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestModuleSummary_GraphError(t *testing.T) {
	svc := NewAnalysisService(&errGraphRepository{findErr: errFoo3})
	got, err := svc.ModuleSummary("p", "", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestSignature_GraphError(t *testing.T) {
	svc := NewAnalysisService(&errGraphRepository{findErr: errFoo3})
	got, err := svc.Signature("p", nil, "", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestEntryPoints_GraphError(t *testing.T) {
	svc := NewAnalysisService(&errGraphRepository{findErr: errFoo3})
	got, err := svc.EntryPoints("p", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestRelatedTests_GraphError(t *testing.T) {
	svc := NewAnalysisService(&errGraphRepository{findErr: errFoo3})
	got, err := svc.RelatedTests("p", "foo", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestCircularDependencies_NodeWithoutPath(t *testing.T) {
	r := setupAnalysisRepo(t, func(r graph.GraphRepository) {
		na := upsNode(t, r, "SourceFile", "a", map[string]any{})
		nb := upsNode(t, r, "SourceFile", "b", map[string]any{})
		r.Link("IMPORTS", na.Node.ID, nb.Node.ID, nil)
		r.Link("IMPORTS", nb.Node.ID, na.Node.ID, nil)
	})
	svc := NewAnalysisService(r)
	got, err := svc.CircularDependencies("p", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cycle, got %d: %v", len(got), got)
	}
}

var errFoo3 error = &ServiceError{Message: "graph error"}
