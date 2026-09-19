package services

import (
	"strings"

	"codergag/internal/graph"
)

type MemoryService struct {
	graph graph.GraphRepository
}

func NewMemoryService(g graph.GraphRepository) *MemoryService {
	return &MemoryService{graph: g}
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
		"content":    content,
		"kind":       kind,
		"agent_source": agent,
		"source":     source,
	})
	if err != nil {
		return nil, err
	}
	return Present(node), nil
}

func (s *MemoryService) Search(projectID, query string, limit int) ([]map[string]any, error) {
	q := strings.ToLower(query)
	nodes, err := s.graph.FindNodes("Memory", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, n := range nodes {
		title, _ := n.Properties["title"].(string)
		content, _ := n.Properties["content"].(string)
		combined := strings.ToLower(title + " " + content)
		if strings.Contains(combined, q) {
			results = append(results, Present(n))
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
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
