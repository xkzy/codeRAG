package graph

import (
	"bytes"
	"encoding/gob"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeState(t *testing.T, path string, state any) {
	t.Helper()
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(state); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMigratesUnversionedFileAndDropsLegacyTypeNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.gob")
	// A pre-versioning file: same shape as repoState but without SchemaVersion.
	type oldState struct {
		Nodes map[string]gobNode
		Edges map[string]gobEdge
	}
	writeState(t, path, oldState{
		Nodes: map[string]gobNode{
			"t1": {Kind: "Type", Properties: map[string]any{"id": "t1", "project_id": "p", "name": "Legacy"}},
			"f1": {Kind: "Function", Properties: map[string]any{"id": "f1", "project_id": "p", "name": "keep"}},
		},
		Edges: map[string]gobEdge{},
	})

	repo, err := NewPersistentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.FindNodes("Type", nil); len(n) != 0 {
		t.Fatalf("legacy Type nodes should be migrated away, got %d", len(n))
	}
	if n, _ := repo.FindNodes("Function", nil); len(n) != 1 {
		t.Fatalf("other nodes must survive migration, got %d", len(n))
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	// The migrated file is saved at the current version and loads without re-running migrations.
	data, _ := os.ReadFile(path)
	var state repoState
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&state); err != nil {
		t.Fatal(err)
	}
	if state.SchemaVersion != SchemaVersion {
		t.Fatalf("saved schema version = %d, want %d", state.SchemaVersion, SchemaVersion)
	}
}

func TestRejectsFileFromNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.gob")
	writeState(t, path, repoState{SchemaVersion: SchemaVersion + 1, Nodes: map[string]gobNode{}, Edges: map[string]gobEdge{}})
	_, err := NewPersistentRepository(path)
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected newer-schema error, got %v", err)
	}
}

func TestNewFileIsWrittenAtCurrentVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.gob")
	repo, _ := NewPersistentRepository(path)
	repo.UpsertNode("Function", map[string]any{"project_id": "p", "qualified_name": "a"}, nil)
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := NewPersistentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if n, _ := again.FindNodes("Function", nil); len(n) != 1 {
		t.Fatalf("round trip lost node: %d", len(n))
	}
}

func openTwo(t *testing.T) (a, b *PersistentGraphRepository, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "shared.gob")
	// Both open the same (empty) file, so each starts with a stale view of the other's work.
	a, err := NewPersistentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err = NewPersistentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	return a, b, path
}

func TestConcurrentWritersKeepBothSetsOfChanges(t *testing.T) {
	a, b, path := openTwo(t)
	a.UpsertNode("Memory", map[string]any{"project_id": "p", "title": "from-a"}, map[string]any{"content": "a"})
	b.UpsertNode("Memory", map[string]any{"project_id": "p", "title": "from-b"}, map[string]any{"content": "b"})
	if err := a.Save(); err != nil {
		t.Fatal(err)
	}
	if err := b.Save(); err != nil { // must merge with a's write, not overwrite it
		t.Fatal(err)
	}
	fresh, _ := NewPersistentRepository(path)
	if n, _ := fresh.FindNodes("Memory", nil); len(n) != 2 {
		t.Fatalf("expected both writers' nodes, got %d", len(n))
	}
	// b adopted a's node when it saved.
	if n, _ := b.FindNodes("Memory", map[string]any{"title": "from-a"}); len(n) != 1 {
		t.Fatal("saving should adopt the other process's changes")
	}
}

func TestSameLogicalNodeCreatedTwiceIsMergedNotDuplicated(t *testing.T) {
	a, b, path := openTwo(t)
	ident := map[string]any{"project_id": "p", "qualified_name": "f.go:Run"}
	na, _ := a.UpsertNode("Function", ident, map[string]any{"name": "Run", "by": "a"})
	nb, _ := b.UpsertNode("Function", ident, map[string]any{"name": "Run", "by": "b"})
	helperA, _ := a.UpsertNode("Function", map[string]any{"project_id": "p", "qualified_name": "f.go:Helper"}, nil)
	helperB, _ := b.UpsertNode("Function", map[string]any{"project_id": "p", "qualified_name": "f.go:Helper"}, nil)
	a.Link("CALLS", na.ID, helperA.ID, map[string]any{"source": "tree-sitter"})
	b.Link("CALLS", nb.ID, helperB.ID, map[string]any{"source": "tree-sitter"})
	if na.ID == nb.ID {
		t.Fatal("test setup: independent creation should give different IDs")
	}
	a.Save()
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	fresh, _ := NewPersistentRepository(path)
	fns, _ := fresh.FindNodes("Function", nil)
	if len(fns) != 2 {
		t.Fatalf("expected 2 functions (deduplicated by identity), got %d", len(fns))
	}
	run, _ := fresh.FindNodes("Function", map[string]any{"name": "Run"})
	calls, _ := fresh.Neighbors(run[0].ID, "CALLS", DirOut)
	if len(calls) != 1 {
		t.Fatalf("expected exactly one CALLS edge after merge, got %d", len(calls))
	}
	if run[0].Properties["by"] != "b" {
		t.Fatalf("last writer wins per node, got by=%v", run[0].Properties["by"])
	}
}

func TestDeletionsAndRefreshAcrossProcesses(t *testing.T) {
	a, b, _ := openTwo(t)
	n, _ := a.UpsertNode("Function", map[string]any{"project_id": "p", "qualified_name": "gone"}, nil)
	keep, _ := a.UpsertNode("Function", map[string]any{"project_id": "p", "qualified_name": "keep"}, nil)
	a.Link("CALLS", n.ID, keep.ID, nil)
	a.Save()

	if err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	if got, _ := b.FindNodes("Function", nil); len(got) != 2 {
		t.Fatalf("refresh should load a's nodes, got %d", len(got))
	}
	// b deletes a node, a adds one; both saves must survive.
	b.RemoveNodes([]string{n.ID})
	a.UpsertNode("Function", map[string]any{"project_id": "p", "qualified_name": "added"}, nil)
	b.Save()
	a.Save()
	if err := b.Refresh(); err != nil {
		t.Fatal(err)
	}
	got, _ := b.FindNodes("Function", nil)
	names := map[string]bool{}
	for _, g := range got {
		names[g.Properties["qualified_name"].(string)] = true
	}
	if names["gone"] || !names["keep"] || !names["added"] {
		t.Fatalf("unexpected merged set: %v", names)
	}
}

func TestMigratesToRelativeStableIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.gob")
	writeState(t, path, repoState{SchemaVersion: 1, Edges: map[string]gobEdge{}, Nodes: map[string]gobNode{
		"p": {Kind: "Project", Properties: map[string]any{"id": "p", "project_id": "p", "path": "/work/proj"}},
		"f1": {Kind: "Function", Properties: map[string]any{"id": "f1", "project_id": "p", "name": "run", "owner": "Svc",
			"path": "/work/proj/src/a.go", "qualified_name": "/work/proj/src/a.go:Svc.run"}},
		"s1": {Kind: "SourceFile", Properties: map[string]any{"id": "s1", "project_id": "p", "path": "/work/proj/src/a.go"}},
		"b1": {Kind: "BinaryFunction", Properties: map[string]any{"id": "b1", "project_id": "p", "binary_id": "fw", "address": "0x10"}},
	}})
	repo, err := NewPersistentRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	get := func(id string) map[string]any { n, _ := repo.GetNode(id); return n.Properties }
	if p := get("f1"); p["stable_id"] != "func:src/a.go:Svc.run" || p["qualified_name"] != "src/a.go:Svc.run" || p["rel_path"] != "src/a.go" {
		t.Fatalf("function: %v", p)
	}
	if p := get("s1"); p["stable_id"] != "file:src/a.go" {
		t.Fatalf("file: %v", p)
	}
	if p := get("b1"); p["stable_id"] != "binfunc:fw:0x10" {
		t.Fatalf("binary function: %v", p)
	}
}
