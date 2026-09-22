package graph

import (
	"fmt"
	"testing"
)

func buildBenchRepo(b *testing.B, nodeCount int) (GraphRepository, *GraphQueryIndex) {
	b.Helper()
	repo := NewMemoryGraphRepository()
	var previous string
	for i := 0; i < nodeCount; i++ {
		stableID := fmt.Sprintf("func:src/f%d.go:Func%d", i, i)
		node, err := repo.UpsertNode("Function",
			map[string]any{"stable_id": stableID, "project_id": "bench"},
			map[string]any{"name": fmt.Sprintf("Func%d", i), "qualified_name": fmt.Sprintf("pkg.Func%d", i)})
		if err != nil {
			b.Fatal(err)
		}
		if previous != "" {
			if _, err := repo.Link("CALLS", previous, node.ID, nil); err != nil {
				b.Fatal(err)
			}
		}
		previous = node.ID
	}
	idx := NewGraphQueryIndex(repo, "bench")
	if err := idx.Rebuild(); err != nil {
		b.Fatal(err)
	}
	return repo, idx
}

func BenchmarkGetNode_Index(b *testing.B) {
	_, idx := buildBenchRepo(b, 1000)
	nodes := idx.NodesByKind("Function")
	if len(nodes) == 0 {
		b.Skip("no nodes")
	}
	targetID := nodes[0].ID
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.GetNode(targetID)
	}
}

func BenchmarkFindByStableID_Index(b *testing.B) {
	_, idx := buildBenchRepo(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.FindByStableID("func:src/f500.go:Func500")
	}
}

func BenchmarkNodesByKind_Index(b *testing.B) {
	_, idx := buildBenchRepo(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = idx.NodesByKind("Function")
	}
}

func BenchmarkNeighbors_Index(b *testing.B) {
	_, idx := buildBenchRepo(b, 1000)
	nodes := idx.NodesByKind("Function")
	if len(nodes) == 0 {
		b.Skip("no nodes")
	}
	nodeID := nodes[500].ID
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.Neighbors(nodeID, "CALLS", DirOut)
	}
}

func BenchmarkAllNeighbors_Index(b *testing.B) {
	_, idx := buildBenchRepo(b, 1000)
	nodes := idx.NodesByKind("Function")
	if len(nodes) == 0 {
		b.Skip("no nodes")
	}
	nodeID := nodes[500].ID
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = idx.AllNeighbors(nodeID, DirBoth)
	}
}

func BenchmarkFindNodes_Authoritative(b *testing.B) {
	repo, _ := buildBenchRepo(b, 1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = repo.FindNodes("Function", map[string]any{"project_id": "bench", "stable_id": "func:src/f500.go:Func500"})
	}
}
