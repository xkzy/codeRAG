package services

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"codergag/internal/graph"
	"codergag/internal/models"
)

type SessionManager struct {
	graph          graph.GraphRepository
	snapshotMgr    *SnapshotManager
	hotCache       map[string]*HotContextCache
	activeSession  *Session
	mu             sync.RWMutex
	app            *Application
}

type Session struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	TaskID       string    `json:"task_id,omitempty"`
	State        string    `json:"state"`
	StartedAt    string    `json:"started_at"`
	LastActiveAt string    `json:"last_active_at"`
	GitCommit    string    `json:"git_commit,omitempty"`
	GitBranch    string    `json:"git_branch,omitempty"`
	IsWarm       bool      `json:"is_warm"`
	SnapshotID   string    `json:"snapshot_id,omitempty"`
	ContextReuse float64   `json:"context_reuse"`
	Metrics      SessionMetrics `json:"metrics"`
}

type SessionMetrics struct {
	ColdStartTimeNS   int64 `json:"cold_start_time_ns,omitempty"`
	WarmStartTimeNS   int64 `json:"warm_start_time_ns,omitempty"`
	FilesSearched     int   `json:"files_searched_before_useful_context"`
	GraphQueries      int   `json:"graph_queries"`
	TokensUsed        int   `json:"tokens_used_exploration"`
	CacheHits         int   `json:"cache_hits"`
	CacheMisses       int   `json:"cache_misses"`
	TimeToFirstAction string `json:"time_to_first_useful_action"`
	StaleContextRate  float64 `json:"stale_context_rate"`
}

const (
	SessionStateInitial    = "INITIAL"
	SessionStateLoading   = "LOADING_SNAPSHOT"
	SessionStateChecking  = "CHECKING_FRESHNESS"
	SessionStateRestoring = "RESTORING_TASK"
	SessionStateWarming   = "WARMING_CONTEXT"
	SessionStateActive    = "ACTIVE"
	SessionStateEnding    = "ENDING"
)

type WarmStartContext struct {
	Warm         bool
	ProjectID    string
	HotFiles     []string
	HotFunctions []string
	ActiveTask   string
}

func NewSessionManager(g graph.GraphRepository, app *Application) *SessionManager {
	return &SessionManager{
		graph:       g,
		snapshotMgr: NewSnapshotManager(g),
		hotCache:    make(map[string]*HotContextCache),
		app:        app,
	}
}

func (sm *SessionManager) StartSession(projectID, sessionID string) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	start := time.Now()
	sm.transitionTo(SessionStateLoading)

	snapshot, err := sm.snapshotMgr.LoadLatestSnapshot(projectID)
	if err != nil {
		snapshot = nil
	}

	sm.transitionTo(SessionStateChecking)

	freshness := sm.checkFreshness(projectID, snapshot)

	sm.transitionTo(SessionStateRestoring)

	task := sm.loadPreviousTask(projectID)

	sm.transitionTo(SessionStateWarming)

	hotCtx := sm.getOrCreateHotCache(projectID)
	if snapshot != nil && freshness.IsFresh {
		hotCtx.MergeFrom(snapshot)
	}

	var taskID string
	if task != nil {
		taskID = task.TaskID
	}

	session := &Session{
		ID:            sessionID,
		ProjectID:     projectID,
		TaskID:        taskID,
		State:         SessionStateActive,
		StartedAt:     start.Format(time.RFC3339),
		LastActiveAt:  time.Now().Format(time.RFC3339),
		GitCommit:     freshness.CurrentCommit,
		GitBranch:     freshness.CurrentBranch,
		IsWarm:        freshness.IsFresh || task != nil,
		ContextReuse:  0.0,
	}

	if snapshot != nil {
		session.SnapshotID = snapshot.GitCommit
	}

	metrics := &SessionMetrics{}

	if snapshot != nil {
		metrics.ColdStartTimeNS = time.Since(start).Nanoseconds()
	} else {
		metrics.WarmStartTimeNS = time.Since(start).Nanoseconds()
	}

	session.Metrics = *metrics
	sm.activeSession = session

	if sm.app.StatusPanel != nil {
		sm.app.StatusPanel.SetProjectInfo(
			projectID,
			freshness.CurrentBranch,
			freshness.CurrentCommit,
		)
	}

	return session, nil
}

func (sm *SessionManager) transitionTo(state string) {
	if sm.activeSession != nil {
		sm.activeSession.State = state
	}
}

type FreshnessResult struct {
	IsFresh       bool
	CurrentCommit string
	CurrentBranch string
	StaleFiles   []string
	StaleSymbols []string
}

func (sm *SessionManager) checkFreshness(projectID string, snapshot *ProjectSnapshot) FreshnessResult {
	result := FreshnessResult{
		IsFresh:       true,
		CurrentCommit: "unknown",
		CurrentBranch: "unknown",
	}

	if snapshot == nil {
		result.IsFresh = false
		return result
	}

	if sm.app.Git != nil {
		head, _ := git(snapshot.RootPath, "rev-parse", "HEAD")
		result.CurrentCommit = head
		if head != snapshot.GitCommit {
			result.IsFresh = false
		}

		branch, _ := git(snapshot.RootPath, "rev-parse", "--abbrev-ref", "HEAD")
		result.CurrentBranch = branch
		if branch != snapshot.GitBranch {
			result.IsFresh = false
		}
	}

	return result
}

func (sm *SessionManager) loadPreviousTask(projectID string) *TaskState {
	tasks, _ := sm.graph.FindNodes("Task", map[string]any{"project_id": projectID})
	if len(tasks) == 0 {
		return nil
	}

	var latest *models.Node
	for _, t := range tasks {
		if latest == nil || strProp(t, "updated_at") > strProp(latest, "updated_at") {
			latest = t
		}
	}

	if latest == nil {
		return nil
	}

	taskID := strProp(latest, "task_id")
	if task, err := sm.app.Tasks.Get(projectID, taskID); err == nil {
		return task
	}

	return nil
}

func (sm *SessionManager) getOrCreateHotCache(projectID string) *HotContextCache {
	if cache, ok := sm.hotCache[projectID]; ok {
		return cache
	}

	cache := NewHotContextCache(projectID, sm.graph)
	sm.hotCache[projectID] = cache
	return cache
}

func (sm *SessionManager) GetHotCache(projectID string) *HotContextCache {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	if cache, ok := sm.hotCache[projectID]; ok {
		return cache
	}
	return nil
}

func (sm *SessionManager) GetActiveSession() *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.activeSession
}

func (sm *SessionManager) UpdateSessionMetrics(metrics SessionMetrics) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.activeSession != nil {
		sm.activeSession.Metrics = metrics
	}
}

func (sm *SessionManager) EndSession() error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.activeSession == nil {
		return nil
	}

	sm.activeSession.State = SessionStateEnding
	sm.activeSession.LastActiveAt = time.Now().Format(time.RFC3339)

	if sm.app.StatusPanel != nil {
		sm.app.StatusPanel.Refresh()
	}

	sm.activeSession = nil
	return nil
}

func (sm *SessionManager) GetCurrentSession() *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.activeSession
}

func (sm *SessionManager) GetWarmStartContext() *WarmStartContext {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.activeSession == nil {
		return &WarmStartContext{Warm: false}
	}

	wsc := &WarmStartContext{
		Warm:      sm.activeSession.IsWarm,
		ProjectID: sm.activeSession.ProjectID,
		ActiveTask: sm.activeSession.TaskID,
	}

	if cache, ok := sm.hotCache[sm.activeSession.ProjectID]; ok {
		hotCtx := cache.GetHotContext()
		for _, f := range hotCtx.Files {
			wsc.HotFiles = append(wsc.HotFiles, f.Path)
		}
		for _, s := range hotCtx.Symbols {
			wsc.HotFunctions = append(wsc.HotFunctions, s.Name)
		}
	}

	return wsc
}

func (sm *SessionManager) CreateSnapshot(projectID string) error {
	snapshot, err := sm.snapshotMgr.CreateSnapshot(projectID, sm.app)
	if err != nil {
		return fmt.Errorf("failed to create snapshot: %w", err)
	}

	if err := sm.snapshotMgr.SaveSnapshot(snapshot); err != nil {
		return fmt.Errorf("failed to save snapshot: %w", err)
	}

	return nil
}

type HierarchicalResolver struct {
	graph graph.GraphRepository
	app   *Application
}

func NewHierarchicalResolver(g graph.GraphRepository, app *Application) *HierarchicalResolver {
	return &HierarchicalResolver{
		graph: g,
		app:   app,
	}
}

type ResolvedContext struct {
	Items    []*ContextItem
	Method   string
	Depth    int
	Found    bool
}

func (hr *HierarchicalResolver) ResolveExact(projectID, symbol, file string, line int) *ResolvedContext {
	ctx := &ResolvedContext{Method: "exact", Depth: 0}

	if symbol != "" {
		nodes, _ := hr.graph.FindNodes("Symbol", map[string]any{
			"project_id": projectID,
			"name":       symbol,
		})
		if len(nodes) > 0 {
			for _, n := range nodes {
				ctx.Items = append(ctx.Items, &ContextItem{
					Kind:   "symbol",
					ID:     n.ID,
					Text:   strProp(n, "name"),
					Source: "exact_match",
				})
			}
			ctx.Found = true
			return ctx
		}
	}

	if file != "" && line > 0 {
		nodes, _ := hr.graph.FindNodes("SourceFile", map[string]any{
			"project_id": projectID,
			"path":      file,
		})
		if len(nodes) > 0 {
			ctx.Items = append(ctx.Items, &ContextItem{
				Kind:   "file",
				ID:     nodes[0].ID,
				Text:   file,
				Source: "exact_match",
			})
			ctx.Found = true
			return ctx
		}
	}

	return ctx
}

func (hr *HierarchicalResolver) ResolveKnownRelationships(projectID, symbolID string) *ResolvedContext {
	ctx := &ResolvedContext{Method: "relationships", Depth: 1}

	callers, _ := hr.graph.Neighbors(symbolID, "CALLS", graph.DirIn)
	for _, e := range callers {
		ctx.Items = append(ctx.Items, &ContextItem{
			Kind:   "caller",
			ID:     e.Node.ID,
			Text:   strProp(e.Node, "name"),
			Source: "relationship",
		})
	}

	callees, _ := hr.graph.Neighbors(symbolID, "CALLS", graph.DirOut)
	for _, e := range callees {
		ctx.Items = append(ctx.Items, &ContextItem{
			Kind:   "callee",
			ID:     e.Node.ID,
			Text:   strProp(e.Node, "name"),
			Source: "relationship",
		})
	}

	ctx.Found = len(ctx.Items) > 0
	return ctx
}

func (hr *HierarchicalResolver) ResolveRecentTaskContext(projectID, taskID string) *ResolvedContext {
	ctx := &ResolvedContext{Method: "task_context", Depth: 2}

	if taskID == "" {
		return ctx
	}

	task, err := hr.app.Tasks.Get(projectID, taskID)
	if err != nil {
		return ctx
	}

	for _, ref := range task.References {
		nodes, _ := hr.graph.FindNodes("", map[string]any{"project_id": projectID, "id": ref})
		if len(nodes) > 0 {
			ctx.Items = append(ctx.Items, &ContextItem{
				Kind:   "task_reference",
				ID:     ref,
				Text:   strProp(nodes[0], "name"),
				Source: "task:" + taskID,
			})
		}
	}

	for _, fact := range task.KnownFacts {
		ctx.Items = append(ctx.Items, &ContextItem{
			Kind:   "task_fact",
			Text:   fact.Text,
			Source: "task:" + taskID,
		})
	}

	ctx.Found = len(ctx.Items) > 0
	return ctx
}

func (hr *HierarchicalResolver) ResolveSubsystem(projectID, filePath string) *ResolvedContext {
	ctx := &ResolvedContext{Method: "subsystem", Depth: 3}

	parts := strings.Split(filePath, "/")
	if len(parts) < 2 {
		return ctx
	}

	subsystem := parts[0]
	ctx.Items = append(ctx.Items, &ContextItem{
		Kind:   "subsystem",
		Text:   subsystem,
		Source: "path_analysis",
	})

	nodes, _ := hr.graph.FindNodes("Module", map[string]any{
		"project_id": projectID,
		"path":      subsystem,
	})
	for _, n := range nodes {
		ctx.Items = append(ctx.Items, &ContextItem{
			Kind:   "module",
			ID:     n.ID,
			Text:   strProp(n, "name"),
			Source: "module_lookup",
		})
	}

	ctx.Found = len(ctx.Items) > 0
	return ctx
}

func (hr *HierarchicalResolver) ResolveHotContext(projectID string, limit int) *ResolvedContext {
	ctx := &ResolvedContext{Method: "hot_context", Depth: 0}

	if hr.app == nil || hr.app.Session == nil {
		return ctx
	}

	cache := hr.app.Session.GetHotCache(projectID)
	if cache == nil {
		return ctx
	}

	files := cache.GetFilesByAccess()
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}

	for _, f := range files {
		ctx.Items = append(ctx.Items, &ContextItem{
			Kind:   "hot_file",
			ID:     f.ID,
			Text:   f.Path,
			Source: "hot_cache",
		})
	}

	symbols := cache.GetSymbolsByAccess()
	if limit > 0 && len(symbols) > limit {
		symbols = symbols[:limit]
	}

	for _, s := range symbols {
		ctx.Items = append(ctx.Items, &ContextItem{
			Kind:   "hot_symbol",
			ID:     s.ID,
			Text:   s.Name,
			Source: "hot_cache",
		})
	}

	ctx.Found = len(ctx.Items) > 0
	return ctx
}

func (hr *HierarchicalResolver) ResolveAll(projectID, symbol, file string, line int, taskID string) []*ResolvedContext {
	var results []*ResolvedContext

	if r := hr.ResolveExact(projectID, symbol, file, line); r.Found {
		results = append(results, r)
	}

	if symbolID := hr.findSymbolID(projectID, symbol); symbolID != "" {
		if r := hr.ResolveKnownRelationships(projectID, symbolID); r.Found {
			results = append(results, r)
		}
	}

	if r := hr.ResolveRecentTaskContext(projectID, taskID); r.Found {
		results = append(results, r)
	}

	if file != "" {
		if r := hr.ResolveSubsystem(projectID, file); r.Found {
			results = append(results, r)
		}
	}

	if r := hr.ResolveHotContext(projectID, 20); r.Found {
		results = append(results, r)
	}

	return results
}

func (hr *HierarchicalResolver) findSymbolID(projectID, symbol string) string {
	nodes, _ := hr.graph.FindNodes("Function", map[string]any{
		"project_id": projectID,
		"name":       symbol,
	})
	if len(nodes) > 0 {
		return nodes[0].ID
	}

	nodes, _ = hr.graph.FindNodes("Symbol", map[string]any{
		"project_id": projectID,
		"name":       symbol,
	})
	if len(nodes) > 0 {
		return nodes[0].ID
	}

	return ""
}
