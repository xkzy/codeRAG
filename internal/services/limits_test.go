package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexRepositoryFileCap(t *testing.T) {
	old := MaxIndexFiles
	MaxIndexFiles = 3
	defer func() { MaxIndexFiles = old }()

	root := t.TempDir()
	for i := 0; i < 8; i++ {
		src := fmt.Sprintf("package p\nfunc F%d() {}\n", i)
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%d.go", i)), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := ApplicationInMemory()
	defer app.Events.Stop()
	res, err := app.Index.IndexRepository("p1", root, false, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if res["truncated"] != true {
		t.Fatalf("expected truncated=true, got %v", res["truncated"])
	}
	if seen := res["files_seen"].(int); seen > 3 {
		t.Fatalf("files_seen = %d, want <= 3", seen)
	}
}

func TestGraphifyBoundsLargeFilesAndConcepts(t *testing.T) {
	root := t.TempDir()
	// Oversized file must be skipped entirely.
	big := strings.Repeat("func Big() {}\n", graphifyMaxFileBytes/8)
	if err := os.WriteFile(filepath.Join(root, "big.go"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	// File with far more concepts than the per-file cap.
	var b strings.Builder
	b.WriteString("package p\n")
	for i := 0; i < graphifyMaxConceptsPerFile*4; i++ {
		fmt.Fprintf(&b, "func Fn%d() {}\n", i)
	}
	if err := os.WriteFile(filepath.Join(root, "many.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := NewGraphify(root, false, false).extract()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range g.Nodes {
		if n.File == "big.go" {
			t.Fatalf("oversized file was not skipped: %+v", n)
		}
	}
	maxEdges := graphifyMaxConceptsPerFile*graphifyMaxConceptsPerFile + graphifyMaxConceptsPerFile + 8
	if len(g.Edges) > maxEdges {
		t.Fatalf("edges = %d, want <= %d", len(g.Edges), maxEdges)
	}
}

func TestDaemonCapsProjects(t *testing.T) {
	base := t.TempDir()
	for i := 0; i < 5; i++ {
		dir := filepath.Join(base, fmt.Sprintf("p%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	app := ApplicationInMemory()
	d := NewDaemon(app, DaemonConfig{Roots: []string{base}, MaxProjects: 2, ProjectDetectInterval: 3600e9, IndexInterval: 3600e9})
	d.Start()
	defer d.Stop()
	defer app.Events.Stop()
	if got := d.Status()["projects"]; got != 2 {
		t.Fatalf("projects = %v, want 2", got)
	}
}
