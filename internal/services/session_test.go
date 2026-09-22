package services

import (
	"testing"
)

func TestSessionManager(t *testing.T) {
	app := ApplicationInMemory()
	if app.Session == nil {
		t.Fatal("Session manager should be initialized")
	}

	session, err := app.Session.StartSession("test-project", "/tmp/test")
	if err != nil {
		t.Fatalf("StartSession failed: %v", err)
	}

	if session.ProjectID != "test-project" {
		t.Errorf("expected project ID 'test-project', got %q", session.ProjectID)
	}

	if session.State != SessionStateActive {
		t.Errorf("expected state %q, got %q", SessionStateActive, session.State)
	}

	current := app.Session.GetCurrentSession()
	if current == nil {
		t.Fatal("GetCurrentSession returned nil")
	}
	if current.ID != session.ID {
		t.Errorf("expected session ID %q, got %q", session.ID, current.ID)
	}

	wsc := app.Session.GetWarmStartContext()
	if wsc == nil {
		t.Fatal("GetWarmStartContext returned nil")
	}

	err = app.Session.EndSession()
	if err != nil {
		t.Fatalf("EndSession failed: %v", err)
	}

	current = app.Session.GetCurrentSession()
	if current != nil {
		t.Error("expected nil session after EndSession")
	}
}

func TestSnapshotManager(t *testing.T) {
	app := ApplicationInMemory()
	if app.Snapshot == nil {
		t.Fatal("Snapshot manager should be initialized")
	}

	// Snapshot without a real project will fail on project lookup
	_, err := app.BuildProjectSnapshot("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent project")
	}
}

func TestHierarchicalResolver(t *testing.T) {
	app := ApplicationInMemory()
	resolver := NewHierarchicalResolver(app.Graph, app)

	result := resolver.ResolveExact("test-project", "parse_cat21", "a.go", 10)
	if result == nil {
		t.Fatal("ResolveExact returned nil")
	}
	if result.Method != "exact" {
		t.Errorf("expected method 'exact', got %q", result.Method)
	}
	if result.Depth != 0 {
		t.Errorf("expected depth 0, got %d", result.Depth)
	}

	result = resolver.ResolveHotContext("test-project", 10)
	if result == nil {
		t.Fatal("ResolveHotContext returned nil")
	}
	if result.Method != "hot_context" {
		t.Errorf("expected method 'hot_context', got %q", result.Method)
	}

	result = resolver.ResolveSubsystem("test-project", "src/main.go")
	if result == nil {
		t.Fatal("ResolveSubsystem returned nil")
	}
	if result.Method != "subsystem" {
		t.Errorf("expected method 'subsystem', got %q", result.Method)
	}
}

func TestHotContextCache(t *testing.T) {
	cache := NewHotContextCache("test-project", nil)

	cache.AddFile("/path/to/file.go", "go", "abc123def456")
	cache.AddSymbol("sym1", "parse_cat21", "function", "file1", "/path/to/file.go", 10, 20, "func parse_cat21()")

	files := cache.GetFilesByAccess()
	if len(files) != 1 {
		t.Errorf("expected 1 file, got %d", len(files))
	}

	symbols := cache.GetSymbolsByAccess()
	if len(symbols) != 1 {
		t.Errorf("expected 1 symbol, got %d", len(symbols))
	}

	hotCtx := cache.GetHotContext()
	if hotCtx == nil {
		t.Fatal("GetHotContext returned nil")
	}
	if len(hotCtx.Files) != 1 {
		t.Errorf("expected 1 hot file, got %d", len(hotCtx.Files))
	}
	if len(hotCtx.Symbols) != 1 {
		t.Errorf("expected 1 hot symbol, got %d", len(hotCtx.Symbols))
	}
}
