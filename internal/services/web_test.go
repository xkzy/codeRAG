package services

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPServerMemories(t *testing.T) {
	app := ApplicationInMemory()
	app.Memory.Store("p", "alpha", "decoder packet packet packet", map[string]any{"auto_compact": false})
	srv := NewHTTPServer(app, ":0")
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/memories?project_id=p&q=packet")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(out))
	}
}

func TestHTTPServerProjects(t *testing.T) {
	app := ApplicationInMemory()
	app.Graph.UpsertNode("Project", map[string]any{"id": "p", "project_id": "p"}, map[string]any{"path": "/tmp/p"})
	srv := NewHTTPServer(app, ":0")
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0]["id"] != "p" {
		t.Fatalf("projects: %v", out)
	}
}

func TestHTTPServerThoughts(t *testing.T) {
	app := ApplicationInMemory()
	app.Graph.UpsertNode("Project", map[string]any{"id": "p", "project_id": "p"}, nil)
	app.Graph.UpsertNode("Memory", map[string]any{"project_id": "p", "title": "m1"}, map[string]any{"content": "x"})
	app.Graph.UpsertNode("Hypothesis", map[string]any{"project_id": "p", "subject_id": "m1", "claim": "c"}, map[string]any{})
	srv := NewHTTPServer(app, ":0")
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/thoughts?project_id=p")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	kinds := out["kinds"].(map[string]any)
	hypo := kinds["Hypothesis"].([]any)
	if len(hypo) != 1 {
		t.Fatalf("thoughts: %v", out)
	}
}

func TestHTTPServerDashboard(t *testing.T) {
	app := ApplicationInMemory()
	srv := NewHTTPServer(app, ":0")
	ts := httptest.NewServer(srv.mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Agent View") {
		t.Fatal("dashboard missing title")
	}
}
