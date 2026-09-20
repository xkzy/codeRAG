package ctxstore

import (
	"testing"

	"codergag/internal/services"
)

func newStore(t *testing.T) (*Store, *services.Application) {
	t.Helper()
	app := services.ApplicationInMemory()
	return New(app), app
}

func TestAddListSearchGetDelete(t *testing.T) {
	s, _ := newStore(t)
	a, err := s.Add("p", Record{Kind: "decision", Title: "Use SQLite", Body: "single binary, no server", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == "" || a.Kind != "decision" || !a.Pinned || a.Source != "manual" {
		t.Fatalf("unexpected record: %+v", a)
	}
	if _, err := s.Add("p", Record{Title: "Logging", Body: "use slog"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Add("other", Record{Title: "Elsewhere", Body: "other project"}); err != nil {
		t.Fatal(err)
	}

	all, _ := s.List("p", "")
	if len(all) != 2 {
		t.Fatalf("want 2 records in p, got %d", len(all))
	}
	if dec, _ := s.List("p", "decision"); len(dec) != 1 || dec[0].Title != "Use SQLite" {
		t.Fatalf("kind filter failed: %+v", dec)
	}
	hits, _ := s.Search("p", "sqlite", 5)
	if len(hits) != 1 || hits[0].ID != a.ID || hits[0].Body == "" {
		t.Fatalf("search failed: %+v", hits)
	}
	got, err := s.Get(a.ID)
	if err != nil || got.Title != "Use SQLite" {
		t.Fatalf("get failed: %+v %v", got, err)
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(a.ID); err == nil {
		t.Fatal("deleted record must not be found")
	}
}

func TestAddValidation(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Add("p", Record{Title: "  "}); err == nil {
		t.Fatal("empty title must fail")
	}
	if _, err := s.Add("p", Record{Title: "x", Kind: "bogus"}); err == nil {
		t.Fatal("invalid kind must fail")
	}
}

func TestSetPinnedAndReset(t *testing.T) {
	s, _ := newStore(t)
	r, _ := s.Add("p", Record{Title: "a", Body: "b"})
	if err := s.SetPinned(r.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(r.ID); !got.Pinned {
		t.Fatal("record should be pinned")
	}
	s.Add("q", Record{Title: "keep", Body: "other project"})
	n, err := s.Reset("p")
	if err != nil || n != 1 {
		t.Fatalf("reset removed %d, err %v", n, err)
	}
	if left, _ := s.List("q", ""); len(left) != 1 {
		t.Fatal("reset must not touch other projects")
	}
}

func TestProjectForDir(t *testing.T) {
	_, app := newStore(t)
	app.Graph.UpsertNode("Project", map[string]any{"id": "outer"}, map[string]any{"project_id": "outer", "path": "/w/outer"})
	app.Graph.UpsertNode("Project", map[string]any{"id": "inner"}, map[string]any{"project_id": "inner", "path": "/w/outer/inner"})
	cases := map[string]string{
		"/w/outer":           "outer",
		"/w/outer/inner/sub": "inner",
		"/w/outer2":          "",
		"/elsewhere":         "",
	}
	for dir, want := range cases {
		got, err := ProjectForDir(app, dir)
		if err != nil || got != want {
			t.Errorf("ProjectForDir(%q) = %q, %v; want %q", dir, got, err, want)
		}
	}
}
