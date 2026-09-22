//go:build postgresql

package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"codergag/internal/models"
	_ "github.com/lib/pq"
)

// SQLGraphRepository implements GraphRepository on top of a SQL database
// (PostgreSQL). Nodes and edges are stored in two tables with JSONB columns
// for properties, enabling rich indexing and filtering.
type SQLGraphRepository struct {
	db *sql.DB
	mu sync.Mutex
}

// NewSQLGraphRepository creates a new SQL-backed graph repository.
// The DSN should be a standard "postgres://" or "postgresql://" connection string.
func NewSQLGraphRepository(ctx context.Context, dsn string) (*SQLGraphRepository, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql open: %w", err)
	}
	db.SetMaxOpenConns(25)
	r := &SQLGraphRepository{db: db}
	if err := r.migrate(ctx); err != nil {
		return nil, fmt.Errorf("sql migrate: %w", err)
	}
	return r, nil
}

// NewSQLGraphRepositoryWithDB creates a SQL repository from an existing *sql.DB.
func NewSQLGraphRepositoryWithDB(db *sql.DB) *SQLGraphRepository {
	r := &SQLGraphRepository{db: db}
	return r
}

func (r *SQLGraphRepository) migrate(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var statements = []string{
		`CREATE TABLE IF NOT EXISTS graph_nodes (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			project_id TEXT NOT NULL,
			properties JSONB NOT NULL,
			created_at TEXT,
			updated_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_project ON graph_nodes(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_kind ON graph_nodes(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_identity ON graph_nodes USING gin ((properties->'identity'))`,
		`CREATE TABLE IF NOT EXISTS graph_edges (
			id TEXT PRIMARY KEY,
			kind TEXT NOT NULL,
			from_id TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
			to_id TEXT NOT NULL REFERENCES graph_nodes(id) ON DELETE CASCADE,
			properties JSONB NOT NULL,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_from ON graph_edges(from_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_to ON graph_edges(to_id)`,
		`CREATE INDEX IF NOT EXISTS idx_edges_kind ON graph_edges(kind)`,
	}
	for _, stmt := range statements {
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLGraphRepository) UpsertNode(kind string, identity, properties map[string]any) (*models.Node, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	ctx := context.Background()
	r.mu.Lock()
	defer r.mu.Unlock()

	props := make(map[string]any, len(identity)+len(properties)+3)
	for k, v := range identity {
		props[k] = v
	}
	for k, v := range properties {
		props[k] = v
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, ok := props["id"]; !ok {
		props["id"] = models.NewID()
	}
	if _, ok := props["created_at"]; !ok {
		props["created_at"] = now
	}
	if _, ok := props["updated_at"]; !ok {
		props["updated_at"] = now
	}
	if _, ok := props["project_id"]; !ok {
		props["project_id"] = identity["project_id"]
	}

	node := &models.Node{
		ID:         props["id"].(string),
		Kind:       kind,
		Properties: props,
	}

	// Try INSERT, fall back to UPDATE if ID exists
	propsJSON, err := json.Marshal(props)
	if err != nil {
		return nil, err
	}

	_, err = r.db.ExecContext(ctx,
		`INSERT INTO graph_nodes (id, kind, project_id, properties, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO UPDATE SET
			kind = EXCLUDED.kind,
			project_id = EXCLUDED.project_id,
			properties = graph_nodes.properties::jsonb || EXCLUDED.properties::jsonb,
			updated_at = EXCLUDED.updated_at`,
		node.ID, kind, props["project_id"], propsJSON, props["created_at"], props["updated_at"])
	if err != nil {
		return nil, fmt.Errorf("upsert node: %w", err)
	}
	return node, nil
}

func (r *SQLGraphRepository) GetNode(nodeID string) (*models.Node, error) {
	ctx := context.Background()
	var kind, propsJSON string
	err := r.db.QueryRowContext(ctx,
		`SELECT kind, properties FROM graph_nodes WHERE id = $1`, nodeID).Scan(&kind, &propsJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var props map[string]any
	if err := json.Unmarshal([]byte(propsJSON), &props); err != nil {
		return nil, err
	}
	return &models.Node{ID: nodeID, Kind: kind, Properties: props}, nil
}

func (r *SQLGraphRepository) FindNodes(kind string, filters map[string]any) ([]*models.Node, error) {
	ctx := context.Background()
	query := "SELECT id, kind, properties FROM graph_nodes"
	args := []any{}
	conds := []string{}
	idx := 1

	if kind != "" {
		conds = append(conds, fmt.Sprintf("kind = $%d", idx))
		args = append(args, kind)
		idx++
	}

	for k, v := range filters {
		conds = append(conds, fmt.Sprintf("properties ->> $%d = $%d", idx, idx+1))
		args = append(args, k, v)
		idx += 2
	}

	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []*models.Node
	for rows.Next() {
		var id, k, propsJSON string
		if err := rows.Scan(&id, &k, &propsJSON); err != nil {
			return nil, err
		}
		var props map[string]any
		if err := json.Unmarshal([]byte(propsJSON), &props); err != nil {
			return nil, err
		}
		results = append(results, &models.Node{ID: id, Kind: k, Properties: props})
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results, nil
}

func (r *SQLGraphRepository) Link(kind, fromID, toID string, properties map[string]any) (*models.Edge, error) {
	if err := ValidateKind(kind); err != nil {
		return nil, err
	}
	ctx := context.Background()
	r.mu.Lock()
	defer r.mu.Unlock()

	if properties == nil {
		properties = make(map[string]any)
	}
	edge := models.NewEdge(kind, fromID, toID, properties)
	propsJSON, err := json.Marshal(properties)
	if err != nil {
		return nil, err
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO graph_edges (id, kind, from_id, to_id, properties, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (id) DO NOTHING`,
		edge.ID, kind, fromID, toID, propsJSON, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("insert edge: %w", err)
	}
	return edge, nil
}

func (r *SQLGraphRepository) Neighbors(nodeID, edgeKind string, direction Direction) ([]EdgeNode, error) {
	ctx := context.Background()
	query := `SELECT e.id, e.kind, e.from_id, e.to_id, e.properties, n.id, n.kind, n.properties
	          FROM graph_edges e JOIN graph_nodes n ON ` +
		`(CASE WHEN $1 = 'out' THEN e.to_id = n.id WHEN $1 = 'in' THEN e.from_id = n.id ELSE FALSE END)
	          WHERE ` +
		`(CASE WHEN $1 = 'out' THEN e.from_id = $2 WHEN $1 = 'in' THEN e.to_id = $2 WHEN $1 = 'both' THEN (e.from_id = $2 OR e.to_id = $2) END)`

	args := []any{direction, nodeID}
	if edgeKind != "" {
		query += " AND e.kind = $3"
		args = append(args, edgeKind)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []EdgeNode
	for rows.Next() {
		var e models.Edge
		var propsJSON string
		var n models.Node
		var nPropsJSON string
		if err := rows.Scan(&e.ID, &e.Kind, &e.FromID, &e.ToID, &propsJSON, &n.ID, &n.Kind, &nPropsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(propsJSON), &e.Properties); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(nPropsJSON), &n.Properties); err != nil {
			return nil, err
		}
		results = append(results, EdgeNode{Edge: &e, Node: &n})
	}
	return results, nil
}

func (r *SQLGraphRepository) RemoveNodes(nodeIDs []string) error {
	ctx := context.Background()
	if len(nodeIDs) == 0 {
		return nil
	}
	args := make([]any, len(nodeIDs))
	placeholders := make([]string, len(nodeIDs))
	for i, id := range nodeIDs {
		args[i] = id
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM graph_nodes WHERE id IN (`+strings.Join(placeholders, ",")+`)`)
	if err != nil {
		return err
	}
	return nil
}

func (r *SQLGraphRepository) RemoveEdges(edgeIDs []string) error {
	ctx := context.Background()
	if len(edgeIDs) == 0 {
		return nil
	}
	args := make([]any, len(edgeIDs))
	placeholders := make([]string, len(edgeIDs))
	for i, id := range edgeIDs {
		args[i] = id
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM graph_edges WHERE id IN (`+strings.Join(placeholders, ",")+`)`)
	if err != nil {
		return err
	}
	return nil
}

func (r *SQLGraphRepository) QueryReadonly(query string, params map[string]any) ([]map[string]any, error) {
	// Security: QueryReadonly is intended for read-only graph introspection.
	// Reject multi-statement queries to prevent injection.
	upper := strings.ToUpper(strings.TrimSpace(query))
	if strings.Contains(upper, ";") || strings.Contains(upper, "DROP") ||
		strings.Contains(upper, "DELETE") || strings.Contains(upper, "UPDATE") ||
		strings.Contains(upper, "INSERT") || strings.Contains(upper, "TRUNCATE") ||
		strings.Contains(upper, "ALTER") || strings.Contains(upper, "CREATE") {
		return nil, fmt.Errorf("unsafe query rejected")
	}

	ctx := context.Background()
	args := make([]any, 0, len(params))
	// Convert params to positional args (simple approach)
	// Note: In production, use proper parameter binding.
	for _, v := range params {
		args = append(args, v)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	cols, _ := rows.Columns()
	for rows.Next() {
		values := make([]any, len(cols))
		scanArgs := make([]any, len(cols))
		for i := range scanArgs {
			scanArgs[i] = &values[i]
		}
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}
	return results, nil
}

func (r *SQLGraphRepository) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}
