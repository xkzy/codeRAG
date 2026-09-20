package services

import (
	"testing"

	"codergag/internal/graph"
)

// TestTeamServiceGetTeamContextNilAgentProperty feeds an Agent node whose
// "agent" property is nil (not a string) into GetTeamContext. The previous
// implementation used an unchecked type assertion here, which panics on a
// nil interface. This test guards against that regression.
func TestTeamServiceGetTeamContextNilAgentProperty(t *testing.T) {
	g := graph.NewMemoryGraphRepository()
	defer g.Close()

	team, err := g.UpsertNode("Team", map[string]any{"project_id": "p", "name": "t"},
		map[string]any{"member_count": 1})
	if err != nil {
		t.Fatal(err)
	}
	// An Agent node with a nil "agent" property — exactly the shape that used
	// to panic before the ok-checked assertion was added.
	if _, err := g.UpsertNode("Agent", map[string]any{
		"project_id": "p",
		"team_id":    team.ID,
		"agent":      nil,
	}, map[string]any{"role": "backend_dev"}); err != nil {
		t.Fatal(err)
	}
	// Also an Agent node that is missing the "agent" key entirely.
	if _, err := g.UpsertNode("Agent", map[string]any{
		"project_id": "p",
		"team_id":    team.ID,
	}, map[string]any{"role": "frontend_dev"}); err != nil {
		t.Fatal(err)
	}

	svc := NewTeamService(g)
	ctx, err := svc.GetTeamContext("p", team.ID, nil, 10)
	if err != nil {
		t.Fatalf("GetTeamContext returned error: %v", err)
	}
	if ctx == nil {
		t.Fatal("GetTeamContext returned nil context")
	}
}

// TestTeamServiceGetTeamContextMalformedScope feeds a non-string scope entry
// into GetTeamContext to ensure the scope check is also panic-safe.
func TestTeamServiceGetTeamContextMalformedScope(t *testing.T) {
	g := graph.NewMemoryGraphRepository()
	defer g.Close()

	team, err := g.UpsertNode("Team", map[string]any{"project_id": "p", "name": "t"},
		map[string]any{"member_count": 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.UpsertNode("SharedKnowledge", map[string]any{
		"project_id": "p",
		"team_id":    team.ID,
		"scope":      []any{nil, 123, "backend_dev"},
	}, map[string]any{"title": "x", "content": "y"}); err != nil {
		t.Fatal(err)
	}

	svc := NewTeamService(g)
	// Must not panic even though the scope slice contains non-string entries.
	ctx, err := svc.GetTeamContext("p", team.ID, []string{"backend_dev"}, 10)
	if err != nil {
		t.Fatalf("GetTeamContext returned error: %v", err)
	}
	if ctx == nil {
		t.Fatal("GetTeamContext returned nil context")
	}
}