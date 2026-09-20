package cache

import (
	"testing"

	"codergag/internal/graph"
)

func newManager(t *testing.T) *CacheManager {
	t.Helper()
	g := graph.NewMemoryGraphRepository()
	cfg := CacheConfig{
		Enabled: true,
		Exact:   LevelConfig{Enabled: true, MaxEntries: 100},
		TTL:     TTLConfig{Enabled: false},
	}
	return NewCacheManager(g, cfg)
}

func TestStatsAndIncrementStat(t *testing.T) {
	cm := newManager(t)
	fields := []string{
		"exact_hits", "semantic_hits", "tool_hits", "analysis_hits",
		"graph_hits", "partial_hits", "cache_misses", "stale_hits",
		"invalidations", "llm_calls_saved", "tool_calls_saved",
	}
	for _, f := range fields {
		cm.IncrementStat(f)
	}
	s := cm.Stats()
	if s.ExactHits != 1 || s.SemanticHits != 1 || s.CacheMisses != 1 {
		t.Fatalf("stat increment failed: %+v", s)
	}
	if s.ToolHits != 1 || s.AnalysisHits != 1 || s.GraphHits != 1 {
		t.Fatalf("stat increment failed: %+v", s)
	}
	if s.PartialHits != 1 || s.StaleHits != 1 || s.Invalidations != 1 {
		t.Fatalf("stat increment failed: %+v", s)
	}
	if s.LLMCallsSaved != 1 || s.ToolCallsSaved != 1 {
		t.Fatalf("stat increment failed: %+v", s)
	}
	// Unknown field is a no-op
	cm.IncrementStat("unknown_field")
}

func TestConfigAndSetTTL(t *testing.T) {
	cm := newManager(t)
	cfg := cm.Config()
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.TTL.Enabled {
		t.Fatal("TTL should start disabled")
	}
	cm.SetTTL(true, 300)
	cfg2 := cm.Config()
	if !cfg2.TTL.Enabled || cfg2.TTL.Seconds != 300 {
		t.Fatalf("SetTTL: %+v", cfg2.TTL)
	}
}

func TestStoreAndCheckExactCache(t *testing.T) {
	cm := newManager(t)
	args := map[string]any{"file": "main.go"}
	result := map[string]any{"symbols": 42.0}

	// Miss first
	entry, err := cm.CheckExactCache("proj", "repo", "abc123", "", "list_symbols", args)
	if err != nil || entry != nil {
		t.Fatalf("expected miss, got %v %v", entry, err)
	}

	// Store
	if err := cm.StoreExactCache("proj", "repo", "abc123", "", "list_symbols", args, result, 1.0, "agent"); err != nil {
		t.Fatal(err)
	}

	// Hit
	entry2, err := cm.CheckExactCache("proj", "repo", "abc123", "", "list_symbols", args)
	if err != nil || entry2 == nil {
		t.Fatalf("expected hit, got %v %v", entry2, err)
	}
	if v, ok := entry2.Result["symbols"]; !ok || v.(float64) != 42.0 {
		t.Fatalf("result mismatch: %v", entry2.Result)
	}
}

func TestExactCacheKeyIsolation(t *testing.T) {
	cm := newManager(t)
	args := map[string]any{"x": "1"}
	cm.StoreExactCache("proj", "repo", "commitA", "", "tool", args, map[string]any{"v": 1.0}, 1.0, "a")

	// Different commit → miss
	entry, err := cm.CheckExactCache("proj", "repo", "commitB", "", "tool", args)
	if err != nil || entry != nil {
		t.Fatalf("different commit should be a miss: %v %v", entry, err)
	}

	// Different project → miss
	entry2, err := cm.CheckExactCache("other", "repo", "commitA", "", "tool", args)
	if err != nil || entry2 != nil {
		t.Fatalf("different project should be a miss: %v %v", entry2, err)
	}
}

func TestCheckCacheMultiLevel(t *testing.T) {
	cm := newManager(t)
	args := map[string]any{"query": "parse function"}

	// Miss
	r, err := cm.CheckCache("proj", "repo", "abc", "", "search", args)
	if err != nil || r.Status != CacheStatusMiss {
		t.Fatalf("expected MISS, got %v %v", r, err)
	}

	// Store via StoreCache (delegates to StoreExactCache)
	if err := cm.StoreCache("proj", "repo", "abc", "", "search", args, map[string]any{"hits": 1.0}, 0.9, "agent"); err != nil {
		t.Fatal(err)
	}

	// Hit
	r2, err := cm.CheckCache("proj", "repo", "abc", "", "search", args)
	if err != nil || r2.Status != CacheStatusHit {
		t.Fatalf("expected HIT, got %v %v", r2, err)
	}
	if r2.Confidence != 0.9 {
		t.Fatalf("confidence mismatch: %v", r2.Confidence)
	}
}

func TestFlush(t *testing.T) {
	cm := newManager(t)
	// Store 3 distinct entries (different args so different keys)
	for i := 0; i < 3; i++ {
		cm.StoreExactCache("proj", "repo", "abc", "", "tool",
			map[string]any{"i": float64(i)},
			map[string]any{"ok": true}, 1.0, "agent")
	}
	// Store one for a different project
	cm.StoreExactCache("other", "repo", "abc", "", "tool",
		map[string]any{"i": 0.0},
		map[string]any{"ok": true}, 1.0, "agent")

	n, err := cm.Flush("proj")
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected flushed > 0")
	}

	// After flush, proj entries are gone
	entry, _ := cm.CheckExactCache("proj", "repo", "abc", "", "tool", map[string]any{"i": 0.0})
	if entry != nil {
		t.Fatal("expected miss after flush")
	}

	// Other project's entry should still be there
	other, _ := cm.CheckExactCache("other", "repo", "abc", "", "tool", map[string]any{"i": 0.0})
	if other == nil {
		t.Fatal("other project's entry should survive proj flush")
	}
}

func TestFlushEmpty(t *testing.T) {
	cm := newManager(t)
	n, err := cm.Flush("nonexistent")
	if err != nil || n != 0 {
		t.Fatalf("flush empty: %d %v", n, err)
	}
}

func TestPrivacyPolicyCache(t *testing.T) {
	cm := newManager(t)

	// Empty
	pol, err := cm.GetPrivacyPolicy("proj")
	if err != nil || pol != "" {
		t.Fatalf("expected empty: %v %v", pol, err)
	}

	// Store and retrieve
	if err := cm.StorePrivacyPolicy("proj", `{"mode":"strict"}`); err != nil {
		t.Fatal(err)
	}
	pol2, err := cm.GetPrivacyPolicy("proj")
	if err != nil || pol2 != `{"mode":"strict"}` {
		t.Fatalf("round trip failed: %v %v", pol2, err)
	}

	// Overwrite
	if err := cm.StorePrivacyPolicy("proj", `{"mode":"relaxed"}`); err != nil {
		t.Fatal(err)
	}
	pol3, _ := cm.GetPrivacyPolicy("proj")
	if pol3 != `{"mode":"relaxed"}` {
		t.Fatalf("overwrite failed: %v", pol3)
	}

	// Different project is isolated
	other, _ := cm.GetPrivacyPolicy("other")
	if other != "" {
		t.Fatalf("project isolation failed: %v", other)
	}
}

func TestPseudonymSnapshotCache(t *testing.T) {
	cm := newManager(t)

	// Empty
	snap, err := cm.GetPseudonymSnapshot("proj")
	if err != nil || snap != "" {
		t.Fatalf("expected empty: %v %v", snap, err)
	}

	// Store and retrieve
	if err := cm.StorePseudonymSnapshot("proj", `{"FUNC_1":"realFunc"}`); err != nil {
		t.Fatal(err)
	}
	snap2, err := cm.GetPseudonymSnapshot("proj")
	if err != nil || snap2 != `{"FUNC_1":"realFunc"}` {
		t.Fatalf("round trip failed: %v %v", snap2, err)
	}
}

func TestCacheDisabled(t *testing.T) {
	g := graph.NewMemoryGraphRepository()
	cm := NewCacheManager(g, CacheConfig{Enabled: false})

	// StoreExactCache is a no-op
	if err := cm.StoreExactCache("p", "", "", "", "t", nil, nil, 0, ""); err != nil {
		t.Fatal(err)
	}
	// CheckExactCache returns nil (no error, no entry)
	entry, err := cm.CheckExactCache("p", "", "", "", "t", nil)
	if err != nil || entry != nil {
		t.Fatalf("disabled CheckExactCache: %v %v", entry, err)
	}

	// Privacy methods are no-ops
	if err := cm.StorePrivacyPolicy("p", "json"); err != nil {
		t.Fatal(err)
	}
	pol, _ := cm.GetPrivacyPolicy("p")
	if pol != "" {
		t.Fatalf("disabled: expected empty policy, got %q", pol)
	}

	if err := cm.StorePseudonymSnapshot("p", "json"); err != nil {
		t.Fatal(err)
	}
	snap, _ := cm.GetPseudonymSnapshot("p")
	if snap != "" {
		t.Fatalf("disabled: expected empty snapshot, got %q", snap)
	}

	// CheckCache returns MISS
	r, err := cm.CheckCache("p", "", "", "", "t", nil)
	if err != nil || r.Status != CacheStatusMiss {
		t.Fatalf("disabled: expected MISS, got %v %v", r, err)
	}
}
