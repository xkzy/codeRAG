package main

import (
	"os"
	"path/filepath"
	"testing"

	"codergag/internal/services"
)

func TestSplitList(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a,b,c", []string{"a", "b", "c"}},
		{"a, b , c", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
		{"  ,  ", nil},
		{"a, ,b", []string{"a", "b"}},
	}
	for _, tt := range tests {
		got := splitList(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitList(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitList(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestFirstProjectNil(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()
	pid, err := firstProject(app)
	if err != nil {
		t.Fatal(err)
	}
	if pid != "" {
		t.Fatalf("expected empty project id, got %q", pid)
	}
}

func TestFirstProjectFound(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()
	if _, err := app.Graph.UpsertNode("Project", map[string]any{"id": "proj-1", "path": "."}, nil); err != nil {
		t.Fatal(err)
	}
	pid, err := firstProject(app)
	if err != nil {
		t.Fatal(err)
	}
	if pid != "proj-1" {
		t.Fatalf("expected proj-1, got %q", pid)
	}
}

func TestRunIndexRunSubcommand(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	dir := t.TempDir()
	src := "package main\n\nfunc Hello() {}\n"
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	code := runIndex(app, []string{"run", dir})
	if code != 0 {
		t.Fatalf("index run returned %d, want 0", code)
	}
}

func TestRunIndexRunWithForce(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	dir := t.TempDir()
	src := "package main\n\nfunc Hello() {}\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	code := runIndex(app, []string{"run", "-force", dir})
	if code != 0 {
		t.Fatalf("index run -force returned %d, want 0", code)
	}
}

func TestRunIndexRunWithSkipGraphify(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code := runIndex(app, []string{"run", "-skip-graphify", dir})
	if code != 0 {
		t.Fatalf("index run -skip-graphify returned %d, want 0", code)
	}
}

func TestRunIndexStatusWithDaemon(t *testing.T) {
	app := services.ApplicationInMemory()
	defer app.Graph.Close()

	code := runIndex(app, []string{"status"})
	if code != 0 {
		t.Fatalf("index status returned %d, want 0", code)
	}
}
