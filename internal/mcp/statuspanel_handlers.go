package mcp

import (
	"fmt"

	"codergag/internal/cache"
	"codergag/internal/services"
)

func (r *ToolRegistry) handleGetStatusPanel(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	_ = projectID

	sp := r.app.StatusPanel
	if sp == nil {
		return nil, fmt.Errorf("status panel not available")
	}

	sp.Refresh()
	snapshot := sp.Snapshot()

	return map[string]any{
		"connection":     snapshot.Connection,
		"session":        snapshot.Session,
		"project":        snapshot.Project,
		"cache_hit_rate": cacheHitRate(snapshot.CacheStats),
		"graph_stats":    snapshot.GraphStats,
		"current_task":    snapshot.CurrentTask,
		"active_files":    snapshot.ActiveFiles,
		"active_symbols":  snapshot.ActiveSymbols,
		"recent_errors":   snapshot.RecentErrors,
		"background_jobs":  snapshot.BackgroundJobs,
		"updated_at":      snapshot.UpdatedAt,
	}, nil
}

func (r *ToolRegistry) handleGetSessionWarmth(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	_ = projectID

	sp := r.app.StatusPanel
	if sp == nil {
		return nil, fmt.Errorf("status panel not available")
	}

	return map[string]any{
		"warm":      sp.IsSessionWarm(),
		"details":   sp.GetSessionWarmthDetails(),
		"reuse":     sp.GetContextReuse(),
		"freshness": sp.GetFreshness(),
	}, nil
}

func (r *ToolRegistry) handleGetSymbolProvenance(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	symbolName := getString(args, "symbol")
	if symbolName == "" {
		return nil, fmt.Errorf("symbol is required")
	}

	sp := r.app.StatusPanel
	if sp == nil {
		return nil, fmt.Errorf("status panel not available")
	}

	_ = projectID
	provenance := sp.GetSymbolProvenance(symbolName)

	return map[string]any{
		"symbol":     symbolName,
		"provenance": provenance,
	}, nil
}

func (r *ToolRegistry) handleGetActiveContext(args map[string]any) (map[string]any, error) {
	sp := r.app.StatusPanel
	if sp == nil {
		return nil, fmt.Errorf("status panel not available")
	}

	return map[string]any{
		"active_files":   sp.GetActiveFiles(),
		"active_symbols": sp.GetActiveSymbols(),
		"recent_events":  sp.GetRecentEvents(),
		"recent_errors":  sp.GetRecentErrors(),
	}, nil
}

func (r *ToolRegistry) handleEmitStatusEvent(args map[string]any) (map[string]any, error) {
	eventKind := getString(args, "event_kind")
	sessionID := getString(args, "session_id")
	projectID, _, _ := r.resolveProjectID(args)

	payload := make(map[string]any)
	if msg, ok := args["message"].(string); ok {
		payload["message"] = msg
	}
	if path, ok := args["path"].(string); ok {
		payload["path"] = path
	}
	if symbol, ok := args["symbol"].(string); ok {
		payload["symbol"] = symbol
	}

	event := services.Event{
		Kind:      services.EventKind(eventKind),
		Session:   sessionID,
		ProjectID: projectID,
		Payload:   payload,
	}

	r.app.Events.Emit(event)

	return map[string]any{
		"emitted": true,
		"event": map[string]any{
			"kind":       eventKind,
			"session_id": sessionID,
		},
	}, nil
}

func (r *ToolRegistry) handleGetBackgroundActivity(args map[string]any) (map[string]any, error) {
	sp := r.app.StatusPanel
	if sp == nil {
		return nil, fmt.Errorf("status panel not available")
	}

	return map[string]any{
		"background_jobs": sp.GetBackgroundActivity(),
		"freshness":       sp.GetFreshness(),
	}, nil
}

func cacheHitRate(stats cache.CacheStats) float64 {
	total := stats.ExactHits + stats.SemanticHits + stats.CacheMisses
	if total == 0 {
		return 0
	}
	return float64(stats.ExactHits+stats.SemanticHits) / float64(total)
}
