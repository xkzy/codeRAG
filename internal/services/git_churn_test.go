package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChurn(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	app := ApplicationInMemory()
	result, err := app.Git.Churn("p", dir, 50)
	if err != nil {
		t.Fatal(err)
	}
	if result["project_id"] != "p" {
		t.Errorf("expected project p, got %v", result["project_id"])
	}
	if result["file_count"].(int) != 0 {
		t.Errorf("expected 0 files in empty git repo, got %v", result["file_count"])
	}
}

func TestChurn_NotGitRepo(t *testing.T) {
	dir := t.TempDir()
	app := ApplicationInMemory()
	_, err := app.Git.Churn("p", dir, 50)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestExportGraph(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(p, []byte("name,age\nAlice,30\nBob,25"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	_, err := app.Documents.IndexDocument("p", p)
	if err != nil {
		t.Fatal(err)
	}

	// The graph should have TableSchema and TableRow nodes
	schemas, _ := app.Graph.FindNodes("TableSchema", map[string]any{"project_id": "p"})
	if len(schemas) != 1 {
		t.Errorf("expected 1 schema, got %d", len(schemas))
	}
	rows, _ := app.Graph.FindNodes("TableRow", map[string]any{"project_id": "p"})
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
}
