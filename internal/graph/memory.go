package graph

import (
	"errors"
	"reflect"
	"sort"
	"sync"
	"time"

	"codergag/internal/models"
)

type MemoryGraphRepository struct {
	mu    sync.RWMutex
	nodes map[string]*models.Node
	edges map[string]*models.Edge
	next  func() string
}

func NewMemoryGraphRepository() *MemoryGraphRepository {
	return &MemoryGraphRepository{
		nodes: make(map[string]*models.Node),
		edges: make(map[string]*models.Edge),
		next:  models.NewID,
	}
}

func (r *MemoryGraphRepository) UpsertNode(kind string, identity, properties map[string]any) (*models.Node, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	existing := r.findNodesLocked(kind, identity)
	if len(existing) > 0 {
		node := existing[0]
		for k, v := range properties {
			node.Properties[k] = v
		}
		node.SetProperty("updated_at", node.Properties["updated_at"])
		return node, nil
	}

	props := make(map[string]any, len(identity)+len(properties))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for k, v := range identity {
		props[k] = v
	}
	for k, v := range properties {
		props[k] = v
	}
	if _, ok := props["id"]; !ok {
		props["id"] = r.next()
	}
	if _, ok := props["created_at"]; !ok {
		props["created_at"] = now
	}
	if _, ok := props["project_id"]; !ok {
		props["project_id"] = identity["project_id"]
	}
	node := &models.Node{Kind: kind, Properties: props, ID: props["id"].(string)}
	r.nodes[node.ID] = node
	return node, nil
}

func (r *MemoryGraphRepository) GetNode(nodeID string) (*models.Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	return node, nil
}

func (r *MemoryGraphRepository) FindNodes(kind string, filters map[string]any) ([]*models.Node, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.findNodesLocked(kind, filters), nil
}

func (r *MemoryGraphRepository) findNodesLocked(kind string, filters map[string]any) []*models.Node {
	var results []*models.Node
	for _, node := range r.nodes {
		if kind != "" && node.Kind != kind {
			continue
		}
		match := true
		for k, v := range filters {
			if !reflect.DeepEqual(node.Properties[k], v) {
				match = false
				break
			}
		}
		if match {
			results = append(results, node)
		}
	}
	// Map iteration order is random; a stable order keeps paginated queries consistent.
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results
}

func (r *MemoryGraphRepository) Link(kind, fromID, toID string, properties map[string]any) (*models.Edge, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, edge := range r.edges {
		if edge.Kind == kind && edge.FromID == fromID && edge.ToID == toID && reflect.DeepEqual(edge.Properties, properties) {
			return edge, nil
		}
	}
	edge := models.NewEdge(kind, fromID, toID, properties)
	edge.ID = r.next()
	r.edges[edge.ID] = edge
	return edge, nil
}

func (r *MemoryGraphRepository) Neighbors(nodeID, edgeKind string, direction Direction) ([]EdgeNode, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var results []EdgeNode
	for _, edge := range r.edges {
		if edgeKind != "" && edge.Kind != edgeKind {
			continue
		}
		switch direction {
		case DirOut:
			if edge.FromID == nodeID {
				if node, ok := r.nodes[edge.ToID]; ok {
					results = append(results, EdgeNode{Edge: edge, Node: node})
				}
			}
		case DirIn:
			if edge.ToID == nodeID {
				if node, ok := r.nodes[edge.FromID]; ok {
					results = append(results, EdgeNode{Edge: edge, Node: node})
				}
			}
		case DirBoth:
			if edge.FromID == nodeID {
				if node, ok := r.nodes[edge.ToID]; ok {
					results = append(results, EdgeNode{Edge: edge, Node: node})
				}
			}
			if edge.ToID == nodeID {
				if node, ok := r.nodes[edge.FromID]; ok {
					results = append(results, EdgeNode{Edge: edge, Node: node})
				}
			}
		}
	}
	return results, nil
}

func (r *MemoryGraphRepository) RemoveNodes(nodeIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	idSet := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		idSet[id] = true
	}

	for id := range idSet {
		delete(r.nodes, id)
	}
	for edgeID, edge := range r.edges {
		if idSet[edge.FromID] || idSet[edge.ToID] {
			delete(r.edges, edgeID)
		}
	}
	return nil
}

// EdgeRef is a copy of an edge's endpoints, safe to use without holding locks.
type EdgeRef struct{ From, To string }

// EdgesOfKind lists every edge of one kind in a single pass.
func (r *MemoryGraphRepository) EdgesOfKind(kind string) []EdgeRef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []EdgeRef
	for _, e := range r.edges {
		if e.Kind == kind {
			out = append(out, EdgeRef{e.FromID, e.ToID})
		}
	}
	return out
}

// Counts returns node and edge totals by kind without copying the graph.
func (r *MemoryGraphRepository) Counts() (nodes, edges map[string]int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes, edges = map[string]int{}, map[string]int{}
	for _, n := range r.nodes {
		nodes[n.Kind]++
	}
	for _, e := range r.edges {
		edges[e.Kind]++
	}
	return nodes, edges
}

func (r *MemoryGraphRepository) RemoveEdges(edgeIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range edgeIDs {
		delete(r.edges, id)
	}
	return nil
}

func (r *MemoryGraphRepository) QueryReadonly(query string, params map[string]any) ([]map[string]any, error) {
	return nil, errors.New("advanced queries are not enabled by this repository")
}

func (r *MemoryGraphRepository) NodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}

func (r *MemoryGraphRepository) EdgeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.edges)
}

func (r *MemoryGraphRepository) Close() error { return nil }
