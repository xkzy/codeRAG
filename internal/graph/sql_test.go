//go:build postgresql || mongodb

package graph

import (
	"context"
	"os"
	"testing"

	"codergag/internal/models"
)

func TestSQLGraphRepository_NotConfigured(t *testing.T) {
	// This is a smoke test — actual SQL tests require a running PostgreSQL instance.
	if os.Getenv("CODERAG_TEST_PG_DSN") == "" {
		t.Skip("set CODERAG_TEST_PG_DSN to test PostgreSQL backend")
	}
	dsn := os.Getenv("CODERAG_TEST_PG_DSN")
	ctx := context.Background()
	repo, err := NewSQLGraphRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	runGraphRepositoryTests(t, repo)
}

func runGraphRepositoryTests(t *testing.T, r GraphRepository) {
	t.Helper()

	// Upsert a node
	node, err := r.UpsertNode("TestNode", map[string]any{
		"project_id": "test-project",
		"name":       "test_func",
	}, map[string]any{
		"type": "function",
	})
	if err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	// Get the node
	got, err := r.GetNode(node.ID)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Kind != "TestNode" {
		t.Errorf("expected kind TestNode, got %s", got.Kind)
	}
	if got.Properties["name"] != "test_func" {
		t.Errorf("expected name test_func, got %v", got.Properties["name"])
	}

	// FindNodes
	nodes, err := r.FindNodes("TestNode", map[string]any{"project_id": "test-project"})
	if err != nil {
		t.Fatalf("FindNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	// Create a second node and link them
	node2, err := r.UpsertNode("TestNode", map[string]any{
		"project_id": "test-project",
		"name":       "test_func2",
	}, map[string]any{
		"type": "function",
	})
	if err != nil {
		t.Fatalf("UpsertNode 2: %v", err)
	}

	edge, err := r.Link("CALLS", node.ID, node2.ID, map[string]any{"weight": 1.0})
	if err != nil {
		t.Fatalf("Link: %v", err)
	}
	if edge.Kind != "CALLS" {
		t.Errorf("expected edge kind CALLS, got %s", edge.Kind)
	}

	// Find neighbors
	neighbors, err := r.Neighbors(node.ID, "CALLS", DirOut)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("expected 1 neighbor, got %d", len(neighbors))
	}
	if neighbors[0].Node.Properties["name"] != "test_func2" {
		t.Errorf("expected neighbor test_func2, got %v", neighbors[0].Node.Properties["name"])
	}

	// Test reverse direction
	inNeighbors, err := r.Neighbors(node2.ID, "CALLS", DirIn)
	if err != nil {
		t.Fatalf("Neighbors (in): %v", err)
	}
	if len(inNeighbors) != 1 {
		t.Fatalf("expected 1 inbound neighbor, got %d", len(inNeighbors))
	}

	// Remove a node
	if err := r.RemoveNodes([]string{node.ID}); err != nil {
		t.Fatalf("RemoveNodes: %v", err)
	}
	_, err = r.GetNode(node.ID)
	if err == nil {
		t.Fatal("expected error after removal")
	}

	// Remove edge
	if err := r.RemoveEdges([]string{edge.ID}); err != nil {
		t.Fatalf("RemoveEdges: %v", err)
	}

	// Clean up
	r.RemoveNodes([]string{node2.ID})
}

func TestSQLGraphRepository_Suite(t *testing.T) {
	if os.Getenv("CODERAG_TEST_PG_DSN") == "" {
		t.Skip("set CODERAG_TEST_PG_DSN to test PostgreSQL backend")
	}
}

func TestMongoGraphRepository_NotConfigured(t *testing.T) {
	if os.Getenv("CODERAG_TEST_MONGO_DSN") == "" {
		t.Skip("set CODERAG_TEST_MONGO_DSN to test MongoDB backend")
	}
	dsn := os.Getenv("CODERAG_TEST_MONGO_DSN")
	ctx := context.Background()
	repo, err := NewMongoGraphRepository(ctx, dsn, "test-project")
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	runGraphRepositoryTests(t, repo)
}

func TestSQLGraphRepository_QueryReadonly(t *testing.T) {
	if os.Getenv("CODERAG_TEST_PG_DSN") == "" {
		t.Skip("set CODERAG_TEST_PG_DSN to test PostgreSQL backend")
	}
	dsn := os.Getenv("CODERAG_TEST_PG_DSN")
	ctx := context.Background()
	repo, err := NewSQLGraphRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()

	// Create a node to query
	node, err := repo.UpsertNode("TestNode", map[string]any{
		"project_id": "qr-project",
		"name":       "queryable",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	rows, err := repo.QueryReadonly("SELECT id, kind FROM graph_nodes WHERE project_id = $1", map[string]any{"project_id": "qr-project"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Error("expected at least 1 row")
	}

	repo.RemoveNodes([]string{node.ID})
}

func TestMongoGraphRepository_ConnectionError(t *testing.T) {
	ctx := context.Background()
	_, err := NewMongoGraphRepository(ctx, "mongodb://127.0.0.1:1", "test")
	if err == nil {
		t.Fatal("expected connection error for unreachable mongo")
	}
}

func TestSQLGraphRepository_ConnectionError(t *testing.T) {
	ctx := context.Background()
	_, err := NewSQLGraphRepository(ctx, "postgres://invalid:@notreal:5432/bad?sslmode=disable")
	if err == nil {
		t.Fatal("expected connection error for unreachable postgres")
	}
}

func TestModelsNodeEdge(t *testing.T) {
	// Verify models.NewNode and NewEdge work for repository tests
	node := models.NewNode("TestKind", map[string]any{"project_id": "p"}, map[string]any{"name": "test"})
	if node.ID == "" {
		t.Error("expected non-empty ID")
	}
	if node.Properties["name"] != "test" {
		t.Error("missing property")
	}

	edge := models.NewEdge("TEST_EDGE", "from", "to", map[string]any{"weight": 1.0})
	if edge.FromID != "from" || edge.ToID != "to" {
		t.Error("edge endpoints mismatch")
	}
}
