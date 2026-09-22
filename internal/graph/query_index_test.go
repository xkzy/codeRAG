package graph

import (
	"testing"
)

func TestGraphQueryIndex_GetNode(t *testing.T) {
	repo := NewMemoryGraphRepository()
	node, err := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:foo", "project_id": "p1"},
		map[string]any{"name": "foo"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	idx := NewGraphQueryIndex(repo, "p1")
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	got, ok := idx.GetNode(node.ID)
	if !ok {
		t.Fatalf("GetNode(%q): expected ok=true, got false", node.ID)
	}
	if got.ID != node.ID {
		t.Errorf("GetNode ID: want %q got %q", node.ID, got.ID)
	}

	_, ok = idx.GetNode("nonexistent-id")
	if ok {
		t.Error("GetNode(nonexistent): expected ok=false, got true")
	}
}

func TestGraphQueryIndex_FindByStableID(t *testing.T) {
	repo := NewMemoryGraphRepository()
	repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:foo", "project_id": "p1"},
		map[string]any{"name": "foo"})

	idx := NewGraphQueryIndex(repo, "p1")
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	nodes, ok := idx.FindByStableID("func:a.go:foo")
	if !ok {
		t.Fatal("FindByStableID: expected ok=true")
	}
	if len(nodes) != 1 {
		t.Fatalf("FindByStableID: expected 1 node, got %d", len(nodes))
	}
	if strValue(nodes[0].Properties["stable_id"]) != "func:a.go:foo" {
		t.Errorf("FindByStableID: wrong stable_id %q", nodes[0].Properties["stable_id"])
	}

	// Miss is still ok=true (index present) with empty slice
	nodes, ok = idx.FindByStableID("func:a.go:notexist")
	if !ok {
		t.Fatal("FindByStableID(miss): expected ok=true")
	}
	if len(nodes) != 0 {
		t.Errorf("FindByStableID(miss): expected 0 nodes, got %d", len(nodes))
	}
}

func TestGraphQueryIndex_FindByName(t *testing.T) {
	repo := NewMemoryGraphRepository()
	repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:Parse", "project_id": "p2"},
		map[string]any{"name": "Parse"})

	idx := NewGraphQueryIndex(repo, "p2")
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	nodes, ok := idx.FindByName("Parse")
	if !ok {
		t.Fatal("FindByName: expected ok=true")
	}
	if len(nodes) != 1 {
		t.Fatalf("FindByName: expected 1 node, got %d", len(nodes))
	}

	nodes, ok = idx.FindByName("NoSuch")
	if !ok {
		t.Fatal("FindByName(miss): expected ok=true")
	}
	if len(nodes) != 0 {
		t.Errorf("FindByName(miss): expected 0 nodes, got %d", len(nodes))
	}
}

func TestGraphQueryIndex_FindByPath(t *testing.T) {
	repo := NewMemoryGraphRepository()
	repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:src/a.go:Foo", "project_id": "p3"},
		map[string]any{"name": "Foo", "rel_path": "src/a.go"})

	idx := NewGraphQueryIndex(repo, "p3")
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	nodes, ok := idx.FindByPath("src/a.go")
	if !ok {
		t.Fatal("FindByPath: expected ok=true")
	}
	if len(nodes) != 1 {
		t.Fatalf("FindByPath: expected 1 node, got %d", len(nodes))
	}

	nodes, ok = idx.FindByPath("src/missing.go")
	if !ok {
		t.Fatal("FindByPath(miss): expected ok=true")
	}
	if len(nodes) != 0 {
		t.Errorf("FindByPath(miss): expected 0 nodes, got %d", len(nodes))
	}
}

func TestGraphQueryIndex_AllNeighbors(t *testing.T) {
	repo := NewMemoryGraphRepository()
	a, _ := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:a", "project_id": "p4"}, nil)
	b, _ := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:b", "project_id": "p4"}, nil)
	c, _ := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:c", "project_id": "p4"}, nil)
	repo.Link("CALLS", a.ID, b.ID, nil)
	repo.Link("DATA_FLOW", a.ID, c.ID, nil)

	idx := NewGraphQueryIndex(repo, "p4")
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}

	// DirOut from a: should see both b (CALLS) and c (DATA_FLOW)
	out, ok := idx.AllNeighbors(a.ID, DirOut)
	if !ok {
		t.Fatal("AllNeighbors DirOut: expected ok=true")
	}
	if len(out) != 2 {
		t.Fatalf("AllNeighbors DirOut: expected 2 neighbors, got %d", len(out))
	}

	// DirIn from b: should see a (CALLS)
	in, ok := idx.AllNeighbors(b.ID, DirIn)
	if !ok {
		t.Fatal("AllNeighbors DirIn: expected ok=true")
	}
	if len(in) != 1 || in[0].Node.ID != a.ID {
		t.Fatalf("AllNeighbors DirIn: expected 1 neighbor (a), got %v", in)
	}

	// DirBoth from a: 2 outgoing + 0 incoming = 2
	both, ok := idx.AllNeighbors(a.ID, DirBoth)
	if !ok {
		t.Fatal("AllNeighbors DirBoth: expected ok=true")
	}
	if len(both) != 2 {
		t.Fatalf("AllNeighbors DirBoth: expected 2, got %d", len(both))
	}
}

func TestGraphQueryIndex_NeighborsWildcard(t *testing.T) {
	repo := NewMemoryGraphRepository()
	a, _ := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:a", "project_id": "wild"}, nil)
	b, _ := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:b", "project_id": "wild"}, nil)
	c, _ := repo.UpsertNode("Function",
		map[string]any{"stable_id": "func:a.go:c", "project_id": "wild"}, nil)
	repo.Link("CALLS", a.ID, b.ID, nil)
	repo.Link("DATA_FLOW", a.ID, c.ID, nil)

	idx := NewGraphQueryIndex(repo, "wild")
	if err := idx.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	got, ok := idx.Neighbors(a.ID, "", DirOut)
	if !ok || len(got) != 2 {
		t.Fatalf("Neighbors wildcard: got %d neighbors, ok=%v", len(got), ok)
	}
}
