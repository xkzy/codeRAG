package services

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"codergag/internal/cache"
)

type StatusPanel struct {
	app     *Application
	events  *EventEngine
	mu      sync.RWMutex

	sessionState   SessionState
	projectInfo    ProjectInfo
	cacheStats    cache.CacheStats
	graphStats    GraphStats
	indexProgress map[string]*IndexProgress
	runtimeStats  map[string]any

	currentTask      *TaskState
	activeFiles      []string
	activeSymbols    []string
	activeFunctions  []string
	recentEvents    []Event
	recentErrors    []Observation
	backgroundJobs   []BackgroundJob

	lastUpdate    time.Time
	updateCounter uint64
	debounceInterval time.Duration
}

type SessionState struct {
	SessionID     string
	ProjectID     string
	Connected     bool
	Warm          bool
	ContextReuse  float64
	FilesExplored int
	SymbolsExplored int
	Errors        int
	Warnings      int
}

type ProjectInfo struct {
	Name         string
	Branch       string
	Commit       string
	CommitTime   time.Time
	LastIndexed  time.Time
	IndexFreshness float64
}

type GraphStats struct {
	NodeCount    int
	EdgeCount    int
	SourceFiles  int
	Functions    int
	Classes      int
	Structs      int
	Binaries     int
	Memories     int
	Docs         int
}

type Observation struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	Source    string    `json:"source"`
	Severity  string    `json:"severity"`
	Timestamp time.Time `json:"timestamp"`
	EntityID  string    `json:"entity_id,omitempty"`
	EntityName string   `json:"entity_name,omitempty"`
}

type BackgroundJob struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Progress   string `json:"progress,omitempty"`
	Percent    int    `json:"percent,omitempty"`
}

type StatusPanelSnapshot struct {
	Connection      string            `json:"connection"`
	Session        SessionState      `json:"session"`
	Project        ProjectInfo       `json:"project"`
	CacheStats     cache.CacheStats  `json:"cache_stats"`
	GraphStats     GraphStats        `json:"graph_stats"`
	IndexProgress  map[string]any    `json:"index_progress,omitempty"`
	RuntimeStats   map[string]any    `json:"runtime_stats,omitempty"`
	CurrentTask    *TaskState        `json:"current_task,omitempty"`
	ActiveFiles    []string          `json:"active_files,omitempty"`
	ActiveSymbols  []string          `json:"active_symbols,omitempty"`
	RecentErrors   []Observation     `json:"recent_errors,omitempty"`
	BackgroundJobs []BackgroundJob   `json:"background_jobs,omitempty"`
	UpdatedAt      time.Time         `json:"updated_at"`
	UpdateCount    uint64            `json:"update_count"`
}

func NewStatusPanel(app *Application) *StatusPanel {
	sp := &StatusPanel{
		app:              app,
		events:           app.Events,
		indexProgress:    make(map[string]*IndexProgress),
		runtimeStats:     make(map[string]any),
		debounceInterval: 500 * time.Millisecond,
		recentErrors:     make([]Observation, 0, 20),
		activeFiles:      make([]string, 0, 50),
		activeSymbols:    make([]string, 0, 50),
		activeFunctions: make([]string, 0, 50),
		recentEvents:    make([]Event, 0, 50),
		backgroundJobs:  make([]BackgroundJob, 0, 10),
	}
	sp.events.AddWorker(sp)
	return sp
}

func (sp *StatusPanel) OnEvent(event Event) {
	sp.mu.Lock()
	defer sp.mu.Unlock()

	sp.updateCounter++
	sp.lastUpdate = time.Now()

	switch event.Kind {
	case AgentStarted, SessionStarted:
		sp.sessionState.Connected = true
		sp.sessionState.SessionID = event.Session
		sp.sessionState.ProjectID = event.ProjectID

	case AgentStopped, SessionEnded:
		sp.sessionState.Connected = false

	case FileOpened:
		if path, ok := event.Payload["path"].(string); ok {
			sp.addActiveFile(path)
		}

	case SymbolReferenced, FunctionInvestigated:
		if name, ok := event.Payload["symbol"].(string); ok {
			sp.addActiveSymbol(name)
		}

	case CompilerError, RuntimeError, ErrorObserved:
		sp.sessionState.Errors++
		sp.addRecentError(event)

	case ToolCallFailed:
		sp.sessionState.Errors++

	case ToolCallCompleted:
	}

	sp.recentEvents = append(sp.recentEvents, event)
	if len(sp.recentEvents) > 100 {
		sp.recentEvents = sp.recentEvents[len(sp.recentEvents)-100:]
	}
}

func (sp *StatusPanel) addActiveFile(path string) {
	for _, f := range sp.activeFiles {
		if f == path {
			return
		}
	}
	sp.activeFiles = append(sp.activeFiles, path)
	if len(sp.activeFiles) > 100 {
		sp.activeFiles = sp.activeFiles[len(sp.activeFiles)-100:]
	}
	sp.sessionState.FilesExplored = len(sp.activeFiles)
}

func (sp *StatusPanel) addActiveSymbol(name string) {
	for _, s := range sp.activeSymbols {
		if s == name {
			return
		}
	}
	sp.activeSymbols = append(sp.activeSymbols, name)
	if len(sp.activeSymbols) > 100 {
		sp.activeSymbols = sp.activeSymbols[len(sp.activeSymbols)-100:]
	}
	sp.sessionState.SymbolsExplored = len(sp.activeSymbols)
}

func (sp *StatusPanel) addRecentError(event Event) {
	obs := Observation{
		ID:        event.ID,
		Kind:      string(event.Kind),
		Source:    event.Agent,
		Timestamp: event.Timestamp,
	}
	if msg, ok := event.Payload["message"].(string); ok {
		obs.Message = msg
	}
	if severity, ok := event.Payload["severity"].(string); ok {
		obs.Severity = severity
	} else {
		obs.Severity = "ERROR"
	}
	sp.recentErrors = append(sp.recentErrors, obs)
	if len(sp.recentErrors) > 20 {
		sp.recentErrors = sp.recentErrors[len(sp.recentErrors)-20:]
	}
}

func (sp *StatusPanel) Refresh() {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.refreshLocked()
}

func (sp *StatusPanel) refreshLocked() {
	sp.updateCounter++
	sp.lastUpdate = time.Now()

	if sp.app == nil {
		return
	}

	if cache := sp.app.Cache; cache != nil {
		sp.cacheStats = cache.Stats()
	}

	sp.graphStats = sp.readGraphStats()

	if runtime := sp.app.Runtime; runtime != nil {
		sp.runtimeStats = runtime.Stats()
	}

	if daemon := sp.app.Daemon; daemon != nil {
		sp.backgroundJobs = sp.readBackgroundJobs(daemon)
	}

	sp.computeSessionWarmth()
}

func (sp *StatusPanel) readGraphStats() GraphStats {
	g := sp.app.Graph
	if g == nil {
		return GraphStats{}
	}

	stats := GraphStats{}

	kinds := []string{"File", "Function", "Class", "Struct", "Binary", "Memory", "Document"}
	for _, kind := range kinds {
		nodes, err := g.FindNodes(kind, nil)
		if err == nil {
			switch kind {
			case "File":
				stats.SourceFiles = len(nodes)
			case "Function":
				stats.Functions = len(nodes)
			case "Class":
				stats.Classes = len(nodes)
			case "Struct":
				stats.Structs = len(nodes)
			case "Binary":
				stats.Binaries = len(nodes)
			case "Memory":
				stats.Memories = len(nodes)
			case "Document":
				stats.Docs = len(nodes)
			}
		}
	}

	edges, err := g.QueryReadonly("MATCH ()-[r]->() RETURN count(r) as cnt", nil)
	if err == nil && len(edges) > 0 {
		if cnt, ok := edges[0]["cnt"].(int64); ok {
			stats.EdgeCount = int(cnt)
		}
	}

	nodes, _ := g.FindNodes("", nil)
	stats.NodeCount = len(nodes)

	return stats
}

func (sp *StatusPanel) readBackgroundJobs(daemon *Daemon) []BackgroundJob {
	var jobs []BackgroundJob

	status := daemon.Status()

	if running, ok := status["running"].(bool); ok && running {
		jobs = append(jobs, BackgroundJob{
			Name:   "Daemon",
			Status: "running",
		})
	}

	if bgJobs, ok := status["background_jobs"].(int); ok && bgJobs > 0 {
		if len(jobs) > 0 {
			jobs[len(jobs)-1].Progress = fmt.Sprintf("%d jobs", bgJobs)
		}
	}

	jobs = append(jobs, BackgroundJob{
		Name:   "Graphify",
		Status: "active",
	})

	if runtime := sp.app.Runtime; runtime != nil {
		jobs = append(jobs, BackgroundJob{
			Name:   "Observing",
			Status: "active",
		})
	}

	return jobs
}

func (sp *StatusPanel) computeSessionWarmth() {
	total := sp.cacheStats.ExactHits + sp.cacheStats.SemanticHits + sp.cacheStats.CacheMisses
	if total == 0 {
		sp.sessionState.Warm = false
		sp.sessionState.ContextReuse = 0
		return
	}

	hits := sp.cacheStats.ExactHits + sp.cacheStats.SemanticHits + sp.cacheStats.GraphHits
	sp.sessionState.ContextReuse = float64(hits) / float64(total)

	sp.sessionState.Warm = sp.sessionState.ContextReuse > 0.3 ||
		sp.sessionState.FilesExplored > 10 ||
		(sp.currentTask != nil && len(sp.currentTask.References) > 0)
}

func (sp *StatusPanel) Snapshot() StatusPanelSnapshot {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	snapshot := StatusPanelSnapshot{
		Connection:      sp.connectionStatus(),
		Session:        sp.sessionState,
		Project:        sp.projectInfo,
		CacheStats:     sp.cacheStats,
		GraphStats:     sp.graphStats,
		IndexProgress:  sp.indexProgressMap(),
		RuntimeStats:   sp.runtimeStats,
		CurrentTask:    sp.currentTask,
		ActiveFiles:    sp.activeFiles,
		ActiveSymbols:  sp.activeSymbols,
		RecentErrors:   sp.recentErrors,
		BackgroundJobs: sp.backgroundJobs,
		UpdatedAt:     sp.lastUpdate,
		UpdateCount:    sp.updateCounter,
	}

	return snapshot
}

func (sp *StatusPanel) connectionStatus() string {
	if sp.sessionState.Connected {
		return "Connected"
	}
	return "Disconnected"
}

func (sp *StatusPanel) indexProgressMap() map[string]any {
	result := make(map[string]any)
	for k, v := range sp.indexProgress {
		if v != nil {
			result[k] = map[string]any{
				"phase":       v.Phase,
				"files_total": v.FilesTotal,
				"files_done":  v.FilesDone,
				"functions":   v.Functions,
				"finished":    v.Finished,
				"error":       v.Error,
			}
		}
	}
	return result
}

func (sp *StatusPanel) SetProjectInfo(name, branch, commit string) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.projectInfo.Name = name
	sp.projectInfo.Branch = branch
	sp.projectInfo.Commit = commit
}

func (sp *StatusPanel) SetCurrentTask(task *TaskState) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.currentTask = task
}

func (sp *StatusPanel) GetActiveSymbols() []string {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.activeSymbols
}

func (sp *StatusPanel) GetActiveFiles() []string {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.activeFiles
}

func (sp *StatusPanel) GetRecentEvents() []Event {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.recentEvents
}

func (sp *StatusPanel) GetRecentErrors() []Observation {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.recentErrors
}

func (sp *StatusPanel) GetContextReuse() float64 {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.sessionState.ContextReuse
}

func (sp *StatusPanel) IsSessionWarm() bool {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.sessionState.Warm
}

type ProvenanceEntry struct {
	Symbol    string   `json:"symbol"`
	Reasons   []string `json:"reasons"`
	Freshness string  `json:"freshness"`
	Revision  string  `json:"revision,omitempty"`
	Source    string  `json:"source"`
	Confidence float64 `json:"confidence"`
}

func (sp *StatusPanel) GetSymbolProvenance(symbolName string) []ProvenanceEntry {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	var entries []ProvenanceEntry

	if sp.currentTask != nil {
		for _, ref := range sp.currentTask.References {
			if strings.Contains(ref, symbolName) || symbolName == ref {
				entries = append(entries, ProvenanceEntry{
					Symbol:    symbolName,
					Reasons:   []string{"Current task reference"},
					Freshness: "current",
					Source:    "TaskState",
					Confidence: 0.95,
				})
			}
		}
		for _, fact := range sp.currentTask.KnownFacts {
			if strings.Contains(fact.Text, symbolName) {
				entries = append(entries, ProvenanceEntry{
					Symbol:    symbolName,
					Reasons:   []string{"Task known fact: " + fact.Text},
					Freshness: "current",
					Source:    "TaskState",
					Confidence: 0.8,
				})
			}
		}
	}

	for _, active := range sp.activeSymbols {
		if active == symbolName {
			entries = append(entries, ProvenanceEntry{
				Symbol:    symbolName,
				Reasons:   []string{"Currently active in session"},
				Freshness: "current",
				Source:    "Session",
				Confidence: 0.9,
			})
		}
	}

	if len(entries) == 0 {
		entries = append(entries, ProvenanceEntry{
			Symbol:    symbolName,
			Reasons:   []string{"No explicit provenance found"},
			Freshness: "unknown",
			Source:    "Unknown",
			Confidence: 0.0,
		})
	}

	return entries
}

func (sp *StatusPanel) GetProjectContextReuse(projectID string) float64 {
	if sp.app.Cache == nil {
		return 0
	}
	stats := sp.app.Cache.Stats()
	total := stats.ExactHits + stats.SemanticHits + stats.CacheMisses
	if total == 0 {
		return 0
	}
	return float64(stats.ExactHits+stats.SemanticHits) / float64(total)
}

func (sp *StatusPanel) GetSessionWarmthDetails() map[string]any {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	details := map[string]any{
		"warm":             sp.sessionState.Warm,
		"context_reuse":    sp.sessionState.ContextReuse,
		"files_explored":   sp.sessionState.FilesExplored,
		"symbols_explored": sp.sessionState.SymbolsExplored,
		"errors":           sp.sessionState.Errors,
		"warnings":        sp.sessionState.Warnings,
	}

	if sp.currentTask != nil {
		details["task_id"] = sp.currentTask.TaskID
		details["task_goal"] = sp.currentTask.Goal
		details["task_state"] = sp.currentTask.State
		details["references"] = len(sp.currentTask.References)
	}

	if len(sp.activeFiles) > 0 {
		details["active_files"] = len(sp.activeFiles)
	}

	return details
}

func (sp *StatusPanel) GetFreshness() map[string]float64 {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	freshness := map[string]float64{}

	freshness["cache"] = sp.sessionState.ContextReuse

	if sp.graphStats.NodeCount > 0 {
		freshness["graph"] = 1.0
	}

	if sp.graphStats.SourceFiles > 0 {
		freshness["index"] = 0.95
	}

	return freshness
}

func (sp *StatusPanel) GetBackgroundActivity() []BackgroundJob {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return sp.backgroundJobs
}

func percent(done, total int) int {
	if total == 0 {
		return 0
	}
	return (done * 100) / total
}
