package services

import (
	"testing"
	"time"
)

func TestSessionState(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	if sp == nil {
		t.Fatal("NewStatusPanel returned nil")
	}

	if sp.IsSessionWarm() {
		t.Error("expected cold session initially")
	}

	if sp.GetContextReuse() != 0 {
		t.Errorf("expected 0 context reuse, got %v", sp.GetContextReuse())
	}
}

func TestStatusPanelEventHandling(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	event := Event{
		ID:        "test-1",
		Kind:      SessionStarted,
		Session:   "session-123",
		ProjectID: "test-project",
		Timestamp: time.Now(),
		Payload:   map[string]any{},
	}

	sp.OnEvent(event)

	sp.mu.RLock()
	connected := sp.sessionState.Connected
	sessionID := sp.sessionState.SessionID
	sp.mu.RUnlock()

	if !connected {
		t.Error("expected session to be connected after SessionStarted event")
	}

	if sessionID != "session-123" {
		t.Errorf("expected session ID session-123, got %s", sessionID)
	}
}

func TestStatusPanelFileOpened(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	event := Event{
		ID:        "test-2",
		Kind:      FileOpened,
		Session:   "session-123",
		ProjectID: "test-project",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"path": "/path/to/file.go",
		},
	}

	sp.OnEvent(event)

	files := sp.GetActiveFiles()
	if len(files) != 1 {
		t.Errorf("expected 1 active file, got %d", len(files))
	}

	if len(files) > 0 && files[0] != "/path/to/file.go" {
		t.Errorf("expected /path/to/file.go, got %s", files[0])
	}
}

func TestStatusPanelSymbolProvenance(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	sp.SetCurrentTask(&TaskState{
		TaskID:     "task-1",
		ProjectID:  "test-project",
		Goal:       "Test task",
		State:      TaskAnalyzing,
		References: []string{"tracker_update", "association_gate"},
		KnownFacts: []TaskFact{
			{Text: "tracker_update reads track_table", Kind: KindFact, Source: "analysis"},
		},
	})

	provenance := sp.GetSymbolProvenance("tracker_update")
	if len(provenance) == 0 {
		t.Fatal("expected provenance entries for tracker_update")
	}

	found := false
	for _, entry := range provenance {
		if entry.Symbol == "tracker_update" {
			found = true
			if entry.Source != "TaskState" {
				t.Errorf("expected source TaskState, got %s", entry.Source)
			}
		}
	}
	if !found {
		t.Error("expected to find provenance entry for tracker_update")
	}
}

func TestStatusPanelWarmthCalculation(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	sp.mu.Lock()
	sp.cacheStats.ExactHits = 80
	sp.cacheStats.SemanticHits = 10
	sp.cacheStats.CacheMisses = 10
	sp.mu.Unlock()

	sp.computeSessionWarmth()

	if !sp.IsSessionWarm() {
		t.Error("expected warm session with 80% cache hit rate")
	}

	reuse := sp.GetContextReuse()
	if reuse < 0.89 || reuse > 0.91 {
		t.Errorf("expected ~90%% context reuse, got %v", reuse)
	}
}

func TestStatusPanelColdSession(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	sp.mu.Lock()
	sp.cacheStats.ExactHits = 0
	sp.cacheStats.SemanticHits = 0
	sp.cacheStats.CacheMisses = 100
	sp.mu.Unlock()

	sp.computeSessionWarmth()

	if sp.IsSessionWarm() {
		t.Error("expected cold session with 0% cache hit rate")
	}
}

func TestStatusPanelSnapshot(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	event := Event{
		ID:        "test-3",
		Kind:      SessionStarted,
		Session:   "session-456",
		ProjectID: "test-project",
		Timestamp: time.Now(),
		Payload:   map[string]any{},
	}
	sp.OnEvent(event)

	sp.SetProjectInfo("wireshark", "master", "abc123")

	snapshot := sp.Snapshot()

	if snapshot.Connection != "Connected" {
		t.Errorf("expected Connected, got %s", snapshot.Connection)
	}

	if snapshot.Session.SessionID != "session-456" {
		t.Errorf("expected session-456, got %s", snapshot.Session.SessionID)
	}

	if snapshot.Project.Name != "wireshark" {
		t.Errorf("expected wireshark, got %s", snapshot.Project.Name)
	}

	if snapshot.Project.Branch != "master" {
		t.Errorf("expected master, got %s", snapshot.Project.Branch)
	}
}

func TestStatusPanelContextReuse(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	reuse := sp.GetProjectContextReuse("test-project")
	if reuse != 0 {
		t.Errorf("expected 0 reuse with no cache, got %v", reuse)
	}
}

func TestStatusPanelFreshness(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	sp.mu.Lock()
	sp.graphStats.NodeCount = 100
	sp.graphStats.SourceFiles = 50
	sp.mu.Unlock()

	freshness := sp.GetFreshness()

	if freshness["graph"] != 1.0 {
		t.Errorf("expected graph freshness 1.0, got %v", freshness["graph"])
	}

	if freshness["index"] <= 0 {
		t.Error("expected some index freshness")
	}
}

func TestStatusPanelBackgroundJobs(t *testing.T) {
	app := ApplicationInMemory()
	app.Daemon = NewDaemon(app, DaemonConfig{
		ProjectDetectInterval: time.Minute,
		IndexInterval:        time.Minute,
		MaxBackgroundJobs:     4,
	})
	app.Daemon.Start()

	sp := NewStatusPanel(app)
	sp.Refresh()

	jobs := sp.GetBackgroundActivity()
	if len(jobs) == 0 {
		t.Error("expected background jobs")
	}

	app.Daemon.Stop()
}

func TestStatusPanelErrors(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	event := Event{
		ID:        "test-error",
		Kind:      ErrorObserved,
		Session:   "session-789",
		ProjectID: "test-project",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"message":  "connection failed",
			"severity": "ERROR",
		},
	}

	sp.OnEvent(event)

	errors := sp.GetRecentErrors()
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(errors))
	}

	if len(errors) > 0 && errors[0].Message != "connection failed" {
		t.Errorf("expected 'connection failed', got '%s'", errors[0].Message)
	}
}

func TestStatusPanelEmitAndRefresh(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	sp.Refresh()
	beforeCount := sp.Snapshot().UpdateCount

	event := Event{
		ID:        "test-refresh",
		Kind:      FileOpened,
		Session:   "session-test",
		ProjectID: "test-project",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"path": "/new/file.go",
		},
	}
	sp.OnEvent(event)

	sp.Refresh()
	afterCount := sp.Snapshot().UpdateCount

	if afterCount <= beforeCount {
		t.Error("expected update count to increase after event and refresh")
	}
}

func TestStatusPanelActiveSymbols(t *testing.T) {
	app := ApplicationInMemory()
	sp := NewStatusPanel(app)

	event := Event{
		ID:        "test-symbol",
		Kind:      SymbolReferenced,
		Session:   "session-symbol",
		ProjectID: "test-project",
		Timestamp: time.Now(),
		Payload: map[string]any{
			"symbol": "process_tracker",
		},
	}

	sp.OnEvent(event)

	symbols := sp.GetActiveSymbols()
	if len(symbols) != 1 {
		t.Errorf("expected 1 active symbol, got %d", len(symbols))
	}

	if len(symbols) > 0 && symbols[0] != "process_tracker" {
		t.Errorf("expected process_tracker, got %s", symbols[0])
	}
}
