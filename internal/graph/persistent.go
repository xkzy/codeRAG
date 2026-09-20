package graph

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"codergag/internal/ids"
	"codergag/internal/models"
)

const (
	DefaultCacheNodes = 1024
	DefaultCacheEdges = 1024
)

type nodeKey struct {
	kind     string
	identity map[string]any
}

type nodeSnapshot struct {
	id         string
	kind       string
	properties map[string]any
	identity   map[string]any
}

type edgeSnapshot struct {
	id         string
	kind       string
	fromID     string
	toID       string
	properties map[string]any
}

type journalSnapshot struct {
	dirty        map[string]*nodeSnapshot
	newEdges     map[string]*edgeSnapshot
	deletedNodes map[string]bool
	deletedEdges map[string]bool
	needsSave    bool
}

type persistentCache struct {
	nodes     map[string]*models.Node
	edges     map[string]*models.Edge
	nodeOrder []string
	edgeOrder []string
	maxNodes  int
	maxEdges  int
	mu        sync.Mutex
}

func newPersistentCache(maxNodes, maxEdges int) *persistentCache {
	if maxNodes <= 0 {
		maxNodes = DefaultCacheNodes
	}
	if maxEdges <= 0 {
		maxEdges = DefaultCacheEdges
	}
	return &persistentCache{
		nodes:     make(map[string]*models.Node),
		edges:     make(map[string]*models.Edge),
		nodeOrder: make([]string, 0, maxNodes),
		edgeOrder: make([]string, 0, maxEdges),
		maxNodes:  maxNodes,
		maxEdges:  maxEdges,
	}
}

func (c *persistentCache) getNode(id string) (*models.Node, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, ok := c.nodes[id]
	if !ok {
		return nil, false
	}
	c.promoteNode(id)
	return cloneNode(n), true
}

func (c *persistentCache) putNode(n *models.Node) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxNodes <= 0 {
		return
	}
	id := n.ID
	if _, exists := c.nodes[id]; exists {
		c.promoteNode(id)
		c.nodes[id] = cloneNode(n)
		return
	}
	if len(c.nodes) >= c.maxNodes {
		c.evictOldestNode()
	}
	c.nodes[n.ID] = cloneNode(n)
	c.nodeOrder = append(c.nodeOrder, n.ID)
}

func (c *persistentCache) promoteNode(id string) {
	for i, v := range c.nodeOrder {
		if v == id {
			c.nodeOrder = append(c.nodeOrder[:i], c.nodeOrder[i+1:]...)
			c.nodeOrder = append(c.nodeOrder, id)
			return
		}
	}
}

func (c *persistentCache) evictOldestNode() {
	if len(c.nodeOrder) == 0 {
		return
	}
	id := c.nodeOrder[0]
	c.nodeOrder = c.nodeOrder[1:]
	delete(c.nodes, id)
}

func (c *persistentCache) removeNode(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.nodes, id)
	for i, v := range c.nodeOrder {
		if v == id {
			c.nodeOrder = append(c.nodeOrder[:i], c.nodeOrder[i+1:]...)
			break
		}
	}
}

func (c *persistentCache) getEdge(id string) (*models.Edge, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.edges[id]
	if !ok {
		return nil, false
	}
	c.promoteEdge(id)
	return cloneEdge(e), true
}

func (c *persistentCache) putEdge(e *models.Edge) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.maxEdges <= 0 {
		return
	}
	if _, exists := c.edges[e.ID]; exists {
		c.promoteEdge(e.ID)
		c.edges[e.ID] = cloneEdge(e)
		return
	}
	if len(c.edges) >= c.maxEdges {
		c.evictOldestEdge()
	}
	c.edges[e.ID] = cloneEdge(e)
	c.edgeOrder = append(c.edgeOrder, e.ID)
}

func (c *persistentCache) promoteEdge(id string) {
	for i, v := range c.edgeOrder {
		if v == id {
			c.edgeOrder = append(c.edgeOrder[:i], c.edgeOrder[i+1:]...)
			c.edgeOrder = append(c.edgeOrder, id)
			return
		}
	}
}

func (c *persistentCache) evictOldestEdge() {
	if len(c.edgeOrder) == 0 {
		return
	}
	id := c.edgeOrder[0]
	c.edgeOrder = c.edgeOrder[1:]
	delete(c.edges, id)
}

func (c *persistentCache) removeEdge(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.edges, id)
	for i, v := range c.edgeOrder {
		if v == id {
			c.edgeOrder = append(c.edgeOrder[:i], c.edgeOrder[i+1:]...)
			break
		}
	}
}

func (c *persistentCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodes = make(map[string]*models.Node)
	c.edges = make(map[string]*models.Edge)
	c.nodeOrder = c.nodeOrder[:0]
	c.edgeOrder = c.edgeOrder[:0]
}

func (c *persistentCache) nodeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.nodes)
}

func (c *persistentCache) edgeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.edges)
}

func cloneNode(n *models.Node) *models.Node {
	props := make(map[string]any, len(n.Properties))
	for k, v := range n.Properties {
		props[k] = v
	}
	return &models.Node{
		ID:         n.ID,
		Kind:       n.Kind,
		Properties: props,
	}
}

func cloneEdge(e *models.Edge) *models.Edge {
	props := make(map[string]any, len(e.Properties))
	for k, v := range e.Properties {
		props[k] = v
	}
	return &models.Edge{
		ID:         e.ID,
		Kind:       e.Kind,
		FromID:     e.FromID,
		ToID:       e.ToID,
		Properties: props,
	}
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	r := make(map[string]any, len(m))
	for k, v := range m {
		r[k] = v
	}
	return r
}

func cloneIdentity(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	r := make(map[string]any, len(m))
	for k, v := range m {
		r[k] = v
	}
	return r
}

type PersistentGraphRepository struct {
	mu           sync.Mutex
	dbPath       string
	cache        *persistentCache
	mem          *MemoryGraphRepository
	dirty        map[string]*nodeSnapshot
	newEdges     map[string]*edgeSnapshot
	deletedNodes map[string]bool
	deletedEdges map[string]bool
	needsSave    bool
	stamp        fileStamp
	cachedState  *repoState
}

type fileStamp struct {
	modTime time.Time
	size    int64
}

func stampOf(path string) fileStamp {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{fi.ModTime(), fi.Size()}
}

func NewPersistentRepository(dbPath string) (*PersistentGraphRepository, error) {
	return NewPersistentRepositoryWithCache(dbPath, DefaultCacheNodes, DefaultCacheEdges)
}

func NewPersistentRepositoryWithCache(dbPath string, maxNodes, maxEdges int) (*PersistentGraphRepository, error) {
	r := &PersistentGraphRepository{
		dbPath:       dbPath,
		cache:        newPersistentCache(maxNodes, maxEdges),
		dirty:        make(map[string]*nodeSnapshot),
		newEdges:     make(map[string]*edgeSnapshot),
		deletedNodes: make(map[string]bool),
		deletedEdges: make(map[string]bool),
	}
	if dbPath == "" {
		r.mem = NewMemoryGraphRepository()
	}
	if dbPath != "" {
		if err := r.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("load graph: %w", err)
		}
	}
	return r, nil
}

func NewPersistentRepositoryWithCacheSize(dbPath string, nodeLimit, edgeLimit int) (*PersistentGraphRepository, error) {
	return NewPersistentRepositoryWithCache(dbPath, nodeLimit, edgeLimit)
}

func (r *PersistentGraphRepository) load() error {
	if _, err := os.Stat(r.dbPath); err != nil {
		return err
	}
	unlock, err := lockFile(r.dbPath + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	st, migrated, err := readState(r.dbPath)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cachedState = st
	r.cache.clear()
	r.stamp = stampOf(r.dbPath)
	r.needsSave = migrated
	return nil
}

func (r *PersistentGraphRepository) UpsertNode(kind string, identity, properties map[string]any) (*models.Node, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.upsertNodeInMemory(kind, identity, properties)
	}
	st, err := r.readEffectiveStateLocked()
	if err != nil {
		return nil, err
	}
	existing := r.findNodeByIdentity(st, kind, identity)
	if existing != nil {
		node := cloneNode(existing)
		for k, v := range properties {
			node.Properties[k] = v
		}
		node.Properties["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
		r.cache.putNode(node)
		var idMap map[string]any
		if snap, ok := r.dirty[existing.ID]; ok {
			idMap = snap.identity
		} else {
			idMap = identity
		}
		r.dirty[node.ID] = &nodeSnapshot{
			id:         existing.ID,
			kind:       kind,
			properties: node.Properties,
			identity:   cloneIdentity(idMap),
		}
		return node, nil
	}
	node := models.NewNode(kind, identity, properties)
	node.Properties["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	r.cache.putNode(node)
	r.dirty[node.ID] = &nodeSnapshot{
		id:         node.ID,
		kind:       kind,
		properties: node.Properties,
		identity:   cloneIdentity(identity),
	}
	return node, nil
}

func (r *PersistentGraphRepository) upsertNodeInMemory(kind string, identity, properties map[string]any) (*models.Node, error) {
	return r.mem.UpsertNode(kind, identity, properties)
}

func (r *PersistentGraphRepository) findNodeByIdentity(st *repoState, kind string, identity map[string]any) *models.Node {
	for id, gn := range st.Nodes {
		if gn.Kind != kind {
			continue
		}
		match := true
		for k, v := range identity {
			if !reflect.DeepEqual(gn.Properties[k], v) {
				match = false
				break
			}
		}
		if match {
			return &models.Node{
				ID:         id,
				Kind:       gn.Kind,
				Properties: cloneMap(gn.Properties),
			}
		}
	}
	return nil
}

func (r *PersistentGraphRepository) Link(kind, fromID, toID string, properties map[string]any) (*models.Edge, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.linkInMemory(kind, fromID, toID, properties)
	}
	st, err := r.readEffectiveStateLocked()
	if err != nil {
		return nil, err
	}
	for _, ge := range st.Edges {
		if ge.Kind != kind {
			continue
		}
		fromMatch := (ge.FromID == fromID)
		if fromID == "" {
			fromMatch = true
		}
		toMatch := (ge.ToID == toID)
		if toID == "" {
			toMatch = true
		}
		if fromMatch && toMatch && reflect.DeepEqual(ge.Properties, properties) {
			edge := &models.Edge{
				ID:         ge.ID,
				Kind:       ge.Kind,
				FromID:     ge.FromID,
				ToID:       ge.ToID,
				Properties: cloneMap(ge.Properties),
			}
			r.cache.putEdge(edge)
			return edge, nil
		}
	}
	edge := models.NewEdge(kind, fromID, toID, properties)
	r.cache.putEdge(edge)
	r.newEdges[edge.ID] = &edgeSnapshot{
		id:         edge.ID,
		kind:       kind,
		fromID:     fromID,
		toID:       toID,
		properties: cloneMap(properties),
	}
	return edge, nil
}

func (r *PersistentGraphRepository) linkInMemory(kind, fromID, toID string, properties map[string]any) (*models.Edge, error) {
	return r.mem.Link(kind, fromID, toID, properties)
}

func (r *PersistentGraphRepository) RemoveNodes(nodeIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.RemoveNodes(nodeIDs)
	}
	idSet := make(map[string]bool, len(nodeIDs))
	for _, id := range nodeIDs {
		idSet[id] = true
	}
	for id := range idSet {
		r.cache.removeNode(id)
		delete(r.dirty, id)
		r.deletedNodes[id] = true
	}
	for eid, e := range r.newEdges {
		if idSet[e.fromID] || idSet[e.toID] {
			delete(r.newEdges, eid)
			r.cache.removeEdge(eid)
		}
	}
	return nil
}

func (r *PersistentGraphRepository) RemoveEdges(edgeIDs []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.RemoveEdges(edgeIDs)
	}
	idSet := make(map[string]bool, len(edgeIDs))
	for _, id := range edgeIDs {
		idSet[id] = true
	}
	for id := range idSet {
		r.cache.removeEdge(id)
		delete(r.newEdges, id)
		r.deletedEdges[id] = true
	}
	return nil
}

func (r *PersistentGraphRepository) journalEmptyLocked() bool {
	return !r.needsSave && len(r.dirty) == 0 && len(r.deletedNodes) == 0 && len(r.newEdges) == 0 && len(r.deletedEdges) == 0
}

func (r *PersistentGraphRepository) clearJournalLocked() {
	r.needsSave = false
	r.dirty = make(map[string]*nodeSnapshot)
	r.deletedNodes = make(map[string]bool)
	r.newEdges = make(map[string]*edgeSnapshot)
	r.deletedEdges = make(map[string]bool)
}

func readState(path string) (state *repoState, migrated bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &repoState{SchemaVersion: SchemaVersion, Nodes: map[string]gobNode{}, Edges: map[string]gobEdge{}}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var st repoState
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&st); err != nil {
		return nil, false, err
	}
	if st.SchemaVersion > SchemaVersion {
		return nil, false, fmt.Errorf("graph file schema %d is newer than this build supports (%d); upgrade codeRAG",
			st.SchemaVersion, SchemaVersion)
	}
	if st.Nodes == nil {
		st.Nodes = map[string]gobNode{}
	}
	for key, gn := range st.Nodes {
		if gn.Properties == nil {
			gn.Properties = map[string]any{}
		}
		if id, _ := gn.Properties["id"].(string); id == "" {
			gn.Properties["id"] = key
		}
		if _, ok := gn.Properties["created_at"].(string); !ok {
			gn.Properties["created_at"] = time.Now().UTC().Format(time.RFC3339)
		}
		if _, ok := gn.Properties["updated_at"].(string); !ok {
			gn.Properties["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		}
		st.Nodes[key] = gn
	}
	edges := make(map[string]gobEdge, len(st.Edges))
	for _, ge := range st.Edges {
		if ge.ID == "" {
			ge.ID = models.NewID()
		}
		edges[ge.ID] = ge
	}
	st.Edges = edges
	if st.SchemaVersion < SchemaVersion {
		tmp := NewMemoryGraphRepository()
		installState(tmp, &st)
		for v := st.SchemaVersion; v < SchemaVersion; v++ {
			if err := migrations[v](tmp); err != nil {
				return nil, false, fmt.Errorf("migrate graph schema %d -> %d: %w", v, v+1, err)
			}
		}
		st = *snapshotState(tmp)
		migrated = true
	}
	return &st, migrated, nil
}

func (r *PersistentGraphRepository) readEffectiveStateLocked() (*repoState, error) {
	if r.dbPath == "" {
		return &repoState{SchemaVersion: SchemaVersion, Nodes: map[string]gobNode{}, Edges: map[string]gobEdge{}}, nil
	}
	currentStamp := stampOf(r.dbPath)
	if r.cachedState != nil && currentStamp == r.stamp {
		// Clone cached state to avoid concurrent modification
		st := cloneRepoState(r.cachedState)
		r.mergeJournalSnapshotLocked(st)
		return st, nil
	}
	st, _, err := readState(r.dbPath)
	if err != nil {
		return nil, err
	}
	r.cachedState = st
	r.stamp = currentStamp
	r.mergeJournalSnapshotLocked(st)
	return st, nil
}

func cloneRepoState(src *repoState) *repoState {
	dst := &repoState{
		SchemaVersion: src.SchemaVersion,
		Nodes:         make(map[string]gobNode, len(src.Nodes)),
		Edges:         make(map[string]gobEdge, len(src.Edges)),
	}
	for k, v := range src.Nodes {
		dst.Nodes[k] = v
	}
	for k, v := range src.Edges {
		dst.Edges[k] = v
	}
	return dst
}

func (r *PersistentGraphRepository) mergeJournalSnapshotLocked(st *repoState) {
	for id := range r.deletedNodes {
		delete(st.Nodes, id)
		for eid, e := range st.Edges {
			if e.FromID == id || e.ToID == id {
				delete(st.Edges, eid)
			}
		}
	}
	for eid := range r.deletedEdges {
		delete(st.Edges, eid)
	}

	type idxKey struct{ kind, key string }
	index := map[idxKey]map[string]string{}
	lookup := func(kind string, identity map[string]any) string {
		names := make([]string, 0, len(identity))
		for k := range identity {
			names = append(names, k)
		}
		sort.Strings(names)
		ik := idxKey{kind, strings.Join(names, ",")}
		idx, ok := index[ik]
		if !ok {
			idx = map[string]string{}
			for id, gn := range st.Nodes {
				if gn.Kind != kind {
					continue
				}
				vals := make([]string, len(names))
				complete := true
				for i, n := range names {
					v, has := gn.Properties[n]
					if !has {
						complete = false
						break
					}
					vals[i] = canon(v)
				}
				if complete {
					idx[strings.Join(vals, "\x00")] = id
				}
			}
			index[ik] = idx
		}
		vals := make([]string, len(names))
		for i, n := range names {
			vals[i] = canon(identity[n])
		}
		return idx[strings.Join(vals, "\x00")]
	}

	remap := map[string]string{}
	ids := make([]string, 0, len(r.dirty))
	for id := range r.dirty {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		snap := r.dirty[id]
		props := make(map[string]any, len(snap.properties))
		for k, v := range snap.properties {
			props[k] = v
		}
		target := snap.id
		if _, onDisk := st.Nodes[snap.id]; !onDisk && len(snap.identity) > 0 {
			if match := lookup(snap.kind, snap.identity); match != "" {
				target = match
				remap[id] = match
				if c, ok := st.Nodes[match].Properties["created_at"]; ok {
					props["created_at"] = c
				}
			}
		}
		props["id"] = target
		st.Nodes[target] = gobNode{Kind: snap.kind, Properties: props}
	}

	byTriple := map[string][]string{}
	for eid, e := range st.Edges {
		k := e.Kind + "|" + e.FromID + "|" + e.ToID
		byTriple[k] = append(byTriple[k], eid)
	}
	eids := make([]string, 0, len(r.newEdges))
	for id := range r.newEdges {
		eids = append(eids, id)
	}
	sort.Strings(eids)
	for _, eid := range eids {
		snap := r.newEdges[eid]
		from, to := snap.fromID, snap.toID
		if m, ok := remap[from]; ok {
			from = m
		}
		if m, ok := remap[to]; ok {
			to = m
		}
		if _, ok := st.Nodes[from]; !ok {
			continue
		}
		if _, ok := st.Nodes[to]; !ok {
			continue
		}
		triple := snap.kind + "|" + from + "|" + to
		dup := false
		for _, other := range byTriple[triple] {
			if reflect.DeepEqual(st.Edges[other].Properties, snap.properties) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		st.Edges[eid] = gobEdge{
			ID:         eid,
			Kind:       snap.kind,
			FromID:     from,
			ToID:       to,
			Properties: snap.properties,
		}
		byTriple[triple] = append(byTriple[triple], eid)
	}
}

func (r *PersistentGraphRepository) GetNode(nodeID string) (*models.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.GetNode(nodeID)
	}
	if n, ok := r.cache.getNode(nodeID); ok {
		return n, nil
	}
	st, err := r.readEffectiveStateLocked()
	if err != nil {
		return nil, err
	}
	if gn, ok := st.Nodes[nodeID]; ok {
		n := &models.Node{
			ID:         nodeID,
			Kind:       gn.Kind,
			Properties: cloneMap(gn.Properties),
		}
		r.cache.putNode(n)
		return n, nil
	}
	if snap, ok := r.dirty[nodeID]; ok {
		n := &models.Node{
			ID:         snap.id,
			Kind:       snap.kind,
			Properties: cloneMap(snap.properties),
		}
		r.cache.putNode(n)
		return n, nil
	}
	return nil, ErrNotFound
}

func (r *PersistentGraphRepository) FindNodes(kind string, filters map[string]any) ([]*models.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.FindNodes(kind, filters)
	}
	st, err := r.readEffectiveStateLocked()
	if err != nil {
		return nil, err
	}
	var results []*models.Node
	for id, gn := range st.Nodes {
		if kind != "" && gn.Kind != kind {
			continue
		}
		match := true
		for k, v := range filters {
			if !reflect.DeepEqual(gn.Properties[k], v) {
				match = false
				break
			}
		}
		if match {
			n := &models.Node{
				ID:         id,
				Kind:       gn.Kind,
				Properties: cloneMap(gn.Properties),
			}
			results = append(results, n)
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	for _, n := range results {
		r.cache.putNode(n)
	}
	return results, nil
}

func (r *PersistentGraphRepository) Neighbors(nodeID, edgeKind string, direction Direction) ([]EdgeNode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.Neighbors(nodeID, edgeKind, direction)
	}
	st, err := r.readEffectiveStateLocked()
	if err != nil {
		return nil, err
	}
	var results []EdgeNode
	for _, ge := range st.Edges {
		if edgeKind != "" && ge.Kind != edgeKind {
			continue
		}
		switch direction {
		case DirOut:
			if ge.FromID == nodeID {
				if node, ok := st.Nodes[ge.ToID]; ok {
					results = append(results, EdgeNode{
						Edge: &models.Edge{
							ID:         ge.ID,
							Kind:       ge.Kind,
							FromID:     ge.FromID,
							ToID:       ge.ToID,
							Properties: cloneMap(ge.Properties),
						},
						Node: &models.Node{
							ID:         ge.ToID,
							Kind:       node.Kind,
							Properties: cloneMap(node.Properties),
						},
					})
				}
			}
		case DirIn:
			if ge.ToID == nodeID {
				if node, ok := st.Nodes[ge.FromID]; ok {
					results = append(results, EdgeNode{
						Edge: &models.Edge{
							ID:         ge.ID,
							Kind:       ge.Kind,
							FromID:     ge.FromID,
							ToID:       ge.ToID,
							Properties: cloneMap(ge.Properties),
						},
						Node: &models.Node{
							ID:         ge.FromID,
							Kind:       node.Kind,
							Properties: cloneMap(node.Properties),
						},
					})
				}
			}
		case DirBoth:
			if ge.FromID == nodeID {
				if node, ok := st.Nodes[ge.ToID]; ok {
					results = append(results, EdgeNode{
						Edge: &models.Edge{
							ID:         ge.ID,
							Kind:       ge.Kind,
							FromID:     ge.FromID,
							ToID:       ge.ToID,
							Properties: cloneMap(ge.Properties),
						},
						Node: &models.Node{
							ID:         ge.ToID,
							Kind:       node.Kind,
							Properties: cloneMap(node.Properties),
						},
					})
				}
			}
			if ge.ToID == nodeID {
				if node, ok := st.Nodes[ge.FromID]; ok {
					results = append(results, EdgeNode{
						Edge: &models.Edge{
							ID:         ge.ID,
							Kind:       ge.Kind,
							FromID:     ge.FromID,
							ToID:       ge.ToID,
							Properties: cloneMap(ge.Properties),
						},
						Node: &models.Node{
							ID:         ge.FromID,
							Kind:       node.Kind,
							Properties: cloneMap(node.Properties),
						},
					})
				}
			}
		}
	}
	for _, en := range results {
		r.cache.putNode(en.Node)
		r.cache.putEdge(en.Edge)
	}
	return results, nil
}

func (r *PersistentGraphRepository) QueryReadonly(query string, params map[string]any) ([]map[string]any, error) {
	st, err := r.readEffectiveStateLocked()
	if err != nil {
		return nil, err
	}
	return parseAndExecuteQuery(query, params, st)
}

func parseAndExecuteQuery(query string, params map[string]any, st *repoState) ([]map[string]any, error) {
	q := strings.TrimSpace(query)
	upper := strings.ToUpper(q)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "MATCH") {
		return nil, errors.New("query_graph accepts one read-only SELECT or MATCH query")
	}
	if strings.Contains(upper, ";") {
		return nil, errors.New("query_graph accepts one read-only SELECT or MATCH query")
	}
	for _, kw := range []string{"INSERT", "UPDATE", "DELETE", "CREATE", "DROP", "ALTER", "TRUNCATE"} {
		if regexp.MustCompile(`\b` + kw + `\b`).MatchString(upper) {
			return nil, errors.New("query_graph accepts one read-only SELECT or MATCH query")
		}
	}

	fromRe := regexp.MustCompile(`(?i)\bFROM\s+(\w+)\b`)
	fromMatch := fromRe.FindStringSubmatch(q)
	kind := ""
	if fromMatch != nil {
		kind = fromMatch[1]
		if err := ValidateKind(kind); err != nil {
			return nil, err
		}
	}

	whereRe := regexp.MustCompile(`(?i)\bWHERE\s+(.+?)(?:\s+LIMIT\s+\d+|$)`)
	whereMatch := whereRe.FindStringSubmatch(q)

	conds := []func(*models.Node) bool{}
	if whereMatch != nil {
		for _, clause := range splitWhere(whereMatch[1]) {
			clause = strings.TrimSpace(clause)
			if clause == "" {
				continue
			}
			fn, err := parseCondition(clause, params)
			if err != nil {
				return nil, err
			}
			conds = append(conds, fn)
		}
	}

	limit := 1000
	limitRe := regexp.MustCompile(`(?i)\bLIMIT\s+(\d+)\b`)
	if m := limitRe.FindStringSubmatch(q); m != nil {
		if l, err := strconv.Atoi(m[1]); err == nil {
			limit = l
		}
	}

	var results []map[string]any
	for id, gn := range st.Nodes {
		if kind != "" && gn.Kind != kind {
			continue
		}
		node := &models.Node{
			ID:         id,
			Kind:       gn.Kind,
			Properties: cloneMap(gn.Properties),
		}
		ok := true
		for _, cond := range conds {
			if !cond(node) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		results = append(results, Present(node))
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func splitWhere(s string) []string {
	var parts []string
	// Handle both SQL AND and && 
	re := regexp.MustCompile(`\s+AND\s+|\s*&&\s*`)
	for _, part := range re.Split(s, -1) {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func parseCondition(clause string, params map[string]any) (func(*models.Node) bool, error) {
	clause = strings.TrimSpace(clause)

	// LIKE pattern: prop LIKE 'pattern'
	likeRe := regexp.MustCompile(`^\s*(\w+)\s+LIKE\s+('.+?')\s*$`)
	m := likeRe.FindStringSubmatch(clause)
	if m != nil {
		propName := m[1]
		pattern := m[2][1 : len(m[2])-1] // remove quotes
		// Convert SQL LIKE to Go regexp
		regexPattern := strings.ReplaceAll(pattern, "%", ".*")
		regexPattern = strings.ReplaceAll(regexPattern, "_", ".")
		re, err := regexp.Compile("^" + regexPattern + "$")
		if err != nil {
			return nil, fmt.Errorf("invalid LIKE pattern: %w", err)
		}
		return func(node *models.Node) bool {
			val := toString(node.Properties[propName])
			return re.MatchString(val)
		}, nil
	}

	// Equality pattern: prop = value
	eqRe := regexp.MustCompile(`^\s*(\w+)\s*=\s*(:\w+|'.+?')\s*$`)
	m = eqRe.FindStringSubmatch(clause)
	if m == nil {
		return nil, fmt.Errorf("unsupported condition: %s", clause)
	}
	propName := m[1]
	rawVal := m[2]
	var expected string
	if strings.HasPrefix(rawVal, ":") {
		paramName := strings.TrimPrefix(rawVal, ":")
		val, ok := params[paramName]
		if !ok {
			return nil, fmt.Errorf("parameter %s not found", paramName)
		}
		expected = toString(val)
	} else {
		expected = rawVal[1 : len(rawVal)-1]
	}
	return func(node *models.Node) bool {
		return toString(node.Properties[propName]) == expected
	}, nil
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func (r *PersistentGraphRepository) Refresh() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" || !r.journalEmptyLocked() || stampOf(r.dbPath) == r.stamp {
		return nil
	}
	unlock, err := lockFile(r.dbPath + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	st, migrated, err := readState(r.dbPath)
	if err != nil {
		return err
	}
	r.cachedState = st
	r.cache.clear()
	r.mergeJournalSnapshotLocked(st)
	r.stamp = stampOf(r.dbPath)
	r.needsSave = migrated
	return nil
}

func (r *PersistentGraphRepository) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" || r.journalEmptyLocked() {
		return nil
	}
	if dir := filepath.Dir(r.dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	unlock, err := lockFile(r.dbPath + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	st, _, err := readState(r.dbPath)
	if err != nil {
		return err
	}
	r.mergeJournalSnapshotLocked(st)

	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(st); err != nil {
		return err
	}
	tmp := r.dbPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, r.dbPath); err != nil {
		return err
	}
	r.cachedState = st
	r.cache.clear()
	r.stamp = stampOf(r.dbPath)
	r.clearJournalLocked()
	return nil
}

func (r *PersistentGraphRepository) Close() error {
	if err := r.Save(); err != nil {
		return err
	}
	r.cache.clear()
	return nil
}

func (r *PersistentGraphRepository) Counts() (nodes, edges map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.Counts()
	}
	st, err := r.readEffectiveStateLocked()
	if err != nil {
		return nil, nil
	}
	nodes = make(map[string]int)
	edges = make(map[string]int)
	for _, n := range st.Nodes {
		nodes[n.Kind]++
	}
	for _, e := range st.Edges {
		edges[e.Kind]++
	}
	return nodes, edges
}

func (r *PersistentGraphRepository) EdgesOfKind(kind string) []EdgeRef {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.EdgesOfKind(kind)
	}
	st, err := r.readEffectiveStateLocked()
	if err != nil {
		return nil
	}
	var out []EdgeRef
	for _, e := range st.Edges {
		if e.Kind == kind {
			out = append(out, EdgeRef{From: e.FromID, To: e.ToID})
		}
	}
	return out
}

func (r *PersistentGraphRepository) CacheNodes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.NodeCount()
	}
	return r.cache.nodeCount()
}

func (r *PersistentGraphRepository) CacheEdges() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return r.mem.EdgeCount()
	}
	return r.cache.edgeCount()
}

func canon(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func installState(m *MemoryGraphRepository, st *repoState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes = make(map[string]*models.Node, len(st.Nodes))
	m.edges = make(map[string]*models.Edge, len(st.Edges))
	for id, gn := range st.Nodes {
		m.nodes[id] = &models.Node{ID: id, Kind: gn.Kind, Properties: cloneMap(gn.Properties)}
	}
	for id, ge := range st.Edges {
		m.edges[id] = &models.Edge{ID: id, Kind: ge.Kind, FromID: ge.FromID, ToID: ge.ToID, Properties: cloneMap(ge.Properties)}
	}
}

func snapshotState(m *MemoryGraphRepository) *repoState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	st := &repoState{
		SchemaVersion: SchemaVersion,
		Nodes:         make(map[string]gobNode, len(m.nodes)),
		Edges:         make(map[string]gobEdge, len(m.edges)),
	}
	for id, n := range m.nodes {
		st.Nodes[id] = gobNode{Kind: n.Kind, Properties: cloneMap(n.Properties)}
	}
	for id, e := range m.edges {
		st.Edges[id] = gobEdge{ID: id, Kind: e.Kind, FromID: e.FromID, ToID: e.ToID, Properties: cloneMap(e.Properties)}
	}
	return st
}

const SchemaVersion = 2

var migrations = []func(r *MemoryGraphRepository) error{
	func(r *MemoryGraphRepository) error {
		legacy, err := r.FindNodes("Type", nil)
		if err != nil {
			return err
		}
		ids := make([]string, 0, len(legacy))
		for _, n := range legacy {
			ids = append(ids, n.ID)
		}
		return r.RemoveNodes(ids)
	},
	migrateStableIDs,
}

func migrateStableIDs(r *MemoryGraphRepository) error {
	roots := map[string]string{}
	projects, _ := r.FindNodes("Project", nil)
	for _, p := range projects {
		if id, _ := p.Properties["id"].(string); id != "" {
			roots[id], _ = p.Properties["path"].(string)
		}
	}
	str := func(n *models.Node, k string) string { s, _ := n.Properties[k].(string); return s }
	for _, kind := range []string{"Function", "Class", "Struct", "SourceFile"} {
		nodes, _ := r.FindNodes(kind, nil)
		for _, n := range nodes {
			if str(n, "stable_id") != "" {
				continue
			}
			rel := ids.Rel(roots[str(n, "project_id")], str(n, "path"))
			n.Properties["rel_path"] = rel
			switch kind {
			case "SourceFile":
				n.Properties["stable_id"] = ids.FileID(rel)
			default:
				label := str(n, "name")
				if owner := str(n, "owner"); owner != "" {
					label = owner + "." + label
				}
				prefix := map[string]string{"Function": ids.Func, "Class": ids.Class, "Struct": ids.Struct}[kind]
				n.Properties["stable_id"] = ids.Symbol(prefix, rel, label)
				n.Properties["qualified_name"] = rel + ":" + label
			}
		}
	}
	bins, _ := r.FindNodes("Binary", nil)
	for _, n := range bins {
		if str(n, "stable_id") == "" {
			n.Properties["stable_id"] = ids.BinaryID(str(n, "binary_id"))
		}
	}
	bfs, _ := r.FindNodes("BinaryFunction", nil)
	for _, n := range bfs {
		if str(n, "stable_id") == "" {
			n.Properties["stable_id"] = ids.BinFuncID(str(n, "binary_id"), str(n, "address"))
		}
	}
	return nil
}

type repoState struct {
	SchemaVersion int
	Nodes         map[string]gobNode
	Edges         map[string]gobEdge
}

type gobNode struct {
	Kind       string
	Properties map[string]any
}

type gobEdge struct {
	ID         string
	Kind       string
	FromID     string
	ToID       string
	Properties map[string]any
}
