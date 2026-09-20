package cache

import (
	"fmt"
	"testing"

	"codergag/internal/graph"
)

func TestSemanticCacheMaxEntries(t *testing.T) {
	cache := NewSemanticCache(SemanticConfig{
		Enabled:    true,
		Threshold:  0,
		MaxEntries: 3,
	})
	for i := 0; i < 5; i++ {
		cache.Index(&CacheEntry{ProjectID: "p"}, fmt.Sprintf("query %d", i))
	}
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	if len(cache.entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(cache.entries))
	}
}

func TestSemanticCacheDefaultMaxEntries(t *testing.T) {
	cache := NewSemanticCache(SemanticConfig{Enabled: true})
	for i := 0; i < 10001; i++ {
		cache.Index(&CacheEntry{ProjectID: "p"}, fmt.Sprintf("query %d", i))
	}
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	if len(cache.entries) != 10000 {
		t.Fatalf("entries = %d, want 10000", len(cache.entries))
	}
}

func TestExactCacheMaxEntries(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Exact.MaxEntries = 3
	manager := NewCacheManager(graph.NewMemoryGraphRepository(), cfg)
	for i := 0; i < 5; i++ {
		if err := manager.StoreExactCache("p", "", "", "", "tool", map[string]any{"n": i}, map[string]any{"value": i}, 1, "test"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := manager.graph.FindNodes("CacheEntry", map[string]any{"project_id": "p"})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("exact cache entries = %d, want 3", len(entries))
	}
}
