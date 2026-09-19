package search

import (
	"reflect"
	"testing"
)

func TestTokenizeSplitsIdentifiersAndFoldsPlurals(t *testing.T) {
	got := Tokenize("ParsePacket parse_headers HTTPServer tracks")
	want := []string{"parsepacket", "parse", "packet", "parse", "header", "httpserver", "http", "server", "track"}
	// "parse_headers" splits on the underscore into two plain words.
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func docs() []Document {
	return []Document{
		{ID: "title-hit", Fields: []Field{{"Packet decoder", 3}, {"unrelated body text about logging", 1}}},
		{ID: "body-hit", Fields: []Field{{"Notes", 3}, {"the decoder handles a packet stream", 1}}},
		{ID: "none", Fields: []Field{{"Other", 3}, {"nothing relevant here", 1}}},
		{ID: "stuffed", Fields: []Field{{"Filler", 3}, {"packet packet packet packet packet packet packet packet packet", 1}}},
	}
}

func TestBM25RanksTitleAndBalancedMatchesAboveKeywordStuffing(t *testing.T) {
	hits := NewBM25().Rank("packet decoder", docs(), 10)
	if len(hits) != 3 {
		t.Fatalf("expected 3 hits (irrelevant doc omitted), got %v", hits)
	}
	if hits[0].ID != "title-hit" {
		t.Fatalf("title match should rank first: %v", hits)
	}
	if hits[len(hits)-1].ID != "stuffed" {
		t.Fatalf("a doc matching one of two terms should rank last: %v", hits)
	}
}

func TestBM25CamelCaseQueryMatchesSplitWords(t *testing.T) {
	d := []Document{{ID: "a", Fields: []Field{{"parse the packet header", 1}}}, {ID: "b", Fields: []Field{{"unrelated", 1}}}}
	hits := NewBM25().Rank("ParsePacket", d, 5)
	if len(hits) != 1 || hits[0].ID != "a" {
		t.Fatalf("got %v", hits)
	}
}

func TestBM25LimitAndEmptyQuery(t *testing.T) {
	if hits := NewBM25().Rank("packet", docs(), 1); len(hits) != 1 {
		t.Fatalf("limit not applied: %v", hits)
	}
	if hits := NewBM25().Rank("  !! ", docs(), 5); hits != nil {
		t.Fatalf("empty query should return nothing: %v", hits)
	}
}

type fixedRanker []string

func (f fixedRanker) Rank(string, []Document, int) []Hit {
	var out []Hit
	for i, id := range f {
		out = append(out, Hit{ID: id, Score: float64(len(f) - i)})
	}
	return out
}

func TestHybridFusesRankers(t *testing.T) {
	h := &Hybrid{Rankers: []Ranker{fixedRanker{"a", "b", "c"}, fixedRanker{"c", "b", "d"}}}
	hits := h.Rank("q", nil, 10)
	if len(hits) != 4 {
		t.Fatalf("expected union of results: %v", hits)
	}
	top := map[string]bool{hits[0].ID: true, hits[1].ID: true}
	if !top["b"] || !top["c"] {
		t.Fatalf("items ranked by both rankers (b, c) should outrank single-ranker items: %v", hits)
	}
}

func TestVectorRanker(t *testing.T) {
	docs := []Document{
		{ID: "semantic", Fields: []Field{{"packet decoder stream", 1}}},
		{ID: "keyword", Fields: []Field{{"packet decoder", 1}}},
		{ID: "partial", Fields: []Field{{"decoder handler", 1}}},
		{ID: "none", Fields: []Field{{"unrelated logging", 1}}},
	}
	vr := NewVectorRanker()
	hits := vr.Rank("decoder stream", docs, 10)
	if len(hits) != 3 {
		t.Fatalf("expected 3 hits (none omitted), got %v", hits)
	}
	if hits[0].ID != "semantic" {
		t.Fatalf("semantic match should rank first: %v", hits)
	}
	if hits[len(hits)-1].ID != "partial" {
		t.Fatalf("partial match should rank last among hits: %v", hits)
	}
	if hits[2].Score >= hits[0].Score {
		t.Fatalf("semantic should score higher than partial: %v", hits)
	}
}

func TestVectorRankerEmpty(t *testing.T) {
	vr := NewVectorRanker()
	if hits := vr.Rank("", docs(), 5); hits != nil {
		t.Fatalf("empty query should return nothing: %v", hits)
	}
	if hits := vr.Rank("query", nil, 5); hits != nil {
		t.Fatalf("nil docs should return nothing: %v", hits)
	}
}

func TestVectorRankerLimit(t *testing.T) {
	docs := []Document{
		{ID: "a", Fields: []Field{{"packet decoder", 1}}},
		{ID: "b", Fields: []Field{{"packet decoder", 1}}},
		{ID: "c", Fields: []Field{{"packet decoder", 1}}},
	}
	vr := NewVectorRanker()
	hits := vr.Rank("decoder", docs, 2)
	if len(hits) != 2 {
		t.Fatalf("limit not applied: %v", hits)
	}
}
