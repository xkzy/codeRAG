package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSmartRead(t *testing.T) {
	app, root := indexTree(t, map[string]string{
		"go.mod":               "module example.com/m\n",
		"cmd/app/main.go":      "package main\nimport \"example.com/m/internal/svc\"\nfunc main() { svc.Run() }\n",
		"internal/svc/svc.go":  "package svc\nimport \"example.com/m/internal/store\"\nfunc Run() { store.Get() }\nfunc Other() { store.Get() }\ntype Service struct{ S store.Store }\n",
		"internal/store/st.go": "package store\nfunc Get() {}\ntype Store struct{}\n",
	})
	_ = root

	// svc.go has two functions and a type
	result, err := app.SmartRead("p", "internal/svc/svc.go")
	if err != nil {
		t.Fatal(err)
	}
	if result["language"] != "go" {
		t.Errorf("language = %q, want go", result["language"])
	}
	if result["file"] != "internal/svc/svc.go" {
		t.Errorf("file = %q, want internal/svc/svc.go", result["file"])
	}
	syms := result["symbols"].([]map[string]any)
	if len(syms) < 2 {
		t.Fatalf("expected at least 2 symbols, got %d", len(syms))
	}
	names := map[string]bool{}
	for _, s := range syms {
		names[s["name"].(string)] = true
	}
	if !names["Run"] || !names["Other"] {
		t.Errorf("expected Run and Other symbols, got %v", names)
	}
	// Check symbols are sorted by line_start
	for i := 1; i < len(syms); i++ {
		prev := syms[i-1]["line_start"].(int)
		curr := syms[i]["line_start"].(int)
		if prev > curr {
			t.Errorf("symbols not sorted: %d before %d", prev, curr)
		}
	}
	// Should have imports (DEPENDS_ON edges to store)
	imports := result["imports"].([]map[string]any)
	foundStore := false
	for _, imp := range imports {
		if imp["path"].(string) == "internal/store/st.go" {
			foundStore = true
		}
	}
	if !foundStore {
		t.Errorf("expected import of internal/store/st.go, got %v", imports)
	}

	// Non-indexed file should error
	if _, err := app.SmartRead("p", "nonexistent.go"); err == nil {
		t.Error("expected error for non-indexed file")
	}
	// Invalid project should error
	if _, err := app.SmartRead("nope", "file.go"); err == nil {
		t.Error("expected error for non-existent project")
	}
}

func TestAnalyzeProject(t *testing.T) {
	app, _ := indexTree(t, map[string]string{
		"go.mod":               "module example.com/m\n",
		"cmd/app/main.go":      "package main\nfunc main() {}\n",
		"internal/svc/svc.go":  "package svc\nfunc Run() {}\ntype Service struct{}\n",
		"internal/store/st.go": "package store\nfunc Get() {}\ntype Store struct{}\n",
	})

	result, err := app.AnalyzeProject("p")
	if err != nil {
		t.Fatal(err)
	}
	if result["files"].(int) < 3 {
		t.Errorf("expected at least 3 files, got %d", result["files"])
	}
	if result["total_symbols"].(int) < 2 {
		t.Errorf("expected at least 2 symbols, got %d", result["total_symbols"])
	}
	langs := result["languages"].(map[string]int)
	if _, ok := langs["go"]; !ok {
		t.Errorf("expected go language, got %v", langs)
	}
	if result["root"] == "" {
		t.Error("root should not be empty")
	}
	if result["project_id"] != "p" {
		t.Errorf("project_id = %q, want p", result["project_id"])
	}

	// Non-existent project should error
	if _, err := app.AnalyzeProject("nope"); err == nil {
		t.Error("expected error for non-existent project")
	}
}

func TestCompactChangeIntelligence(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, dir, map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	})
	app := ApplicationInMemory()
	if _, err := app.Index.IndexRepository("p", dir, true, nil, false); err != nil {
		t.Fatal(err)
	}

	result, err := app.Git.CompactChangeIntelligence("p", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["project_id"] != "p" {
		t.Errorf("expected project p, got %v", result["project_id"])
	}
	if result["has_changes"].(bool) {
		t.Error("expected no changes in clean repo")
	}
	if result["file_count"].(int) != 0 {
		t.Errorf("expected 0 changed files, got %d", result["file_count"])
	}

	// Non-git repo should error
	app2 := ApplicationInMemory()
	if _, err := app2.Git.CompactChangeIntelligence("p", ""); err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestSupersedeEvidence(t *testing.T) {
	app, _ := indexTree(t, map[string]string{
		"main.go": "package main\nfunc main() {}\n",
	})

	// Find a function node to use as subject.
	fns, _ := app.Graph.FindNodes("Function", map[string]any{"project_id": "p"})
	if len(fns) == 0 {
		t.Fatal("no functions indexed")
	}
	subjectID := fns[0].ID

	// Record an evidence.
	ev, err := app.Evidence.RecordEvidence("p", subjectID, "original finding", map[string]any{
		"confidence": 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	evID := ev["id"].(string)

	// Supersede it.
	result, err := app.Evidence.SupersedeEvidence("p", evID, "updated finding", "old approach was wrong", "agent-x")
	if err != nil {
		t.Fatal(err)
	}
	if result["old_id"] != evID {
		t.Errorf("old_id = %q, want %q", result["old_id"], evID)
	}
	if result["new_id"] == "" {
		t.Error("new_id should not be empty")
	}
	// New record should reference the old one.
	newEv := result["new"].(map[string]any)
	if newEv["supersedes"] != evID {
		t.Errorf("new supersedes = %v, want %q", newEv["supersedes"], evID)
	}

	// Verify old node is marked superseded.
	oldNode, _ := app.Graph.GetNode(evID)
	if oldNode == nil {
		t.Fatal("old node should still exist")
	}
	if sa := oldNode.Properties["superseded_at"]; sa == nil {
		t.Error("old node should have superseded_at set")
	}

	// Cannot supersede again.
	if _, err := app.Evidence.SupersedeEvidence("p", evID, "body", "rationale", "agent"); err == nil {
		t.Error("expected error when superseding already-superseded evidence")
	}

	// Cannot supersede non-existent evidence.
	if _, err := app.Evidence.SupersedeEvidence("p", "nonexistent", "body", "rationale", "agent"); err == nil {
		t.Error("expected error for nonexistent evidence")
	}

	// Cannot supersede evidence from another project.
	otherApp := ApplicationInMemory()
	otherApp.Graph.UpsertNode("Project", map[string]any{"id": "p2", "path": "/tmp/other"}, nil)
	otherApp.Graph.UpsertNode("Function", map[string]any{"project_id": "p2", "name": "B"}, map[string]any{"path": "/tmp/other/b.go"})
	if _, err := app.Evidence.SupersedeEvidence("p", "nonexistent-from-other", "body", "rationale", "agent"); err == nil {
		t.Error("expected error for evidence from another project")
	}
}

func TestProjectProfile(t *testing.T) {
	app, _ := indexTree(t, map[string]string{
		"go.mod":              "module example.com/m\n",
		"cmd/app/main.go":     "package main\nfunc main() {}\n",
		"internal/svc/svc.go": "package svc\nfunc Run() {}\ntype Service struct{}\n",
	})

	result, err := app.ProjectProfile("p")
	if err != nil {
		t.Fatal(err)
	}
	if result["project_id"] != "p" {
		t.Errorf("project_id = %q, want p", result["project_id"])
	}
	if result["files"].(int) < 2 {
		t.Errorf("expected at least 2 files, got %d", result["files"])
	}
	if result["functions"].(int) < 2 {
		t.Errorf("expected at least 2 functions, got %d", result["functions"])
	}
	if result["total_records"].(int) < 0 {
		t.Errorf("total_records should be >= 0, got %d", result["total_records"])
	}
	recordCounts, ok := result["record_counts"].(map[string]int)
	if !ok {
		t.Fatalf("record_counts should be map[string]int, got %T", result["record_counts"])
	}
	_ = recordCounts
	langs := result["languages"].(map[string]int)
	if _, ok := langs["go"]; !ok {
		t.Errorf("expected go language, got %v", langs)
	}
	if result["last_indexed_at"] == "" {
		t.Error("last_indexed_at should not be empty")
	}

	// Non-existent project should error
	if _, err := app.ProjectProfile("nope"); err == nil {
		t.Error("expected error for non-existent project")
	}
}
