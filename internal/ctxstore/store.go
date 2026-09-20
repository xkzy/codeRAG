package ctxstore

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/services"
)

// Kinds are the record kinds `ctx add` accepts. Memories created through the
// MCP tools may carry other kinds; they are listed and searched unchanged.
var Kinds = []string{"decision", "convention", "task", "note"}

func ValidKind(k string) bool {
	for _, v := range Kinds {
		if v == k {
			return true
		}
	}
	return false
}

type Record struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	Source    string  `json:"source"`
	Pinned    bool    `json:"pinned"`
	CreatedAt string  `json:"created_at"`
	Score     float64 `json:"score,omitempty"`
}

// Store is a thin layer over the graph's Memory nodes.
type Store struct {
	g   graph.GraphRepository
	mem *services.MemoryService
}

func New(app *services.Application) *Store {
	return &Store{g: app.Graph, mem: app.Memory}
}

func fromNode(n *models.Node) Record {
	pinned, _ := n.Properties["pinned"].(bool)
	return Record{
		ID:        n.ID,
		Kind:      services.StrProp(n, "kind"),
		Title:     services.StrProp(n, "title"),
		Body:      services.StrProp(n, "content"),
		Source:    services.StrProp(n, "source"),
		Pinned:    pinned,
		CreatedAt: services.StrProp(n, "created_at"),
	}
}

func fromMap(m map[string]any) Record {
	str := func(k string) string { s, _ := m[k].(string); return s }
	pinned, _ := m["pinned"].(bool)
	score, _ := m["score"].(float64)
	return Record{
		ID: str("id"), Kind: str("kind"), Title: str("title"), Body: str("content"),
		Source: str("source"), Pinned: pinned, CreatedAt: str("created_at"), Score: score,
	}
}

func archived(n *models.Node) bool {
	b, _ := n.Properties["archived"].(bool)
	return b
}

// Add stores a record. An existing record with the same title in the same
// project is updated (Memory identity is project_id + title).
func (s *Store) Add(project string, r Record) (Record, error) {
	r.Title = strings.TrimSpace(r.Title)
	if r.Title == "" {
		return Record{}, errors.New("title is required")
	}
	if r.Kind == "" {
		r.Kind = "note"
	}
	if !ValidKind(r.Kind) {
		return Record{}, fmt.Errorf("invalid kind %q (want one of %s)", r.Kind, strings.Join(Kinds, ", "))
	}
	if r.Source == "" {
		r.Source = "manual"
	}
	res, err := s.mem.Store(project, r.Title, r.Body, map[string]any{
		"kind": r.Kind, "source": r.Source, "auto_compact": false,
	})
	if err != nil {
		return Record{}, err
	}
	id, _ := res["id"].(string)
	if r.Pinned {
		if err := s.SetPinned(id, true); err != nil {
			return Record{}, err
		}
	}
	return s.Get(id)
}

func (s *Store) Get(id string) (Record, error) {
	n, err := s.g.GetNode(id)
	if err != nil {
		return Record{}, err
	}
	if n == nil || n.Kind != "Memory" {
		return Record{}, graph.ErrNotFound
	}
	return fromNode(n), nil
}

func (s *Store) List(project, kind string) ([]Record, error) {
	nodes, err := s.g.FindNodes("Memory", map[string]any{"project_id": project})
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, n := range nodes {
		if archived(n) {
			continue
		}
		r := fromNode(n)
		if kind != "" && r.Kind != kind {
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (s *Store) Search(project, query string, limit int) ([]Record, error) {
	rows, err := s.mem.Search(project, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(rows))
	for _, m := range rows {
		out = append(out, fromMap(m))
	}
	return out, nil
}

func (s *Store) SetPinned(id string, pinned bool) error {
	n, err := s.g.GetNode(id)
	if err != nil {
		return err
	}
	if n == nil || n.Kind != "Memory" {
		return graph.ErrNotFound
	}
	_, err = s.g.UpsertNode("Memory", map[string]any{
		"project_id": n.Properties["project_id"],
		"title":      n.Properties["title"],
	}, map[string]any{"pinned": pinned})
	return err
}

func (s *Store) Delete(id string) error {
	if _, err := s.Get(id); err != nil {
		return err
	}
	return s.g.RemoveNodes([]string{id})
}

// Reset deletes every Memory node of the project (including archived ones and
// those created through the MCP tools) and returns how many were removed.
func (s *Store) Reset(project string) (int, error) {
	nodes, err := s.g.FindNodes("Memory", map[string]any{"project_id": project})
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return len(ids), s.g.RemoveNodes(ids)
}

// ProjectForDir returns the id of the indexed project whose path is the
// longest ancestor of (or equal to) dir, or "" when none matches.
func ProjectForDir(app *services.Application, dir string) (string, error) {
	nodes, err := app.Graph.FindNodes("Project", nil)
	if err != nil {
		return "", err
	}
	dir = filepath.Clean(dir)
	best, bestLen := "", -1
	for _, n := range nodes {
		p := filepath.Clean(services.StrProp(n, "path"))
		if p == "." || p == "" {
			continue
		}
		if dir == p || strings.HasPrefix(dir, p+string(filepath.Separator)) {
			if len(p) > bestLen {
				best, bestLen = services.StrProp(n, "id"), len(p)
			}
		}
	}
	return best, nil
}
