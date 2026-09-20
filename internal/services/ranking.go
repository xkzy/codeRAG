package services

import (
	"strconv"
	"strings"

	"codergag/internal/models"
	"codergag/internal/search"
)

// rankNodes scores nodes against query with ranker, using build to describe
// each node's searchable fields. If BM25-style ranking finds nothing (for
// example a partial-word query like "pars") it falls back to a case-insensitive
// substring match over the same fields so recall never regresses.
func rankNodes(ranker search.Ranker, nodes []*models.Node, query string, limit int,
	build func(*models.Node) []search.Field) []map[string]any {
	if limit <= 0 {
		limit = 20
	}
	byID := make(map[string]*models.Node, len(nodes))
	docs := make([]search.Document, 0, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
		docs = append(docs, search.Document{ID: n.ID, Fields: build(n)})
	}
	var out []map[string]any
	for _, h := range ranker.Rank(query, docs, limit) {
		row := Present(byID[h.ID])
		row["score"] = h.Score
		out = append(out, row)
	}
	if len(out) > 0 {
		return out
	}
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	for _, d := range docs {
		for _, f := range d.Fields {
			if strings.Contains(strings.ToLower(f.Text), q) {
				row := Present(byID[d.ID])
				row["score"] = 0.0
				out = append(out, row)
				break
			}
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

func strProp(n *models.Node, key string) string {
	s, _ := n.Properties[key].(string)
	return s
}

// StrProp is the exported form of strProp, used by the CLI and HTTP layer.
func StrProp(n *models.Node, key string) string { return strProp(n, key) }

// DecodeUsage decodes a JSON-encoded per-tool usage blob.
func DecodeUsage(blob string) map[string]*ToolUsage { return decodeUsage(blob) }

// mapStrProp reads a string from a plain map[string]any (e.g. a parsed section).
func mapStrProp(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// mapIntProp reads an int from a plain map[string]any, tolerating float64
// (JSON numbers) and string-encoded integers.
func mapIntProp(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return 0
}

// SetRanker replaces the ranking strategy used by every search surface.
func (a *Application) SetRanker(r search.Ranker) {
	a.Memory.ranker = r
	a.Documents.ranker = r
	a.Code.ranker = r
}
