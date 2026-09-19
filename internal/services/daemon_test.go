package services

import (
	"github.com/fsnotify/fsnotify"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDaemonStatusAndLifecycle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	d := NewDaemon(app, DaemonConfig{Roots: []string{root}, ProjectDetectInterval: time.Hour, IndexInterval: time.Hour})
	d.Start()
	d.Start() // idempotent
	if got := d.Status()["projects"]; got != 1 {
		t.Fatalf("projects = %v, want 1", got)
	}
	d.Stop()
	if d.Status()["running"] != false {
		t.Fatal("daemon should be stopped")
	}
}

func TestObserverOverflowEmitsOldest(t *testing.T) {
	root := t.TempDir()
	engine := NewEventEngine(16)
	o := NewObserverWithConfig(root, "p", engine, ObserverConfig{MaxPending: 1}, 8)
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	for _, f := range []string{a, b} {
		if err := os.WriteFile(f, []byte("package x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	o.handleEvent(fsnotifyEvent(a))
	o.handleEvent(fsnotifyEvent(b))
	if len(engine.Snapshot()) != 1 {
		t.Fatalf("expected evicted change to be emitted, got %d events", len(engine.Snapshot()))
	}
}

func fsnotifyEvent(name string) fsnotify.Event {
	return fsnotify.Event{Name: name, Op: fsnotify.Write}
}

func TestIndexFilesReportsFailuresAndResolves(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "a.go")
	if err := os.WriteFile(good, []byte("package a\nfunc A() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	res, err := app.Index.IndexFiles("p", root, []string{good}, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res["files_changed"] != 1 {
		t.Fatalf("files_changed = %v", res["files_changed"])
	}
	// A directory path cannot be read as a file: the failure must surface.
	res, err = app.Index.IndexFiles("p", root, []string{root}, true, nil)
	if err == nil {
		t.Fatalf("expected error for unreadable file, got %v", res)
	}
}

type countWorker struct{ n int }

func (c *countWorker) OnEvent(Event) { c.n++ }

func TestEventEngineDrainsOnStop(t *testing.T) {
	e := NewEventEngine(8)
	w := &countWorker{}
	e.workers = append(e.workers, w)
	e.queue <- Event{Kind: FileModified}
	e.queue <- Event{Kind: FileModified}
	e.drain()
	if w.n != 2 {
		t.Fatalf("drained %d events, want 2", w.n)
	}
}

func TestEndToEndFileChangeUpdatesGraph(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := ApplicationInMemory()
	d := NewDaemon(app, DaemonConfig{
		Roots: []string{root}, ProjectDetectInterval: time.Hour, IndexInterval: time.Hour,
		IndexOnChange: true, IndexIncremental: true,
	})
	d.Start()
	defer d.Stop()
	defer app.Events.Stop()
	projects := d.Projects()
	if len(projects) != 1 {
		t.Fatalf("projects = %d", len(projects))
	}
	pid := projects[0].ID

	time.Sleep(300 * time.Millisecond)
	file := filepath.Join(root, "e2e.go")
	if err := os.WriteFile(file, []byte("package x\nfunc EndToEnd() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		nodes, _ := app.Graph.FindNodes("SourceFile", map[string]any{"project_id": pid})
		for _, n := range nodes {
			if p, _ := n.Properties["path"].(string); filepath.Base(p) == "e2e.go" {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("changed file never reached the graph")
}
