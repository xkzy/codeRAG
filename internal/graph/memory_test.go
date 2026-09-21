package graph

import (
	"testing"

	"codergag/internal/models"
)

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
	// GetNode on unknown ID
	missing, err := g.GetNode("__nope__")
	if err != ErrNotFound || missing != nil {
		t.Fatalf("missing should return nil,ErrNotFound: %v %v", missing, err)
	}
}

func TestMemoryLinkAndNeighbors(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	if _, err := g.Link("CALLS", a.ID, b.ID, nil); err != nil {
		t.Fatal(err)
	}
	nbrs, err := g.Neighbors(a.ID, "CALLS", DirOut)
	if err != nil || len(nbrs) != 1 || nbrs[0].Node.ID != b.ID {
		t.Fatalf("neighbors out: %v %v", nbrs, err)
	}
	nbrsIn, err := g.Neighbors(b.ID, "CALLS", DirIn)
	if err != nil || len(nbrsIn) != 1 {
		t.Fatalf("neighbors in: %v %v", nbrsIn, err)
	}
	// Unknown direction
	_, err = g.Neighbors(a.ID, "CALLS", DirBoth)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMemoryEdgesOfKind(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	g.Link("CALLS", a.ID, b.ID, nil)
	g.Link("USES", a.ID, b.ID, nil)
	calls := g.EdgesOfKind("CALLS")
	if len(calls) == 0 {
		t.Fatal("EdgesOfKind: expected calls")
	}
	uses := g.EdgesOfKind("USES")
	if len(uses) == 0 {
		t.Fatal("EdgesOfKind USES: expected uses")
	}
	none := g.EdgesOfKind("NOOP")
	if len(none) != 0 {
		t.Fatalf("EdgesOfKind empty: %v", none)
	}
}

func TestMemoryRemoveEdges(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	edge, _ := g.Link("CALLS", a.ID, b.ID, nil)
	if err := g.RemoveEdges([]string{edge.ID}); err != nil {
		t.Fatal(err)
	}
	nbrs, _ := g.Neighbors(a.ID, "CALLS", DirOut)
	if len(nbrs) != 0 {
		t.Fatal("edge should be removed")
	}
}

func TestMemoryCounts(t *testing.T) {
	g := NewMemoryGraphRepository()
	g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	nc, ec := g.Counts()
	if nc["Function"] < 2 {
		t.Fatalf("Counts: %v %v", nc, ec)
	}
}

func TestMemoryNodeEdgeCount(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "x"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "y"}, nil)
	g.Link("CALLS", a.ID, b.ID, nil)
	if n := g.NodeCount(); n < 2 {
		t.Fatalf("NodeCount: %d", n)
	}
	if e := g.EdgeCount(); e < 1 {
		t.Fatalf("EdgeCount: %d", e)
	}
}

func TestMemoryQueryReadonly(t *testing.T) {
	g := NewMemoryGraphRepository()
	// Memory repository does not support advanced queries; verify the error path.
	_, err := g.QueryReadonly("SELECT * FROM Function", map[string]any{})
	if err == nil {
		t.Fatal("memory repository should reject advanced queries")
	}
}

func TestMemoryClose(t *testing.T) {
	g := NewMemoryGraphRepository()
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryFindNodes(t *testing.T) {
	g := NewMemoryGraphRepository()
	g.UpsertNode("Function", map[string]any{"name": "a", "project_id": "p"}, nil)
	g.UpsertNode("Function", map[string]any{"name": "b", "project_id": "p"}, nil)
	g.UpsertNode("Function", map[string]any{"name": "c", "project_id": "other"}, nil)

	// Filter by project
	results, err := g.FindNodes("Function", map[string]any{"project_id": "p"})
	if err != nil || len(results) != 2 {
		t.Fatalf("FindNodes: %v %v", results, err)
	}

	// Filter by name
	results2, err := g.FindNodes("Function", map[string]any{"name": "a"})
	if err != nil || len(results2) != 1 || results2[0].Properties["name"] != "a" {
		t.Fatalf("FindNodes name: %v %v", results2, err)
	}

	// No match
	none, err := g.FindNodes("Function", map[string]any{"name": "missing"})
	if err != nil || len(none) != 0 {
		t.Fatalf("FindNodes none: %v %v", none, err)
	}

	// Empty kind returns all
	all, err := g.FindNodes("", map[string]any{"project_id": "p"})
	if err != nil || len(all) != 2 {
		t.Fatalf("FindNodes empty kind: %v %v", all, err)
	}
}

func TestMemoryRemoveNodes(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	g.Link("CALLS", a.ID, b.ID, nil)

	if err := g.RemoveNodes([]string{a.ID}); err != nil {
		t.Fatal(err)
	}
	// Node a is gone
	if _, err := g.GetNode(a.ID); err != ErrNotFound {
		t.Fatalf("node should be removed: %v", err)
	}
	// Edge involving a should also be removed
	nbrs, _ := g.Neighbors(b.ID, "CALLS", DirIn)
	if len(nbrs) != 0 {
		t.Fatal("edge should be removed with node")
	}
	// Node b still exists
	if _, err := g.GetNode(b.ID); err != nil {
		t.Fatal("node b should remain")
	}
}

func TestMemoryLinkIdempotent(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	e1, _ := g.Link("CALLS", a.ID, b.ID, map[string]any{})
	e2, _ := g.Link("CALLS", a.ID, b.ID, map[string]any{})
	if e1.ID != e2.ID {
		t.Fatal("duplicate link should return same edge")
	}
	if e := g.EdgeCount(); e != 1 {
		t.Fatalf("EdgeCount: %d, want 1", e)
	}
}

func TestMemoryUpsertMergesProperties(t *testing.T) {
	g := NewMemoryGraphRepository()
	n1, _ := g.UpsertNode("Function", map[string]any{"name": "foo"}, map[string]any{"lang": "go"})
	n2, _ := g.UpsertNode("Function", map[string]any{"name": "foo"}, map[string]any{"version": "1.0"})
	if n1.ID != n2.ID {
		t.Fatal("upsert should merge, not duplicate")
	}
	if n2.Properties["lang"] != "go" {
		t.Fatal("merged node should retain lang")
	}
	if n2.Properties["version"] != "1.0" {
		t.Fatal("merged node should gain version")
	}
}

func TestMemoryUpsertNodeInvalidKind(t *testing.T) {
	g := NewMemoryGraphRepository()
	if _, err := g.UpsertNode("bad-kind!", map[string]any{"name": "x"}, nil); err == nil {
		t.Fatal("upsert with invalid kind should return error")
	}
}

func TestMemoryUpsertNodeMissingIDGeneratesOne(t *testing.T) {
	g := NewMemoryGraphRepository()
	node, err := g.UpsertNode("Function", map[string]any{"name": "foo"}, nil)
	if err != nil || node.ID == "" {
		t.Fatalf("expected auto-generated ID, got err=%v node=%v", err, node)
	}
}

func TestMemoryLinkInvalidKind(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	if _, err := g.Link("bad kind", a.ID, b.ID, nil); err == nil {
		t.Fatal("link with invalid kind should return error")
	}
}

func TestMemoryLinkDuplicateProperties(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	props := map[string]any{"source": "test"}
	e1, _ := g.Link("CALLS", a.ID, b.ID, props)
	e2, _ := g.Link("CALLS", a.ID, b.ID, props)
	if e1.ID != e2.ID {
		t.Fatal("identical link should return same edge")
	}
}

func TestMemoryNeighborsDirBoth(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	g.Link("CALLS", a.ID, b.ID, nil)
	nbrs, err := g.Neighbors(b.ID, "CALLS", DirBoth)
	if err != nil || len(nbrs) != 1 {
		t.Fatalf("DirBoth should find incoming edge to b: %v %v", nbrs, err)
	}
}

func TestMemoryNeighborsDirBothBidirectional(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	g.Link("CALLS", a.ID, b.ID, nil)
	nbrs, err := g.Neighbors(a.ID, "CALLS", DirBoth)
	if err != nil || len(nbrs) != 1 {
		t.Fatalf("DirBoth from a should find outgoing edge: %v %v", nbrs, err)
	}
}

func TestMemoryNeighborsMissingTargetNode(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	g.Link("CALLS", a.ID, "nonexistent", nil)
	nbrs, err := g.Neighbors(a.ID, "CALLS", DirOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(nbrs) != 0 {
		t.Fatalf("missing target node should not appear in neighbors: %v", nbrs)
	}
}

func TestMemoryNeighborsDirInMissingSourceNode(t *testing.T) {
	g := NewMemoryGraphRepository()
	b, _ := g.UpsertNode("Function", map[string]any{"name": "b"}, nil)
	g.Link("CALLS", "nonexistent", b.ID, nil)
	nbrs, err := g.Neighbors(b.ID, "CALLS", DirIn)
	if err != nil {
		t.Fatal(err)
	}
	if len(nbrs) != 0 {
		t.Fatalf("missing source node should not appear in neighbors: %v", nbrs)
	}
}

func TestMemoryNeighborsDirBothMissingNodes(t *testing.T) {
	g := NewMemoryGraphRepository()
	a, _ := g.UpsertNode("Function", map[string]any{"name": "a"}, nil)
	g.Link("CALLS", a.ID, "nonexistent", nil)
	g.Link("CALLS", "nonexistent", a.ID, nil)
	nbrs, err := g.Neighbors(a.ID, "CALLS", DirBoth)
	if err != nil {
		t.Fatal(err)
	}
	if len(nbrs) != 0 {
		t.Fatalf("missing nodes should not appear in DirBoth neighbors: %v", nbrs)
	}
}

func TestPresent(t *testing.T) {
	node := &models.Node{
		ID:   "n1",
		Kind: "Function",
		Properties: map[string]any{
			"name":       "foo",
			"project_id": "p1",
		},
	}
	result := Present(node)
	if result["id"] != "n1" {
		t.Errorf("expected id=n1, got %v", result["id"])
	}
	if result["kind"] != "Function" {
		t.Errorf("expected kind=Function, got %v", result["kind"])
	}
	if result["name"] != "foo" {
		t.Errorf("expected name=foo, got %v", result["name"])
	}
	if result["project_id"] != "p1" {
		t.Errorf("expected project_id=p1, got %v", result["project_id"])
	}
}

func TestPresentNilNode(t *testing.T) {
	if result := Present(nil); result != nil {
		t.Errorf("expected nil for nil node, got %v", result)
	}
}

func TestMemoryCountsEmpty(t *testing.T) {
	g := NewMemoryGraphRepository()
	nc, ec := g.Counts()
	if len(nc) != 0 || len(ec) != 0 {
		t.Fatalf("empty counts: nodes=%v edges=%v", nc, ec)
	}
}

func TestMemoryRemoveNonexistentEdges(t *testing.T) {
	g := NewMemoryGraphRepository()
	if err := g.RemoveEdges([]string{"does-not-exist"}); err != nil {
		t.Fatal(err)
	}
}