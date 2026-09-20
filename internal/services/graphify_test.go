package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)


func TestGraphifyRun(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.go", "package main\n\nfunc hello() {}\nfunc world() {}\ntype Server struct {}\n")
	write(t, dir, "doc.md", "# Introduction\n\nSome text here.\n\n## Details\nMore text.")
	write(t, dir, filepath.Join(".git", "config"), "ignore")
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755)
	write(t, dir, filepath.Join("node_modules", "pkg.js"), "console.log('hi')")

	g := NewGraphify(dir, true, false)
	graph, err := g.Run()
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) == 0 {
		t.Fatal("expected nodes")
	}
	if len(graph.Edges) == 0 {
		t.Fatal("expected edges")
	}
	if len(graph.Communities) == 0 {
		t.Fatal("expected communities")
	}
	for _, n := range graph.Nodes {
		if n.ID == "" {
			t.Fatal("node id empty")
		}
	}
	for _, e := range graph.Edges {
		if e.Source == "" || e.Target == "" {
			t.Fatal("edge source/target empty")
		}
		if e.Confidence == "" {
			t.Fatal("edge confidence empty")
		}
	}
}

func TestGraphifyEmpty(t *testing.T) {
	dir := t.TempDir()
	g := NewGraphify(dir, false, false)
	_, err := g.Run()
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

func TestGraphifyOutputs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "main.py", "def foo():\n    pass\n\nclass Bar:\n    pass\n")

	g := NewGraphify(dir, false, false)
	_, err := g.Run()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"graphify-out/graph.json", "graphify-out/graph.html", "graphify-out/GRAPH_REPORT.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); os.IsNotExist(err) {
			t.Errorf("missing output %s", f)
		}
	}
}

func TestExtractFunctionNames(t *testing.T) {
	goCode := "func main() {}\nfunc helper(x int) int { return x }\n"
	funcs := extractFunctionNames(goCode, ".go")
	if len(funcs) != 2 {
		t.Fatalf("expected 2, got %d", len(funcs))
	}
	if funcs[0] != "main" || funcs[1] != "helper" {
		t.Fatalf("unexpected: %v", funcs)
	}
}

func TestExtractTypeNames(t *testing.T) {
	goCode := "type User struct {}\ntype Service interface {}\n"
	types := extractTypeNames(goCode, ".go")
	if len(types) != 2 {
		t.Fatalf("expected 2, got %d", len(types))
	}
}

func TestConceptID(t *testing.T) {
	id := conceptID("Hello World")
	if id != "concept:hello_world" {
		t.Fatalf("unexpected id %q", id)
	}
}

func TestLabelPropagate(t *testing.T) {
	graph := &Graph{
		Nodes: []Node{
			{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"},
		},
		Edges: []Edge{
			{Source: "a", Target: "b"},
			{Source: "b", Target: "c"},
			{Source: "c", Target: "a"},
			{Source: "d", Target: "a"},
		},
	}
	comms := labelPropagate(graph, false)
	if len(comms) == 0 {
		t.Fatal("expected communities")
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteGraphHTMLUsesCanvas(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "a", Label: "Alpha", Kind: "file"},
			{ID: "b", Label: "Beta", Kind: "concept"},
		},
		Edges: []Edge{
			{Source: "a", Target: "b", Relation: "defines", Confidence: "EXTRACTED", ConfidenceScore: 1.0},
		},
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.html")
	if err := writeGraphHTML(g, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "<canvas") {
		t.Error("graph.html must use <canvas> not SVG")
	}
	if strings.Contains(body, "<svg") {
		t.Error("graph.html must not use SVG")
	}
	if !strings.Contains(body, "forceSimulation") {
		t.Error("graph.html must use d3.forceSimulation")
	}
	if !strings.Contains(body, "requestAnimationFrame") {
		t.Error("graph.html must use requestAnimationFrame for canvas rendering")
	}
	if !strings.Contains(body, `"Alpha"`) {
		t.Error("graph.html must inline node labels")
	}
}

