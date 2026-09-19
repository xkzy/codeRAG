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

func TestHTTPServerStartBackgroundSharedPort(t *testing.T) {
	app := ApplicationInMemory()
	first := NewHTTPServer(app, "127.0.0.1:0")
	addr, stop, err := first.StartBackground()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	resp, err := http.Get("http://" + addr.String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	// A second instance on the same port must report an error, not panic.
	if _, _, err := NewHTTPServer(app, addr.String()).StartBackground(); err == nil {
		t.Fatal("expected bind error on occupied port")
	}
}

func TestHTTPServerGraphifyGraphAndIdleProgress(t *testing.T) {
	app := ApplicationInMemory()
	g := Graph{
		Nodes: []Node{{ID: "a", Label: "a"}, {ID: "b", Label: "b"}, {ID: "c", Label: "c"}},
		Edges: []Edge{{Source: "a", Target: "b"}, {Source: "a", Target: "c"}},
	}
	data, _ := json.Marshal(g)
	app.Graph.UpsertNode("GraphifyRun", map[string]any{"project_id": "p"}, map[string]any{
		"graph_json": string(data), "nodes": 3, "edges": 2, "communities": 1,
	})
	ts := httptest.NewServer(NewHTTPServer(app, ":0").mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/graphify/graph.json?limit=2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Nodes      []Node `json:"nodes"`
		Edges      []Edge `json:"edges"`
		TotalNodes int    `json:"total_nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Nodes) != 2 || out.TotalNodes != 3 || out.Nodes[0].ID != "a" {
		t.Fatalf("expected 2 capped nodes led by most-connected 'a', got %+v", out)
	}
	for _, e := range out.Edges {
		if e.Source == "c" || e.Target == "c" {
			t.Fatalf("edge references dropped node: %+v", e)
		}
	}

	sse, err := http.Get(ts.URL + "/api/graphify/progress")
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	buf := make([]byte, 512)
	n, _ := sse.Body.Read(buf)
	if !strings.Contains(string(buf[:n]), `"nodes":3`) {
		t.Fatalf("idle progress should report last stored run: %s", buf[:n])
	}
}
