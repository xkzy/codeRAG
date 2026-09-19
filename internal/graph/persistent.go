package graph

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"codergag/internal/models"
)

type PersistentGraphRepository struct {
	*MemoryGraphRepository
	mu       sync.Mutex
	dbPath   string
	modified bool
}

func NewPersistentRepository(dbPath string) (*PersistentGraphRepository, error) {
	r := &PersistentGraphRepository{
		MemoryGraphRepository: NewMemoryGraphRepository(),
		dbPath:                dbPath,
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
	r.setModified()
	return node, nil
}

func (r *PersistentGraphRepository) Link(kind, fromID, toID string, properties map[string]any) (*models.Edge, error) {
	edge, err := r.MemoryGraphRepository.Link(kind, fromID, toID, properties)
	if err != nil {
		return nil, err
	}
	r.setModified()
	return edge, nil
}

func (r *PersistentGraphRepository) RemoveNodes(nodeIDs []string) error {
	if err := r.MemoryGraphRepository.RemoveNodes(nodeIDs); err != nil {
		return err
	}
	r.setModified()
	return nil
}

func (r *PersistentGraphRepository) setModified() {
	r.mu.Lock()
	r.modified = true
	r.mu.Unlock()
}

func (r *PersistentGraphRepository) Save() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dbPath == "" {
		return nil
	}
	r.modified = false
	buf := new(bytes.Buffer)
	enc := gob.NewEncoder(buf)
	state := repoState{
		Nodes: make(map[string]gobNode, len(r.nodes)),
		Edges: make(map[string]gobEdge, len(r.edges)),
	}
	for id, node := range r.nodes {
		state.Nodes[id] = gobNode{Kind: node.Kind, Properties: node.Properties}
	}
	for id, edge := range r.edges {
		state.Edges[id] = gobEdge{Kind: edge.Kind, FromID: edge.FromID, ToID: edge.ToID, Properties: edge.Properties}
	}
	if err := enc.Encode(state); err != nil {
		return err
	}
	dir := filepath.Dir(r.dbPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	tmp := r.dbPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.dbPath)
}

type repoState struct {
	Nodes map[string]gobNode
	Edges map[string]gobEdge
}

type gobNode struct {
	Kind       string
	Properties map[string]any
}

type gobEdge struct {
	Kind       string
	FromID     string
	ToID       string
	Properties map[string]any
}

func (r *PersistentGraphRepository) load() error {
	data, err := os.ReadFile(r.dbPath)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	dec := gob.NewDecoder(bytes.NewReader(data))
	var state repoState
	if err := dec.Decode(&state); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, gn := range state.Nodes {
		id, _ := gn.Properties["id"].(string)
		if id == "" {
			id = models.NewID()
		}
		gn.Properties["created_at"] = now
		gn.Properties["updated_at"] = now
		node := &models.Node{ID: id, Kind: gn.Kind, Properties: gn.Properties}
		r.nodes[node.ID] = node
	}
	for _, ge := range state.Edges {
		edge := &models.Edge{
			ID:         models.NewID(),
			Kind:       ge.Kind,
			FromID:     ge.FromID,
			ToID:       ge.ToID,
			Properties: ge.Properties,
		}
		r.edges[edge.ID] = edge
	}
	return nil
}

func (r *PersistentGraphRepository) Close() error {
	if err := r.Save(); err != nil {
		return err
	}
	return r.MemoryGraphRepository.Close()
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
