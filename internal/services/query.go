package services

import (
	"sort"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/search"
)

type CodeGraphService struct {
	graph  graph.GraphRepository
	ranker search.Ranker
}

func NewCodeGraphService(g graph.GraphRepository) *CodeGraphService {
	return &CodeGraphService{graph: g, ranker: search.NewBM25()}
}

// Search ranks code entities (functions, classes, structs, modules, files) by
// relevance to a free-text query. Names count most; identifiers are split so
// "parse packet" finds ParsePacket. Generated code is hidden unless
// includeGenerated is set.
func (s *CodeGraphService) Search(projectID, query string, limit int, includeGenerated bool) ([]map[string]any, error) {
	var nodes []*models.Node
	for _, kind := range []string{"Function", "Class", "Struct", "Module", "SourceFile"} {
		found, err := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
		if err != nil {
			return nil, err
		}
		for _, n := range found {
			if g, _ := n.Properties["generated"].(bool); g && !includeGenerated {
				continue
			}
			nodes = append(nodes, n)
		}
	}
	return rankNodes(s.ranker, nodes, query, limit, func(n *models.Node) []search.Field {
		return []search.Field{
			{Text: strProp(n, "name"), Weight: 3},
			{Text: strProp(n, "owner"), Weight: 1.5},
			{Text: strProp(n, "qualified_name"), Weight: 1},
			{Text: strProp(n, "path"), Weight: 0.5},
		}
	}), nil
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
	sort.SliceStable(nodes, func(i, j int) bool {
		return strProp(nodes[i], "name") < strProp(nodes[j], "name")
	})
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
		"DATA_FLOW": true,
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

// TraceDataFlow follows DATA_FLOW edges from sourceID toward targetID,
// optionally including CALLS edges as stepping stones. Returns a path
// of functions through which data flows.
func (s *CodeGraphService) TraceDataFlow(sourceID, targetID string, maxDepth int, includeCalls bool) (map[string]any, error) {
	type item struct {
		id    string
		path  []string
		kinds []string
	}
	queue := []item{{sourceID, []string{sourceID}, nil}}
	seen := map[string]bool{sourceID: true}
	edgeKinds := []string{"DATA_FLOW"}
	if includeCalls {
		edgeKinds = append(edgeKinds, "CALLS")
	}
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
			return map[string]any{"found": true, "path": path, "via": current.kinds}, nil
		}
		if len(current.path) > maxDepth {
			continue
		}
		for _, kind := range edgeKinds {
			neighbors, err := s.graph.Neighbors(current.id, kind, graph.DirOut)
			if err != nil {
				continue
			}
			for _, en := range neighbors {
				if !seen[en.Node.ID] {
					seen[en.Node.ID] = true
					kinds := append(append([]string{}, current.kinds...), kind)
					queue = append(queue, item{en.Node.ID, append(append([]string{}, current.path...), en.Node.ID), kinds})
				}
			}
		}
	}
	return map[string]any{"found": false, "path": []map[string]any{}, "via": []string{}}, nil
}

// Trace follows CALLS edges from sourceID toward targetID.
func (s *CodeGraphService) Trace(sourceID, targetID string, maxDepth int) (map[string]any, error) {
	type item struct {
		id   string
		path []string
	}
	queue := []item{{sourceID, []string{sourceID}}}
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

// Hierarchy walks EXTENDS / IMPLEMENTS edges from nodeID. direction is "up"
// (supertypes), "down" (subtypes) or "both". Cycles are broken by a visited set.
func (s *CodeGraphService) Hierarchy(nodeID, direction string, depth int) (map[string]any, error) {
	if _, err := s.graph.GetNode(nodeID); err != nil {
		return nil, err
	}
	if depth <= 0 {
		depth = 3
	}
	walk := func(dir graph.Direction) []map[string]any {
		type item struct {
			id    string
			level int
		}
		seen := map[string]bool{nodeID: true}
		queue := []item{{nodeID, 0}}
		var out []map[string]any
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if cur.level >= depth {
				continue
			}
			for _, edgeKind := range []string{"EXTENDS", "IMPLEMENTS"} {
				nbrs, err := s.graph.Neighbors(cur.id, edgeKind, dir)
				if err != nil {
					continue
				}
				for _, en := range nbrs {
					if seen[en.Node.ID] {
						continue
					}
					seen[en.Node.ID] = true
					row := Present(en.Node)
					row["relationship"] = edgeKind
					row["depth"] = cur.level + 1
					row["via"] = cur.id
					out = append(out, row)
					queue = append(queue, item{en.Node.ID, cur.level + 1})
				}
			}
		}
		return out
	}
	res := map[string]any{"node_id": nodeID, "depth": depth}
	if direction != "down" {
		res["supertypes"] = walk(graph.DirOut)
	}
	if direction != "up" {
		res["subtypes"] = walk(graph.DirIn)
	}
	return res, nil
}
