package graph

import (
	"errors"
	"sort"
	"strings"
	"sync"

	"codergag/internal/models"
)

type IndexState string

const (
	IndexReady      IndexState = "READY"
	IndexPartial    IndexState = "PARTIAL"
	IndexRebuilding IndexState = "REBUILDING"
	IndexStale      IndexState = "STALE"
	IndexFailed     IndexState = "FAILED"
)

type GraphSnapshot struct {
	Nodes      []*models.Node
	Edges      []*models.Edge
	Generation uint64
}

type GraphSnapshotProvider interface {
	GraphSnapshot() (GraphSnapshot, error)
}

type GraphGenerationProvider interface {
	Generation() uint64
}

type adjacencyKey struct {
	nodeID    string
	edgeKind  string
	direction Direction
}

type GraphQueryIndex struct {
	mu              sync.RWMutex
	repo            GraphRepository
	projectID       string
	nodesByID       map[string]*models.Node
	nodesByKind     map[string][]string
	stableByID      map[string][]string
	byName          map[string][]string
	byQualified     map[string][]string
	byPath          map[string][]string
	outgoing        map[adjacencyKey][]EdgeNode
	incoming        map[adjacencyKey][]EdgeNode
	outgoingAll     map[string][]EdgeNode
	incomingAll     map[string][]EdgeNode
	graphGeneration uint64
	indexGeneration uint64
	state           IndexState
	lastErr         error
}

func NewGraphQueryIndex(repo GraphRepository, projectID string) *GraphQueryIndex {
	idx := &GraphQueryIndex{
		repo:        repo,
		projectID:   projectID,
		nodesByID:   make(map[string]*models.Node),
		nodesByKind: make(map[string][]string),
		stableByID:  make(map[string][]string),
		byName:      make(map[string][]string),
		byQualified: make(map[string][]string),
		byPath:      make(map[string][]string),
		outgoing:    make(map[adjacencyKey][]EdgeNode),
		incoming:    make(map[adjacencyKey][]EdgeNode),
		outgoingAll: make(map[string][]EdgeNode),
		incomingAll: make(map[string][]EdgeNode),
		state:       IndexStale,
	}
	return idx
}

func (idx *GraphQueryIndex) Rebuild() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	return idx.rebuildLocked()
}

func (idx *GraphQueryIndex) rebuildLocked() error {
	if idx.repo == nil || idx.projectID == "" {
		idx.state = IndexFailed
		idx.lastErr = errors.New("graph query index requires a repository and project")
		return idx.lastErr
	}

	idx.state = IndexRebuilding
	snapshot, err := idx.snapshotLocked()
	if err != nil {
		idx.state = IndexFailed
		idx.lastErr = err
		return err
	}
	idx.replaceLocked(snapshot)
	idx.indexGeneration++
	idx.state = IndexReady
	idx.lastErr = nil
	return nil
}

func (idx *GraphQueryIndex) snapshotLocked() (GraphSnapshot, error) {
	if provider, ok := idx.repo.(GraphSnapshotProvider); ok {
		return provider.GraphSnapshot()
	}
	return idx.scanSnapshot()
}

func (idx *GraphQueryIndex) scanSnapshot() (GraphSnapshot, error) {
	filters := map[string]any{"project_id": idx.projectID}
	nodes, err := idx.repo.FindNodes("", filters)
	if err != nil {
		return GraphSnapshot{}, err
	}
	nodeByID := make(map[string]*models.Node, len(nodes))
	for _, node := range nodes {
		if node == nil || strValue(node.Properties["project_id"]) != idx.projectID {
			continue
		}
		nodeByID[node.ID] = cloneNode(node)
	}

	edgesByID := make(map[string]*models.Edge)
	for _, node := range nodes {
		if node == nil || nodeByID[node.ID] == nil {
			continue
		}
		neighbors, err := idx.repo.Neighbors(node.ID, "", DirBoth)
		if err != nil {
			return GraphSnapshot{}, err
		}
		for _, en := range neighbors {
			if en.Edge == nil || en.Node == nil || nodeByID[en.Node.ID] == nil {
				continue
			}
			if strValue(en.Node.Properties["project_id"]) != idx.projectID {
				continue
			}
			if _, exists := edgesByID[en.Edge.ID]; exists {
				continue
			}
			edgesByID[en.Edge.ID] = cloneEdge(en.Edge)
		}
	}
	edges := make([]*models.Edge, 0, len(edgesByID))
	for _, edge := range edgesByID {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return GraphSnapshot{Nodes: sortedNodeValues(nodeByID), Edges: edges}, nil
}

func (idx *GraphQueryIndex) replaceLocked(snapshot GraphSnapshot) {
	nodesByID := make(map[string]*models.Node, len(snapshot.Nodes))
	nodesByKind := make(map[string][]string)
	stableByID := make(map[string][]string)
	byName := make(map[string][]string)
	byQualified := make(map[string][]string)
	byPath := make(map[string][]string)

	for _, node := range snapshot.Nodes {
		if node == nil || strValue(node.Properties["project_id"]) != idx.projectID {
			continue
		}
		copy := cloneNode(node)
		nodesByID[copy.ID] = copy
		nodesByKind[copy.Kind] = append(nodesByKind[copy.Kind], copy.ID)
		if stable := strValue(copy.Properties["stable_id"]); stable != "" {
			stableByID[stable] = append(stableByID[stable], copy.ID)
		}
		if name := strValue(copy.Properties["name"]); name != "" {
			byName[name] = append(byName[name], copy.ID)
		}
		if qualified := strValue(copy.Properties["qualified_name"]); qualified != "" {
			byQualified[qualified] = append(byQualified[qualified], copy.ID)
		}
		for _, key := range pathKeys(copy) {
			byPath[key] = append(byPath[key], copy.ID)
		}
	}

	outgoing := make(map[adjacencyKey][]EdgeNode)
	incoming := make(map[adjacencyKey][]EdgeNode)
	outgoingAll := make(map[string][]EdgeNode)
	incomingAll := make(map[string][]EdgeNode)
	for _, edge := range snapshot.Edges {
		if edge == nil {
			continue
		}
		from := nodesByID[edge.FromID]
		to := nodesByID[edge.ToID]
		if from == nil || to == nil {
			continue
		}
		copy := cloneEdge(edge)
		out := EdgeNode{Edge: copy, Node: cloneNode(to)}
		in := EdgeNode{Edge: cloneEdge(copy), Node: cloneNode(from)}
		outKey := adjacencyKey{nodeID: edge.FromID, edgeKind: edge.Kind, direction: DirOut}
		inKey := adjacencyKey{nodeID: edge.ToID, edgeKind: edge.Kind, direction: DirIn}
		outgoing[outKey] = append(outgoing[outKey], out)
		incoming[inKey] = append(incoming[inKey], in)
		outgoingAll[edge.FromID] = append(outgoingAll[edge.FromID], out)
		incomingAll[edge.ToID] = append(incomingAll[edge.ToID], in)
	}

	for ids := range nodesByKind {
		sort.Strings(nodesByKind[ids])
	}
	for key := range stableByID {
		sort.Strings(stableByID[key])
	}
	for key := range byName {
		sort.Strings(byName[key])
	}
	for key := range byQualified {
		sort.Strings(byQualified[key])
	}
	for key := range byPath {
		sort.Strings(byPath[key])
	}
	for key := range outgoing {
		sortEdgeNodes(outgoing[key])
	}
	for key := range incoming {
		sortEdgeNodes(incoming[key])
	}
	for nodeID := range outgoingAll {
		sortEdgeNodes(outgoingAll[nodeID])
	}
	for nodeID := range incomingAll {
		sortEdgeNodes(incomingAll[nodeID])
	}

	idx.nodesByID = nodesByID
	idx.nodesByKind = nodesByKind
	idx.stableByID = stableByID
	idx.byName = byName
	idx.byQualified = byQualified
	idx.byPath = byPath
	idx.outgoing = outgoing
	idx.incoming = incoming
	idx.outgoingAll = outgoingAll
	idx.incomingAll = incomingAll
	idx.graphGeneration = snapshot.Generation
}

func (idx *GraphQueryIndex) Refresh() error {
	idx.mu.RLock()
	current := idx.graphGeneration
	state := idx.state
	idx.mu.RUnlock()
	if state == IndexReady {
		provider, hasProvider := idx.repo.(GraphGenerationProvider)
		if !hasProvider || provider.Generation() == current {
			return nil
		}
	}
	return idx.Rebuild()
}

func (idx *GraphQueryIndex) Invalidate() {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.state = IndexStale
	idx.indexGeneration++
}

func (idx *GraphQueryIndex) Node(nodeID string) (*models.Node, bool) {
	if err := idx.Refresh(); err != nil {
		return nil, false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	node, ok := idx.nodesByID[nodeID]
	if !ok {
		return nil, false
	}
	return cloneNode(node), true
}

func (idx *GraphQueryIndex) NodesByKind(kind string) []*models.Node {
	if err := idx.Refresh(); err != nil {
		return nil
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ids := idx.nodesByKind[kind]
	nodes := make([]*models.Node, 0, len(ids))
	for _, id := range ids {
		if node := idx.nodesByID[id]; node != nil {
			nodes = append(nodes, cloneNode(node))
		}
	}
	return nodes
}

func (idx *GraphQueryIndex) StableIDs(stableID string) []string {
	if err := idx.Refresh(); err != nil {
		return nil
	}
	return idx.copyIDs(idx.stableByID[stableID])
}

func (idx *GraphQueryIndex) ByName(name string) []string {
	if err := idx.Refresh(); err != nil {
		return nil
	}
	return idx.copyIDs(idx.byName[name])
}

func (idx *GraphQueryIndex) ByQualifiedName(qualifiedName string) []string {
	if err := idx.Refresh(); err != nil {
		return nil
	}
	return idx.copyIDs(idx.byQualified[qualifiedName])
}

func (idx *GraphQueryIndex) ByPath(path string) []string {
	if err := idx.Refresh(); err != nil {
		return nil
	}
	return idx.copyIDs(idx.byPath[path])
}

func (idx *GraphQueryIndex) Neighbors(nodeID, edgeKind string, direction Direction) ([]EdgeNode, bool) {
	if err := idx.Refresh(); err != nil {
		return nil, false
	}
	if edgeKind == "" {
		return idx.AllNeighbors(nodeID, direction)
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var result []EdgeNode
	switch direction {
	case DirOut:
		result = append(result, idx.outgoing[adjacencyKey{nodeID, edgeKind, DirOut}]...)
	case DirIn:
		result = append(result, idx.incoming[adjacencyKey{nodeID, edgeKind, DirIn}]...)
	case DirBoth:
		result = append(result, idx.outgoing[adjacencyKey{nodeID, edgeKind, DirOut}]...)
		result = append(result, idx.incoming[adjacencyKey{nodeID, edgeKind, DirIn}]...)
	default:
		return nil, true
	}
	result = cloneEdgeNodes(result)
	sortEdgeNodes(result)
	return result, true
}

func (idx *GraphQueryIndex) GetNode(nodeID string) (*models.Node, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	node, ok := idx.nodesByID[nodeID]
	if !ok {
		return nil, false
	}
	return cloneNode(node), true
}

func (idx *GraphQueryIndex) FindByStableID(stableID string) ([]*models.Node, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ids := idx.stableByID[stableID]
	if len(ids) == 0 {
		return nil, true
	}
	out := make([]*models.Node, 0, len(ids))
	for _, id := range ids {
		if node := idx.nodesByID[id]; node != nil {
			out = append(out, cloneNode(node))
		}
	}
	return out, true
}

func (idx *GraphQueryIndex) FindByName(name string) ([]*models.Node, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ids := idx.byName[name]
	if ids == nil {
		return nil, true
	}
	out := make([]*models.Node, 0, len(ids))
	for _, id := range ids {
		if node := idx.nodesByID[id]; node != nil {
			out = append(out, cloneNode(node))
		}
	}
	return out, true
}

func (idx *GraphQueryIndex) FindByPath(path string) ([]*models.Node, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	ids := idx.byPath[path]
	if ids == nil {
		return nil, true
	}
	out := make([]*models.Node, 0, len(ids))
	for _, id := range ids {
		if node := idx.nodesByID[id]; node != nil {
			out = append(out, cloneNode(node))
		}
	}
	return out, true
}

func (idx *GraphQueryIndex) AllNeighbors(nodeID string, direction Direction) ([]EdgeNode, bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	var result []EdgeNode
	switch direction {
	case DirOut:
		result = append(result, idx.outgoingAll[nodeID]...)
	case DirIn:
		result = append(result, idx.incomingAll[nodeID]...)
	case DirBoth:
		result = append(result, idx.outgoingAll[nodeID]...)
		result = append(result, idx.incomingAll[nodeID]...)
	default:
		return nil, true
	}
	result = cloneEdgeNodes(result)
	sortEdgeNodes(result)
	return result, true
}

func (idx *GraphQueryIndex) Generation() (graphGeneration, indexGeneration uint64) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.graphGeneration, idx.indexGeneration
}

func (idx *GraphQueryIndex) State() (IndexState, error) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.state, idx.lastErr
}

func (idx *GraphQueryIndex) ProjectID() string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.projectID
}

func (idx *GraphQueryIndex) copyIDs(ids []string) []string {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}

func cloneEdgeNodes(items []EdgeNode) []EdgeNode {
	result := make([]EdgeNode, len(items))
	for i, item := range items {
		if item.Edge != nil {
			result[i].Edge = cloneEdge(item.Edge)
		}
		if item.Node != nil {
			result[i].Node = cloneNode(item.Node)
		}
	}
	return result
}

func sortEdgeNodes(items []EdgeNode) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Edge.ID != items[j].Edge.ID {
			return items[i].Edge.ID < items[j].Edge.ID
		}
		return items[i].Node.ID < items[j].Node.ID
	})
}

func sortedNodeValues(nodes map[string]*models.Node) []*models.Node {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]*models.Node, 0, len(ids))
	for _, id := range ids {
		result = append(result, cloneNode(nodes[id]))
	}
	return result
}

func pathKeys(node *models.Node) []string {
	seen := map[string]bool{}
	var keys []string
	for _, key := range []string{"path", "rel_path", "file_id"} {
		if value := strValue(node.Properties[key]); value != "" && !seen[value] {
			seen[value] = true
			keys = append(keys, value)
		}
	}
	return keys
}

func strValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
