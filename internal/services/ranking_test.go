package services

import (
	"os"
	"path/filepath"
	"testing"

	"codergag/internal/search"
)

func TestMemorySearchIsRankedByRelevance(t *testing.T) {
	app := ApplicationInMemory()
	m := app.Memory
	off := map[string]any{"auto_compact": false}
	m.Store("p", "logging", "the decoder writes verbose logs. packet packet packet packet packet", off)
	m.Store("p", "Packet decoder design", "how the decoder splits a stream", off)
	m.Store("p", "unrelated", "kubernetes deployment notes", off)
	m.Store("other", "Packet decoder design", "belongs to another project", off)

	res, err := m.Search("p", "packet decoder", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 relevant memories in project p, got %d: %v", len(res), res)
	}
	if res[0]["title"] != "Packet decoder design" {
		t.Fatalf("title match on both terms should rank first, got %v", res[0]["title"])
	}
	if res[0]["score"].(float64) <= res[1]["score"].(float64) {
		t.Fatalf("scores must be descending: %v %v", res[0]["score"], res[1]["score"])
	}
}

func TestMemorySearchSubstringFallback(t *testing.T) {
	app := ApplicationInMemory()
	app.Memory.Store("p", "note", "CAT48 updates target tracks", map[string]any{"auto_compact": false})
	if res, _ := app.Memory.Search("p", "updat", 5); len(res) != 1 {
		t.Fatalf("partial-word query should fall back to substring match, got %v", res)
	}
	if res, _ := app.Memory.Search("p", "zzzz", 5); len(res) != 0 {
		t.Fatalf("non-matching query should return nothing, got %v", res)
	}
}

func TestDocumentSearchRanksHeadingMatches(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "d.md")
	md := "# Overview\nlots of text about caching caching caching caching\n\n# Retry policy\nbackoff details\n\n# Misc\nnothing\n"
	if err := os.WriteFile(doc, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	if _, err := app.Documents.IndexMarkdown("p", doc); err != nil {
		t.Fatal(err)
	}
	res, err := app.Documents.Search("p", "retry", 5)
	if err != nil || len(res) != 1 || res[0]["heading"] != "Retry policy" {
		t.Fatalf("got %v %v", res, err)
	}
	if res[0]["level"] != 1 {
		t.Fatalf("heading level = %v, want 1", res[0]["level"])
	}
}

func TestCodeSearchSplitsIdentifiersAndRanksNames(t *testing.T) {
	dir := t.TempDir()
	src := "package x\nfunc ParsePacket() {}\nfunc Other() { parsePacketHelper() }\nfunc parsePacketHelper() {}\ntype PacketParser struct{}\n"
	if err := os.WriteFile(filepath.Join(dir, "p.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil); err != nil {
		t.Fatal(err)
	}
	res, err := app.Code.Search("p", "parse packet", 10, false)
	if err != nil || len(res) < 3 {
		t.Fatalf("expected ParsePacket, parsePacketHelper and PacketParser, got %v %v", res, err)
	}
	for _, r := range res[:3] {
		if r["name"] == "Other" {
			t.Fatalf("Other does not match the query but ranked in top 3: %v", res)
		}
	}
	if res[0]["score"].(float64) < res[1]["score"].(float64) {
		t.Fatal("results must be sorted by descending score")
	}
	if isolated, _ := app.Code.Search("another", "parse packet", 10, false); len(isolated) != 0 {
		t.Fatalf("search leaked across projects: %v", isolated)
	}
}

type reverseRanker struct{}

func (reverseRanker) Rank(_ string, docs []search.Document, limit int) []search.Hit {
	var out []search.Hit
	for i, d := range docs {
		out = append(out, search.Hit{ID: d.ID, Score: float64(i + 1)})
	}
	return out
}

func TestRankerIsReplaceable(t *testing.T) {
	app := ApplicationInMemory()
	app.Memory.Store("p", "a", "alpha", map[string]any{"auto_compact": false})
	app.SetRanker(reverseRanker{})
	res, _ := app.Memory.Search("p", "anything", 10)
	if len(res) != 1 {
		t.Fatalf("custom ranker not used: %v", res)
	}
}
