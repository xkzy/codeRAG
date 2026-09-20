package services

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"codergag/internal/graph"
	"codergag/internal/models"
)

type countingMemoryGraph struct {
	graph.GraphRepository
	upserts atomic.Int64
}

func (g *countingMemoryGraph) UpsertNode(kind string, identity, properties map[string]any) (*models.Node, error) {
	g.upserts.Add(1)
	return g.GraphRepository.UpsertNode(kind, identity, properties)
}

func TestMemoryStoreWritesNodeOnce(t *testing.T) {
	base := graph.NewMemoryGraphRepository()
	spy := &countingMemoryGraph{GraphRepository: base}
	service := NewMemoryService(spy)
	if _, err := service.Store("p", "title", "content", nil); err != nil {
		t.Fatal(err)
	}
	if got := spy.upserts.Load(); got != 1 {
		t.Fatalf("UpsertNode calls = %d, want 1", got)
	}
}

func setupMemory(t *testing.T) (*Application, map[string]any) {
	t.Helper()
	app := ApplicationInMemory()
	fn, err := app.Graph.UpsertNode("Function", map[string]any{"project_id": "p", "qualified_name": "a.go:ParsePacket"},
		map[string]any{"name": "ParsePacket"})
	if err != nil {
		t.Fatal(err)
	}
	return app, map[string]any{"id": fn.ID}
}

func TestMemoryLinksEntitiesAndCompactsLosslessly(t *testing.T) {
	app, fn := setupMemory(t)
	m := app.Memory
	stored := []struct{ title, content string }{
		{"m1", "ParsePacket reads a 4 byte header.\nIt rejects length zero."},
		{"m2", "ParsePacket reads a 4 byte header.\nIt is called from the decoder loop."},
		{"m3", "Unrelated note without entities."},
	}
	for _, s := range stored {
		if _, err := m.Store("p", s.title, s.content, map[string]any{"agent": "a", "auto_compact": false}); err != nil {
			t.Fatal(err)
		}
	}
	nbrs, _ := app.Graph.Neighbors(fn["id"].(string), "MENTIONS", graph.DirIn)
	if len(nbrs) != 2 {
		t.Fatalf("expected 2 MENTIONS edges into function, got %d", len(nbrs))
	}

	stats, err := m.Compact("p", 2)
	if err != nil {
		t.Fatal(err)
	}
	if stats["memories_archived"].(int) != 2 || stats["summaries_updated"].(int) != 1 {
		t.Fatalf("unexpected stats %v", stats)
	}

	// Default search hides archived originals but the summary keeps every unique line.
	res, _ := m.Search("p", "decoder loop", 10)
	if len(res) != 1 || res[0]["kind"] != compactedKind {
		t.Fatalf("expected only summary, got %v", res)
	}
	content := res[0]["content"].(string)
	for _, line := range []string{"4 byte header", "rejects length zero", "decoder loop"} {
		if !strings.Contains(content, line) {
			t.Fatalf("summary lost %q:\n%s", line, content)
		}
	}
	if strings.Count(content, "reads a 4 byte header") != 1 {
		t.Fatalf("duplicate line not folded:\n%s", content)
	}
	if all, _ := m.SearchAll("p", "decoder loop", 10, true); len(all) != 2 {
		t.Fatalf("include_archived should return summary and original, got %d", len(all))
	}
	// Unlinked memory untouched; originals retrievable by title.
	if got, _ := m.Get("p", "m3"); got == nil || got["archived"] == true {
		t.Fatalf("unrelated memory should stay active: %v", got)
	}
	if got, _ := m.Get("p", "m1"); got == nil || got["compacted_into"] == nil {
		t.Fatalf("original must be preserved and point to summary: %v", got)
	}

	// New memory about the same entity merges into the existing summary.
	m.Store("p", "m4", "ParsePacket also validates the checksum.", map[string]any{"auto_compact": false})
	if _, err := m.Compact("p", 2); err != nil {
		t.Fatal(err)
	}
	res, _ = m.Search("p", "checksum", 10)
	if len(res) != 1 || !strings.Contains(res[0]["content"].(string), "4 byte header") {
		t.Fatalf("summary should accumulate old and new knowledge: %v", res)
	}
}

func TestMemoryAutoCompactsAtThreshold(t *testing.T) {
	app, _ := setupMemory(t)
	var last map[string]any
	for i := 0; i < autoCompactThreshold; i++ {
		res, err := app.Memory.Store("p", fmt.Sprintf("note-%d", i), fmt.Sprintf("ParsePacket fact number %d", i), nil)
		if err != nil {
			t.Fatal(err)
		}
		last = res
	}
	if last["compaction"] == nil {
		t.Fatal("expected automatic compaction at threshold")
	}
	res, _ := app.Memory.Search("p", "fact number 7", 10)
	if len(res) != 1 || !strings.Contains(res[0]["content"].(string), "fact number 7") {
		t.Fatalf("knowledge lost after auto compaction: %v", res)
	}
}

func TestMemoryCompactionSurvivesPersistentReload(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.gob")
	open := func() (*Application, *graph.PersistentGraphRepository) {
		repo, err := graph.NewPersistentRepository(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		return NewApplication(repo), repo
	}

	app, repo := open()
	fn, err := app.Graph.UpsertNode("Function", map[string]any{"project_id": "p", "qualified_name": "a.go:ParsePacket"},
		map[string]any{"name": "ParsePacket"})
	if err != nil {
		t.Fatal(err)
	}
	for i, c := range []string{"ParsePacket reads a 4 byte header.\nIt rejects length zero.", "ParsePacket reads a 4 byte header.\nIt is called from the decoder loop."} {
		if _, err := app.Memory.Store("p", fmt.Sprintf("m%d", i+1), c, map[string]any{"agent": "a", "auto_compact": false}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.Memory.Compact("p", 2); err != nil {
		t.Fatal(err)
	}
	before, _ := app.Memory.Get("p", "m1")
	createdBefore := before["created_at"]
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen from disk and verify everything survived the gob round trip.
	app, repo = open()
	defer repo.Close()
	res, _ := app.Memory.Search("p", "decoder loop", 10)
	if len(res) != 1 || res[0]["kind"] != compactedKind {
		t.Fatalf("summary missing after reload: %v", res)
	}
	content := res[0]["content"].(string)
	for _, line := range []string{"4 byte header", "rejects length zero", "decoder loop"} {
		if !strings.Contains(content, line) {
			t.Fatalf("summary lost %q after reload:\n%s", line, content)
		}
	}
	if got, _ := app.Memory.Get("p", "m1"); got == nil || got["archived"] != true || got["compacted_into"] == nil {
		t.Fatalf("archived original not preserved: %v", got)
	}
	if in, _ := app.Graph.Neighbors(fn.ID, "MENTIONS", graph.DirIn); len(in) != 3 { // 2 memories + summary
		t.Fatalf("expected 3 MENTIONS edges after reload, got %d", len(in))
	}
	if got, _ := app.Memory.Get("p", "m1"); got["created_at"] != createdBefore {
		t.Fatalf("created_at changed across reload: %v -> %v", createdBefore, got["created_at"])
	}
	summaryID := res[0]["id"].(string)
	if in, _ := app.Graph.Neighbors(summaryID, "SUMMARIZED_BY", graph.DirIn); len(in) != 2 {
		t.Fatalf("expected 2 SUMMARIZED_BY edges after reload, got %d", len(in))
	}

	// Compaction keeps working on the reloaded graph and merges into the same summary.
	app.Memory.Store("p", "m3", "ParsePacket also validates the checksum.", map[string]any{"auto_compact": false})
	if _, err := app.Memory.Compact("p", 2); err != nil {
		t.Fatal(err)
	}
	res, _ = app.Memory.Search("p", "ParsePacket", 10)
	if len(res) != 1 {
		t.Fatalf("expected a single merged summary, got %d results", len(res))
	}
	c := res[0]["content"].(string)
	if !strings.Contains(c, "checksum") || !strings.Contains(c, "4 byte header") {
		t.Fatalf("merged summary lost knowledge:\n%s", c)
	}
	if res[0]["compacted_count"] != 3 {
		t.Fatalf("compacted_count = %v, want 3", res[0]["compacted_count"])
	}
}
