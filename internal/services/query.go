package services

import (
	"sort"
	"strings"
	"sync"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/search"
)

type CodeGraphService struct {
	graph       graph.GraphRepository
	ranker      search.Ranker
	indexes     map[string]*graph.GraphQueryIndex
	indexBuilds map[string]*graph.GraphQueryIndex
	indexMu     sync.RWMutex
}

func NewCodeGraphService(g graph.GraphRepository) *CodeGraphService {
	return &CodeGraphService{graph: g, ranker: search.NewBM25(), indexes: make(map[string]*graph.GraphQueryIndex), indexBuilds: make(map[string]*graph.GraphQueryIndex)}
}

// NewCodeGraphServiceHybrid returns a service whose retrieval fuses BM25 keyword
// matching with a bag-of-words vector ranker via reciprocal rank fusion. It is
// used when semantic retrieval is requested; the plain constructor keeps the
// keyword-only behavior for callers that do not need vector similarity.
func NewCodeGraphServiceHybrid(g graph.GraphRepository) *CodeGraphService {
	return &CodeGraphService{
		graph:       g,
		ranker:      &search.Hybrid{Rankers: []search.Ranker{search.NewBM25(), search.NewVectorRanker()}},
		indexes:     make(map[string]*graph.GraphQueryIndex),
		indexBuilds: make(map[string]*graph.GraphQueryIndex),
	}
}

// Search ranks code entities (functions, classes, structs, modules, files) by
// relevance to a free-text query. Names count most; identifiers are split so
// "parse packet" finds ParsePacket. Generated code is hidden unless
// includeGenerated is set.
func (s *CodeGraphService) Search(projectID, query string, limit int, includeGenerated bool) ([]map[string]any, error) {
	nodes, err := s.corpus(projectID, includeGenerated)
	if err != nil {
		return nil, err
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

// SearchSemantic ranks the same corpus but fuses BM25 keyword matching with a
// bag-of-words vector similarity via reciprocal rank fusion. It surfaces
// semantically related identifiers (e.g. "cleanup" -> "teardown") that keyword
// search misses, while still ranking exact name matches highly.
func (s *CodeGraphService) SearchSemantic(projectID, query string, limit int, includeGenerated bool) ([]map[string]any, error) {
	nodes, err := s.corpus(projectID, includeGenerated)
	if err != nil {
		return nil, err
	}
	ranker := s.ranker
	if _, ok := ranker.(*search.Hybrid); !ok {
		ranker = &search.Hybrid{Rankers: []search.Ranker{search.NewBM25(), search.NewVectorRanker()}}
	}
	return rankNodes(ranker, nodes, query, limit, func(n *models.Node) []search.Field {
		return []search.Field{
			{Text: strProp(n, "name"), Weight: 3},
			{Text: strProp(n, "owner"), Weight: 1.5},
			{Text: strProp(n, "qualified_name"), Weight: 1},
			{Text: strProp(n, "path"), Weight: 0.5},
		}
	}), nil
}

// corpus gathers every indexable code entity for a project in one pass so the
// ranker sees the full document set (needed for document-frequency statistics).
func (s *CodeGraphService) corpus(projectID string, includeGenerated bool) ([]*models.Node, error) {
	var nodes []*models.Node
	for _, kind := range []string{"Function", "Class", "Struct", "Module", "SourceFile"} {
		found, ok := s.nodesByKind(projectID, kind)
		if !ok {
			var err error
			found, err = s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
			if err != nil {
				return nil, err
			}
		}
		for _, n := range found {
			if g, _ := n.Properties["generated"].(bool); g && !includeGenerated {
				continue
			}
			nodes = append(nodes, n)
		}
	}
	return nodes, nil
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
	low := strings.ToLower(query)
	// Fast path: exact name or qualified-name hit against the project index is
	// O(candidate set). Substring queries fall through to the authoritative scan,
	// which returns node references without cloning the whole project.
	if idx := s.indexFor(projectID); idx != nil {
		if state, _ := idx.State(); state == graph.IndexReady {
			if ids := idx.ByName(query); len(ids) > 0 {
				return s.presentNodes(idx, ids, low, limit), nil
			}
			if ids := idx.ByQualifiedName(query); len(ids) > 0 {
				return s.presentNodes(idx, ids, low, limit), nil
			}
		}
	}
	nodes, err := s.graph.FindNodes(kind, map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		return strProp(nodes[i], "name") < strProp(nodes[j], "name")
	})
	return s.presentMatches(nodes, low, limit), nil
}

func (s *CodeGraphService) presentNodes(idx *graph.GraphQueryIndex, ids []string, low string, limit int) []map[string]any {
	var rows []map[string]any
	for _, id := range ids {
		node, ok := idx.GetNode(id)
		if !ok {
			continue
		}
		name, _ := node.Properties["name"].(string)
		qname, _ := node.Properties["qualified_name"].(string)
		if strings.Contains(strings.ToLower(name), low) || strings.Contains(strings.ToLower(qname), low) {
			rows = append(rows, Present(node))
			if len(rows) >= limit {
				break
			}
		}
	}
	return rows
}

func (s *CodeGraphService) presentMatches(nodes []*models.Node, low string, limit int) []map[string]any {
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
	return rows
}

func (s *CodeGraphService) Function(projectID, name string) (map[string]any, error) {
	// Fast path: exact name lookup against the project index (O(candidate set)),
	// not a full project scan. Falls back to the authoritative graph when the
	// index is unavailable.
	if idx := s.indexFor(projectID); idx != nil {
		if state, _ := idx.State(); state == graph.IndexReady {
			if node, ok := s.nodeByName(idx, name); ok {
				return Present(node), nil
			}
		}
	}
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

// nodeByName resolves an exact function name through the index, preferring a
// qualified "Owner.method" match and then a bare-name match. Returns the first
// deterministic candidate; ambiguous names fall through to the caller.
func (s *CodeGraphService) nodeByName(idx *graph.GraphQueryIndex, name string) (*models.Node, bool) {
	if strings.Contains(name, ".") {
		if ids := idx.ByQualifiedName(name); len(ids) > 0 {
			if node, ok := idx.GetNode(ids[0]); ok && strProp(node, "project_id") == s.projectForNode(ids[0]) {
				return node, true
			}
		}
	}
	if ids := idx.ByName(name); len(ids) > 0 {
		if node, ok := idx.GetNode(ids[0]); ok {
			return node, true
		}
	}
	return nil, false
}

const (
	defaultGraphDepth = 3
	defaultGraphLimit = 1000
	maxGraphDepth     = 64
	maxGraphFanOut    = 1024
	maxGraphNodes     = 10000
	maxGraphEdges     = 100000
)

type graphTraversalLimits struct {
	depth     int
	nodes     int
	edges     int
	fanOut    int
	truncated bool
}

func normalizeGraphTraversal(depth, limit int) graphTraversalLimits {
	if depth < 0 {
		depth = 0
	}
	if depth > maxGraphDepth {
		depth = maxGraphDepth
	}
	if limit <= 0 {
		limit = defaultGraphLimit
	}
	if limit > maxGraphNodes {
		limit = maxGraphNodes
	}
	return graphTraversalLimits{depth: depth, nodes: limit, edges: maxGraphEdges, fanOut: maxGraphFanOut}
}

func sortEdgeNodes(items []graph.EdgeNode) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Edge.ID != items[j].Edge.ID {
			return items[i].Edge.ID < items[j].Edge.ID
		}
		return items[i].Node.ID < items[j].Node.ID
	})
}

func boundedNeighbors(items []graph.EdgeNode, limits *graphTraversalLimits) []graph.EdgeNode {
	sortEdgeNodes(items)
	if len(items) > limits.fanOut {
		limits.truncated = true
		return items[:limits.fanOut]
	}
	return items
}

func traversalMetadata(limits graphTraversalLimits) map[string]any {
	return map[string]any{
		"truncated": limits.truncated,
		"limits": map[string]any{
			"depth":   limits.depth,
			"nodes":   limits.nodes,
			"edges":   limits.edges,
			"fan_out": limits.fanOut,
		},
	}
}

// TraceDataFlow follows DATA_FLOW edges from sourceID toward targetID,
// optionally including CALLS edges as stepping stones. Returns a path
// of functions through which data flows.
func (s *CodeGraphService) IndexFor(projectID string) *graph.GraphQueryIndex {
	return s.indexFor(projectID)
}

func (s *CodeGraphService) InvalidateIndex(projectID string) {
	if projectID == "" {
		return
	}
	s.indexMu.RLock()
	idx := s.indexes[projectID]
	s.indexMu.RUnlock()
	if idx != nil {
		idx.Invalidate()
	}
}

func (s *CodeGraphService) RefreshIndexAsync(projectID string) {
	if projectID == "" {
		return
	}
	go func() {
		if idx := s.indexFor(projectID); idx != nil {
			_ = idx.Refresh()
		}
	}()
}

func (s *CodeGraphService) indexFor(projectID string) *graph.GraphQueryIndex {
	if projectID == "" {
		return nil
	}
	if _, ok := s.graph.(graph.GraphGenerationProvider); !ok {
		return nil
	}
	s.indexMu.RLock()
	idx := s.indexes[projectID]
	if idx != nil {
		s.indexMu.RUnlock()
		return idx
	}
	idx = s.indexBuilds[projectID]
	s.indexMu.RUnlock()
	if idx != nil {
		return idx
	}

	idx = graph.NewGraphQueryIndex(s.graph, projectID)
	s.indexMu.Lock()
	if existing := s.indexes[projectID]; existing != nil {
		s.indexMu.Unlock()
		return existing
	}
	if building := s.indexBuilds[projectID]; building != nil {
		s.indexMu.Unlock()
		return building
	}
	s.indexBuilds[projectID] = idx
	s.indexMu.Unlock()

	if err := idx.Rebuild(); err != nil {
		s.indexMu.Lock()
		if s.indexBuilds[projectID] == idx {
			delete(s.indexBuilds, projectID)
		}
		s.indexMu.Unlock()
		return nil
	}

	s.indexMu.Lock()
	delete(s.indexBuilds, projectID)
	if existing := s.indexes[projectID]; existing != nil {
		s.indexMu.Unlock()
		return existing
	}
	s.indexes[projectID] = idx
	s.indexMu.Unlock()
	return idx
}

func (s *CodeGraphService) nodesByKind(projectID, kind string) ([]*models.Node, bool) {
	idx := s.indexFor(projectID)
	if idx == nil {
		return nil, false
	}
	nodes := idx.NodesByKind(kind)
	state, _ := idx.State()
	if state != graph.IndexReady {
		return nil, false
	}
	return nodes, true
}

func (s *CodeGraphService) neighbors(projectID, nodeID, edgeKind string, direction graph.Direction) ([]graph.EdgeNode, bool) {
	idx := s.indexFor(projectID)
	if idx == nil {
		return nil, false
	}
	return idx.Neighbors(nodeID, edgeKind, direction)
}

func (s *CodeGraphService) projectForNode(nodeID string) string {
	node, err := s.graph.GetNode(nodeID)
	if err != nil {
		return ""
	}
	return strProp(node, "project_id")
}

func (s *CodeGraphService) nodeNeighbors(nodeID, edgeKind string, direction graph.Direction) ([]graph.EdgeNode, bool) {
	return s.neighbors(s.projectForNode(nodeID), nodeID, edgeKind, direction)
}

func (s *CodeGraphService) Related(nodeID, edgeKind string, direction string) ([]map[string]any, error) {
	dir := graph.Direction(direction)
	var neighbors []graph.EdgeNode
	var ok bool
	if projectID := s.projectForNode(nodeID); projectID != "" {
		neighbors, ok = s.neighbors(projectID, nodeID, edgeKind, dir)
	}
	if !ok {
		var err error
		neighbors, err = s.graph.Neighbors(nodeID, edgeKind, dir)
		if err != nil {
			return nil, err
		}
	}
	neighbors = boundedNeighbors(neighbors, &graphTraversalLimits{fanOut: maxGraphFanOut})
	var results []map[string]any
	for _, en := range neighbors {
		if en.Node == nil || en.Edge == nil {
			continue
		}
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
	limits := normalizeGraphTraversal(depth, limit)
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
	projectID := strProp(root, "project_id")
	for len(queue) > 0 && len(impacts) < limits.nodes {
		current := queue[0]
		queue = queue[1:]
		if current.level >= limits.depth {
			continue
		}
		var neighbors []graph.EdgeNode
		var ok bool
		if projectID != "" {
			neighbors, ok = s.neighbors(projectID, current.id, "", graph.DirIn)
		}
		if !ok {
			neighbors, _ = s.graph.Neighbors(current.id, "", graph.DirIn)
		}
		neighbors = boundedNeighbors(neighbors, &limits)
		for _, en := range neighbors {
			limits.edges--
			if limits.edges < 0 {
				limits.truncated = true
				break
			}
			if en.Node == nil || en.Edge == nil || !inbound[en.Edge.Kind] || seen[en.Node.ID] {
				continue
			}
			seen[en.Node.ID] = true
			queue = append(queue, item{en.Node.ID, current.level + 1})
			row := Present(en.Node)
			row["via"] = en.Edge.Kind
			row["distance"] = current.level + 1
			impacts = append(impacts, row)
		}
		if limits.truncated && limits.edges < 0 {
			break
		}
	}
	result := map[string]any{
		"subject":        Present(root),
		"depth":          depth,
		"affected_count": len(impacts),
		"affected":       impacts,
	}
	for key, value := range traversalMetadata(limits) {
		result[key] = value
	}
	return result, nil
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
			neighbors, ok := s.nodeNeighbors(current.id, kind, graph.DirOut)
			if !ok {
				neighbors, _ = s.graph.Neighbors(current.id, kind, graph.DirOut)
			}
			for _, en := range neighbors {
				if en.Node == nil {
					continue
				}
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
		neighbors, ok := s.nodeNeighbors(current.id, "CALLS", graph.DirOut)
		if !ok {
			neighbors, _ = s.graph.Neighbors(current.id, "CALLS", graph.DirOut)
		}
		for _, en := range neighbors {
			if en.Node == nil {
				continue
			}
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
				nbrs, ok := s.nodeNeighbors(cur.id, edgeKind, dir)
				if !ok {
					nbrs, _ = s.graph.Neighbors(cur.id, edgeKind, dir)
				}
				for _, en := range nbrs {
					if en.Node == nil {
						continue
					}
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
