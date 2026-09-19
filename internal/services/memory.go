package services

import (
	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/search"
)

type MemoryService struct {
	graph  graph.GraphRepository
	ranker search.Ranker
}

func NewMemoryService(g graph.GraphRepository) *MemoryService {
	return &MemoryService{graph: g, ranker: search.NewBM25()}
}

func (s *MemoryService) Store(projectID, title, content string, opts map[string]any) (map[string]any, error) {
	kind := "engineering_memory"
	if o, ok := opts["kind"].(string); ok {
		kind = o
	}
	agent := "unknown"
	if o, ok := opts["agent"].(string); ok {
		agent = o
	}
	source := "agent"
	if o, ok := opts["source"].(string); ok {
		source = o
	}
	node, err := s.graph.UpsertNode("Memory", map[string]any{
		"project_id": projectID,
		"title":      title,
	}, map[string]any{
		"content":      content,
		"kind":         kind,
		"agent_source": agent,
		"source":       source,
		"archived":     false,
	})
	if err != nil {
		return nil, err
	}
	ids, names, err := s.linkEntities(projectID, node, title+"\n"+content)
	if err != nil {
		return nil, err
	}
	node.SetProperty("entity_ids", toAny(ids))
	node.SetProperty("entities", toAny(names))
	if _, err := s.graph.UpsertNode("Memory", map[string]any{"project_id": projectID, "title": title},
		map[string]any{"entity_ids": toAny(ids), "entities": toAny(names)}); err != nil {
		return nil, err
	}
	res := Present(node)
	if auto, ok := opts["auto_compact"].(bool); !ok || auto {
		if s.activeLinkedCount(projectID) >= autoCompactThreshold {
			if stats, err := s.Compact(projectID, 2); err == nil {
				res["compaction"] = stats
			}
		}
	}
	return res, nil
}

func (s *MemoryService) Search(projectID, query string, limit int) ([]map[string]any, error) {
	return s.SearchAll(projectID, query, limit, false)
}

// SearchAll searches memories; archived originals (folded into compacted
// summaries) are skipped unless includeArchived is set.
func (s *MemoryService) SearchAll(projectID, query string, limit int, includeArchived bool) ([]map[string]any, error) {
	nodes, err := s.graph.FindNodes("Memory", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	if !includeArchived {
		active := nodes[:0:0]
		for _, n := range nodes {
			if !isArchived(n) {
				active = append(active, n)
			}
		}
		nodes = active
	}
	return rankNodes(s.ranker, nodes, query, limit, func(n *models.Node) []search.Field {
		return []search.Field{
			{Text: strProp(n, "title"), Weight: 3},
			{Text: strProp(n, "content"), Weight: 1},
		}
	}), nil
}

func (s *MemoryService) Get(projectID, title string) (map[string]any, error) {
	nodes, err := s.graph.FindNodes("Memory", map[string]any{"project_id": projectID, "title": title})
	if err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, nil
	}
	return Present(nodes[0]), nil
}
