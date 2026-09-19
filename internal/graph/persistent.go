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
	"time"

	"sync"

	"codergag/internal/ids"
	"codergag/internal/models"
)

// nodeKey remembers how a node was upserted so a merge can recognise the same
// logical node created independently by another process.
type nodeKey struct {
	kind     string
	identity map[string]any
}

// PersistentGraphRepository is the in-memory graph plus a gob file that several
// processes may share. Changes are journaled; Save merges them into whatever is
// on disk under an exclusive file lock, so concurrent writers do not clobber
// each other, and Refresh picks up other processes' writes.
type PersistentGraphRepository struct {
	*MemoryGraphRepository
	mu           sync.Mutex
	dbPath       string
	dirty        map[string]nodeKey
	deletedNodes map[string]bool
	newEdges     map[string]bool
	deletedEdges map[string]bool
	needsSave    bool // e.g. a schema migration rewrote the graph
	stamp        fileStamp
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
	r := &PersistentGraphRepository{
		MemoryGraphRepository: NewMemoryGraphRepository(),
		dbPath:                dbPath,
		dirty:                 map[string]nodeKey{},
		deletedNodes:          map[string]bool{},
		newEdges:              map[string]bool{},
		deletedEdges:          map[string]bool{},
	}
	if dbPath != "" {
		if err := r.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("load graph: %w", err)
		}
	}
	return r, nil
}

func (r *PersistentGraphRepository) UpsertNode(kind string, identity, properties map[string]any) (*models.Node, error) {
	node, err := r.MemoryGraphRepository.UpsertNode(kind, identity, properties)
	if err != nil {
		return nil, err
	}
	id := make(map[string]any, len(identity))
	for k, v := range identity {
		id[k] = v
	}
	r.mu.Lock()
	r.dirty[node.ID] = nodeKey{kind: kind, identity: id}
	delete(r.deletedNodes, node.ID)
	r.mu.Unlock()
	return node, nil
}

func (r *PersistentGraphRepository) Link(kind, fromID, toID string, properties map[string]any) (*models.Edge, error) {
	edge, err := r.MemoryGraphRepository.Link(kind, fromID, toID, properties)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.newEdges[edge.ID] = true
	delete(r.deletedEdges, edge.ID)
	r.mu.Unlock()
	return edge, nil
}

func (r *PersistentGraphRepository) RemoveNodes(nodeIDs []string) error {
	if err := r.MemoryGraphRepository.RemoveNodes(nodeIDs); err != nil {
		return err
	}
	r.mu.Lock()
	for _, id := range nodeIDs {
		delete(r.dirty, id)
		r.deletedNodes[id] = true
	}
	r.mu.Unlock()
	return nil
}

func (r *PersistentGraphRepository) RemoveEdges(edgeIDs []string) error {
	if err := r.MemoryGraphRepository.RemoveEdges(edgeIDs); err != nil {
		return err
	}
	r.mu.Lock()
	for _, id := range edgeIDs {
		delete(r.newEdges, id)
		r.deletedEdges[id] = true
	}
	r.mu.Unlock()
	return nil
}

func (r *PersistentGraphRepository) journalEmptyLocked() bool {
	return !r.needsSave && len(r.dirty) == 0 && len(r.deletedNodes) == 0 && len(r.newEdges) == 0 && len(r.deletedEdges) == 0
}

func (r *PersistentGraphRepository) clearJournalLocked() {
	r.needsSave = false
	r.dirty = map[string]nodeKey{}
	r.deletedNodes = map[string]bool{}
	r.newEdges = map[string]bool{}
	r.deletedEdges = map[string]bool{}
}

// readState decodes the graph file, upgrading older schemas in memory. A missing
// file yields an empty state.
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
	now := time.Now().UTC().Format(time.RFC3339)
	for key, gn := range st.Nodes {
		if gn.Properties == nil {
			gn.Properties = map[string]any{}
		}
		if id, _ := gn.Properties["id"].(string); id == "" {
			gn.Properties["id"] = key
		}
		if _, ok := gn.Properties["created_at"].(string); !ok {
			gn.Properties["created_at"] = now
		}
		if _, ok := gn.Properties["updated_at"].(string); !ok {
			gn.Properties["updated_at"] = now
		}
		st.Nodes[key] = gn
	}
	edges := make(map[string]gobEdge, len(st.Edges))
	for _, ge := range st.Edges {
		if ge.ID == "" {
			ge.ID = models.NewID() // pre-ID files: assigned once, persisted on the next save
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

func installState(m *MemoryGraphRepository, st *repoState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nodes = make(map[string]*models.Node, len(st.Nodes))
	m.edges = make(map[string]*models.Edge, len(st.Edges))
	for id, gn := range st.Nodes {
		m.nodes[id] = &models.Node{ID: id, Kind: gn.Kind, Properties: gn.Properties}
	}
	for id, ge := range st.Edges {
		m.edges[id] = &models.Edge{ID: id, Kind: ge.Kind, FromID: ge.FromID, ToID: ge.ToID, Properties: ge.Properties}
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
		st.Nodes[id] = gobNode{Kind: n.Kind, Properties: n.Properties}
	}
	for id, e := range m.edges {
		st.Edges[id] = gobEdge{ID: id, Kind: e.Kind, FromID: e.FromID, ToID: e.ToID, Properties: e.Properties}
	}
	return st
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
	installState(r.MemoryGraphRepository, st)
	r.stamp = stampOf(r.dbPath)
	r.needsSave = migrated
	return nil
}

// Refresh reloads the graph if another process changed the file and this
// process has no unsaved changes.
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
	installState(r.MemoryGraphRepository, st)
	r.stamp = stampOf(r.dbPath)
	r.needsSave = migrated
	return nil
}

// Save merges this process's journaled changes into the file and adopts the
// merged result, so changes other processes made since the last sync are kept.
// Concurrent writers are serialized by an exclusive file lock.
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

	disk, _, err := readState(r.dbPath)
	if err != nil {
		return err
	}
	r.mergeJournalLocked(disk)

	buf := new(bytes.Buffer)
	if err := gob.NewEncoder(buf).Encode(disk); err != nil {
		return err
	}
	tmp := r.dbPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, r.dbPath); err != nil {
		return err
	}
	installState(r.MemoryGraphRepository, disk)
	r.stamp = stampOf(r.dbPath)
	r.clearJournalLocked()
	return nil
}

func canon(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// mergeJournalLocked applies this process's changes to disk (last writer wins
// per node). A new node whose identity matches a node another process already
// wrote is folded into that node instead of duplicated; edges are remapped and
// de-duplicated accordingly.
func (r *PersistentGraphRepository) mergeJournalLocked(disk *repoState) {
	r.MemoryGraphRepository.mu.RLock()
	defer r.MemoryGraphRepository.mu.RUnlock()

	type idxKey struct{ kind, keys string }
	indexes := map[idxKey]map[string]string{}
	lookup := func(kind string, identity map[string]any) string {
		names := make([]string, 0, len(identity))
		for k := range identity {
			names = append(names, k)
		}
		sort.Strings(names)
		ik := idxKey{kind, strings.Join(names, ",")}
		idx, ok := indexes[ik]
		if !ok {
			idx = map[string]string{}
			for id, gn := range disk.Nodes {
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
			indexes[ik] = idx
		}
		vals := make([]string, len(names))
		for i, n := range names {
			vals[i] = canon(identity[n])
		}
		return idx[strings.Join(vals, "\x00")]
	}

	for id := range r.deletedNodes {
		delete(disk.Nodes, id)
		for eid, e := range disk.Edges {
			if e.FromID == id || e.ToID == id {
				delete(disk.Edges, eid)
			}
		}
	}
	for eid := range r.deletedEdges {
		delete(disk.Edges, eid)
	}

	remap := map[string]string{}
	ids := make([]string, 0, len(r.dirty))
	for id := range r.dirty {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n, ok := r.nodes[id]
		if !ok {
			continue
		}
		props := make(map[string]any, len(n.Properties))
		for k, v := range n.Properties {
			props[k] = v
		}
		target := id
		if _, onDisk := disk.Nodes[id]; !onDisk && len(r.dirty[id].identity) > 0 {
			if match := lookup(n.Kind, r.dirty[id].identity); match != "" {
				target = match
				remap[id] = match
				if c, ok := disk.Nodes[match].Properties["created_at"]; ok { // keep the first writer's created_at
					props["created_at"] = c
				}
			}
		}
		props["id"] = target
		disk.Nodes[target] = gobNode{Kind: n.Kind, Properties: props}
	}

	byTriple := map[string][]string{}
	for eid, e := range disk.Edges {
		k := e.Kind + "|" + e.FromID + "|" + e.ToID
		byTriple[k] = append(byTriple[k], eid)
	}
	eids := make([]string, 0, len(r.newEdges))
	for id := range r.newEdges {
		eids = append(eids, id)
	}
	sort.Strings(eids)
	for _, eid := range eids {
		e, ok := r.edges[eid]
		if !ok {
			continue
		}
		from, to := e.FromID, e.ToID
		if m, ok := remap[from]; ok {
			from = m
		}
		if m, ok := remap[to]; ok {
			to = m
		}
		if _, ok := disk.Nodes[from]; !ok {
			continue
		}
		if _, ok := disk.Nodes[to]; !ok {
			continue
		}
		triple := e.Kind + "|" + from + "|" + to
		dup := false
		for _, other := range byTriple[triple] {
			if reflect.DeepEqual(disk.Edges[other].Properties, e.Properties) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		disk.Edges[eid] = gobEdge{ID: eid, Kind: e.Kind, FromID: from, ToID: to, Properties: e.Properties}
		byTriple[triple] = append(byTriple[triple], eid)
	}
	disk.SchemaVersion = SchemaVersion
}

func (r *PersistentGraphRepository) Close() error {
	if err := r.Save(); err != nil {
		return err
	}
	return r.MemoryGraphRepository.Close()
}

// SchemaVersion is the on-disk format version. Files written before versioning
// existed decode as version 0. Bump it and append to migrations when node or
// edge semantics change in a way old files cannot be read as-is.
const SchemaVersion = 2

// migrations[i] upgrades a graph from schema version i to i+1.
var migrations = []func(r *MemoryGraphRepository) error{
	// 0 -> 1: drop the unlinked generic "Type" nodes written by the regex
	// indexer; Class and Struct nodes replace them on the next index.
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
	// 1 -> 2: add repo-relative stable IDs and make symbol identity
	// (qualified_name) relative, so a moved checkout updates nodes in place.
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

func (r *PersistentGraphRepository) QueryReadonly(query string, params map[string]any) ([]map[string]any, error) {
	return parseAndExecuteQuery(r.MemoryGraphRepository, query, params)
}

func parseAndExecuteQuery(r *MemoryGraphRepository, query string, params map[string]any) ([]map[string]any, error) {
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

	r.mu.RLock()
	defer r.mu.RUnlock()
	var results []map[string]any
	for _, node := range r.nodes {
		if kind != "" && node.Kind != kind {
			continue
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
	for _, part := range strings.Split(s, "&&") {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func parseCondition(clause string, params map[string]any) (func(*models.Node) bool, error) {
	eqRe := regexp.MustCompile(`^\s*(\w+)\s*=\s*(:\w+|'.+?')\s*$`)
	m := eqRe.FindStringSubmatch(clause)
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
		return fmt.Sprintf("%v", v)
	}
}
