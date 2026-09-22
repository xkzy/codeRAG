package services_test

import (
	"fmt"
	"testing"

	"codergag/internal/graph"
	"codergag/internal/services"
)

func buildRefBenchRepo(b *testing.B, count int) (graph.GraphRepository, *graph.GraphQueryIndex) {
	b.Helper()
	repo := graph.NewMemoryGraphRepository()
	if _, err := repo.UpsertNode("Project",
		map[string]any{"id": "bp", "project_id": "bp"},
		map[string]any{"path": "/tmp/bp"}); err != nil {
		b.Fatal(err)
	}
	for i := 0; i < count; i++ {
		stableID := fmt.Sprintf("func:src/f%d.go:F%d", i, i)
		if _, err := repo.UpsertNode("Function",
			map[string]any{"stable_id": stableID, "project_id": "bp"},
			map[string]any{"name": fmt.Sprintf("F%d", i), "qualified_name": fmt.Sprintf("pkg.F%d", i)}); err != nil {
			b.Fatal(err)
		}
	}
	idx := graph.NewGraphQueryIndex(repo, "bp")
	if err := idx.Rebuild(); err != nil {
		b.Fatal(err)
	}
	return repo, idx
}

func BenchmarkLookup_Authoritative(b *testing.B) {
	repo, _ := buildRefBenchRepo(b, 1000)
	resolver := services.NewReferenceResolver(repo)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = resolver.Lookup("bp", "func:src/f500.go:F500")
	}
}

func BenchmarkLookup_Index(b *testing.B) {
	repo, idx := buildRefBenchRepo(b, 1000)
	resolver := services.NewReferenceResolverWithIndex(repo, idx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = resolver.Lookup("bp", "func:src/f500.go:F500")
	}
}

func BenchmarkResolveSymbol_Authoritative(b *testing.B) {
	repo, _ := buildRefBenchRepo(b, 1000)
	resolver := services.NewReferenceResolver(repo)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = resolver.ResolveSymbol("bp", "F500")
	}
}

func BenchmarkResolveSymbol_Index(b *testing.B) {
	repo, idx := buildRefBenchRepo(b, 1000)
	resolver := services.NewReferenceResolverWithIndex(repo, idx)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = resolver.ResolveSymbol("bp", "F500")
	}
}
