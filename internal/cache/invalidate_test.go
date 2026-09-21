package cache

import (
	"testing"

	"codergag/internal/graph"
)

// ── Invalidation ──────────────────────────────────────────────────────────────

func TestInvalidate(t *testing.T) {
	cm := newManager(t)
	args := map[string]any{"f": "main.go"}
	cm.StoreExactCache("p", "r", "abc", "", "tool", args, map[string]any{"v": 1.0}, 1.0, "a")

	// Hit exists
	e, _ := cm.CheckExactCache("p", "r", "abc", "", "tool", args)
	if e == nil {
		t.Fatal("expected hit before invalidate")
	}

	err := cm.Invalidate(InvalidateArgs{ProjectID: "p", ToolName: "tool"})
	if err != nil {
		t.Fatal(err)
	}

	// After invalidate, miss
	e2, _ := cm.CheckExactCache("p", "r", "abc", "", "tool", args)
	if e2 != nil {
		t.Fatal("expected miss after invalidate")
	}
}

func TestInvalidateByCommit(t *testing.T) {
	cm := newManager(t)
	args := map[string]any{"x": "1"}
	cm.StoreExactCache("p", "r", "commitX", "", "tool", args, map[string]any{}, 1.0, "a")
	if err := cm.InvalidateByCommit("p", "commitX"); err != nil {
		t.Fatal(err)
	}
	e, _ := cm.CheckExactCache("p", "r", "commitX", "", "tool", args)
	if e != nil {
		t.Fatal("expected miss after commit invalidation")
	}
}

func TestInvalidateByBinaryHash(t *testing.T) {
	cm := newManager(t)
	args := map[string]any{"x": "1"}
	cm.StoreExactCache("p", "r", "", "hashABC", "tool", args, map[string]any{}, 1.0, "a")
	if err := cm.InvalidateByBinaryHash("p", "hashABC"); err != nil {
		t.Fatal(err)
	}
	e, _ := cm.CheckExactCache("p", "r", "", "hashABC", "tool", args)
	if e != nil {
		t.Fatal("expected miss after binary hash invalidation")
	}
}

func TestExactLimit(t *testing.T) {
	cm := newManager(t)
	limit := cm.ExactLimit()
	if limit != 100 {
		t.Fatalf("ExactLimit: got %d, want 100", limit)
	}
}

// ── TTL / isFresh ─────────────────────────────────────────────────────────────

func TestTTLExpiry(t *testing.T) {
	g := graph.NewMemoryGraphRepository()
	cfg := CacheConfig{
		Enabled: true,
		Exact:   LevelConfig{Enabled: true, MaxEntries: 100},
		TTL:     TTLConfig{Enabled: true, Seconds: 0}, // 0s = immediately stale
	}
	cm := NewCacheManager(g, cfg)
	args := map[string]any{"k": "v"}
	cm.StoreExactCache("p", "r", "c", "", "t", args, map[string]any{"ok": true}, 1.0, "a")

	// With 0-second TTL, entry should be stale
	e, _ := cm.CheckExactCache("p", "r", "c", "", "t", args)
	// May be nil (stale evicted) or stale entry
	if e != nil && e.Freshness != FreshnessStale {
		t.Fatalf("expected nil or stale with 0s TTL, got: %+v", e)
	}
}

func TestTTLEnabled(t *testing.T) {
	g := graph.NewMemoryGraphRepository()
	cfg := CacheConfig{
		Enabled: true,
		Exact:   LevelConfig{Enabled: true, MaxEntries: 100},
		TTL:     TTLConfig{Enabled: true, Seconds: 3600},
	}
	cm := NewCacheManager(g, cfg)
	if !cm.Config().TTL.Enabled {
		t.Fatal("TTL should be enabled")
	}
	if cm.Config().TTL.Seconds != 3600 {
		t.Fatalf("TTL seconds: %d", cm.Config().TTL.Seconds)
	}
}

// ── SemanticCache ─────────────────────────────────────────────────────────────

func TestSemanticCacheSearchAndClear(t *testing.T) {
	cfg := SemanticConfig{
		Enabled:    true,
		Threshold:  0.0, // accept everything
		MaxResults: 5,
		MaxEntries: 100,
	}
	sc := NewSemanticCache(cfg)

	// Index an entry
	entry := &CacheEntry{
		ProjectID: "p",
		ToolName:  "search",
		Result:    map[string]any{"hits": 1.0},
		Freshness: FreshnessValid,
	}
	sc.Index(entry, "find the parser")

	// Search with overlapping query
	result := sc.Search("parser function", "p", "", "", "", 5)
	if result == nil {
		t.Fatal("expected semantic hit for overlapping query")
	}

	// Clear and search again
	sc.Clear()
	after := sc.Search("parser function", "p", "", "", "", 5)
	if after != nil {
		t.Fatal("expected miss after Clear()")
	}
}

func TestSemanticCacheProjectIsolation(t *testing.T) {
	cfg := SemanticConfig{Enabled: true, Threshold: 0.0, MaxResults: 5, MaxEntries: 100}
	sc := NewSemanticCache(cfg)
	entry := &CacheEntry{ProjectID: "proj-A", ToolName: "t", Result: map[string]any{}, Freshness: FreshnessValid}
	sc.Index(entry, "database connection pooling")

	// Different project should not match
	result := sc.Search("database connection pooling", "proj-B", "", "", "", 5)
	if result != nil {
		t.Fatal("semantic cache must not cross project boundaries")
	}
}

func TestSemanticCacheDisabled(t *testing.T) {
	cfg := SemanticConfig{Enabled: false}
	sc := NewSemanticCache(cfg)
	entry := &CacheEntry{ProjectID: "p", ToolName: "t", Result: map[string]any{}, Freshness: FreshnessValid}
	sc.Index(entry, "test query")
	result := sc.Search("test query", "p", "", "", "", 5)
	if result != nil {
		t.Fatal("disabled semantic cache should return nil")
	}
}

func TestSemanticCacheEmptyEmbedding(t *testing.T) {
	cfg := SemanticConfig{Enabled: true, Threshold: 0.0, MaxResults: 5, MaxEntries: 100}
	sc := NewSemanticCache(cfg)
	entry := &CacheEntry{ProjectID: "p", ToolName: "t", Result: map[string]any{}, Freshness: FreshnessValid, Embedding: []float64{}}
	sc.Index(entry, "test")
	result := sc.Search("test", "p", "", "", "", 5)
	if result != nil {
		t.Fatal("empty embedding should not match")
	}
}

// ── JobRegistry ───────────────────────────────────────────────────────────────

func TestJobRegistryAcquireReleaseGet(t *testing.T) {
	jr := NewJobRegistry()

	key := "proj:tool:abc"
	job1 := jr.Acquire(key)
	if job1 == nil {
		t.Fatal("first acquire should return job")
	}
	if job1.Done == nil {
		t.Fatal("job should have Done channel")
	}

	// Second acquire with same key returns same job
	job2 := jr.Acquire(key)
	if job2 != job1 {
		t.Fatal("should return same job")
	}

	// Get returns the job
	got := jr.Get(key)
	if got != job1 {
		t.Fatal("Get should return the same job")
	}

	jr.Release(key)

	// After release, next acquire returns new job
	job3 := jr.Acquire(key)
	if job3 == job1 {
		t.Fatal("after release, acquire should return new job")
	}
	_ = job3
}

func TestJobRegistryGetMissing(t *testing.T) {
	jr := NewJobRegistry()
	got := jr.Get("nonexistent")
	if got != nil {
		t.Fatal("Get missing key should return nil")
	}
}

func TestJobRegistryConcurrentAcquire(t *testing.T) {
	jr := NewJobRegistry()
	key := "concurrent:test"
	done := make(chan *Job, 10)

	// Multiple goroutines acquire same key
	for i := 0; i < 10; i++ {
		go func() {
			done <- jr.Acquire(key)
		}()
	}

	var first *Job
	for i := 0; i < 10; i++ {
		job := <-done
		if first == nil {
			first = job
		}
		if job != first {
			t.Fatal("all concurrent acquires should return same job")
		}
	}
}