package services

import (
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
)

type CodeGraphService struct {
	graph graph.GraphRepository
}

func NewCodeGraphService(g graph.GraphRepository) *CodeGraphService {
	return &CodeGraphService{graph: g}
}

func Present(node *models.Node) map[string]any {
	if node == nil {
		return nil
	}
	result := map[string]any{"id": node.ID, "kind": node.Kind}
	for k, v := range node.Properties {
		result[k] = v
	}
	return result
}

func (s *CodeGraphService) Find(projectID, kind, query string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 20
	}
	nodes, err := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(query)
	var rows []map[string]any
	for _, n := range nodes {
		name, _ := n.Properties["name"].(string)
		qname, _ := n.Properties["qualified_name"].(string)
		if strings.Contains(strings.ToLower(name), low) || strings.Contains(strings.ToLower(qname), low) {
			rows = append(rows, Present(n))
			if len(rows) >= limit {
				break
			}
		}
	}
	return rows, nil
}

func (s *CodeGraphService) Function(projectID, name string) (map[string]any, error) {
	nodes, err := s.Find(projectID, "Function", name, 20)
	if err != nil {
		return nil, err
	}
	low := strings.ToLower(name)
	for _, n := range nodes {
		nName, _ := n["name"].(string)
		if strings.EqualFold(nName, name) {
			return n, nil
		}
	}
	for _, n := range nodes {
		nName, _ := n["name"].(string)
		if strings.EqualFold(strings.ToLower(nName), low) {
			return n, nil
		}
	}
	if len(nodes) > 0 {
		return nodes[0], nil
	}
	return nil, nil
}

func (s *CodeGraphService) Related(nodeID, edgeKind string, direction string) ([]map[string]any, error) {
	neighbors, err := s.graph.Neighbors(nodeID, edgeKind, graph.Direction(direction))
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, en := range neighbors {
		row := Present(en.Node)
		row["relationship"] = en.Edge.Kind
		row["relationship_metadata"] = en.Edge.Properties
		results = append(results, row)
	}
	return results, nil
}

func (s *CodeGraphService) Impact(nodeID string, depth, limit int) (map[string]any, error) {
	root, err := s.graph.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{nodeID: true}
	type item struct {
		id    string
		level int
	}
	queue := []item{{nodeID, 0}}
	var impacts []map[string]any
	inbound := map[string]bool{
		"CALLS": true, "REFERENCES": true, "USES": true,
		"HAS_TYPE": true, "IMPORTS": true, "DEPENDS_ON": true,
	}
	for len(queue) > 0 && len(impacts) < limit {
		current := queue[0]
		queue = queue[1:]
		if current.level >= depth {
			continue
		}
		neighbors, err := s.graph.Neighbors(current.id, "", graph.DirIn)
		if err != nil {
			continue
		}
		for _, en := range neighbors {
			if inbound[en.Edge.Kind] && !seen[en.Node.ID] {
				seen[en.Node.ID] = true
				queue = append(queue, item{en.Node.ID, current.level + 1})
				row := Present(en.Node)
				row["via"] = en.Edge.Kind
				row["distance"] = current.level + 1
				impacts = append(impacts, row)
			}
		}
	}
	return map[string]any{
		"subject":        Present(root),
		"depth":          depth,
		"affected_count": len(impacts),
		"affected":       impacts,
	}, nil
}

func (s *CodeGraphService) Trace(sourceID, targetID string, maxDepth int) (map[string]any, error) {
	type item struct {
		id   string
		path []string
	}
	queue := []item{{sourceID, {sourceID}}}
	seen := map[string]bool{sourceID: true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.id == targetID {
			path := make([]map[string]any, len(current.path))
			for i, id := range current.path {
				node, err := s.graph.GetNode(id)
				if err != nil {
					continue
				}
				path[i] = Present(node)
			}
			return map[string]any{"found": true, "path": path}, nil
		}
		if len(current.path) > maxDepth {
			continue
		}
		neighbors, err := s.graph.Neighbors(current.id, "CALLS", graph.DirOut)
		if err != nil {
			continue
		}
		for _, en := range neighbors {
			if !seen[en.Node.ID] {
				seen[en.Node.ID] = true
				queue = append(queue, item{en.Node.ID, append(append([]string{}, current.path...), en.Node.ID)})
			}
		}
	}
	return map[string]any{"found": false, "path": []map[string]any{}}, nil
}
