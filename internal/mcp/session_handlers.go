package mcp

import (
	"fmt"

	"codergag/internal/services"
)

func (r *ToolRegistry) handleStartSession(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	projectRoot := getString(args, "project_root")
	if projectRoot == "" {
		return nil, fmt.Errorf("project_root is required")
	}

	session, err := r.app.Session.StartSession(projectID, projectRoot)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"session_id":      session.ID,
		"project_id":      session.ProjectID,
		"warm":            session.IsWarm,
		"context_reuse":   session.ContextReuse,
		"git_commit":      session.GitCommit,
		"git_branch":      session.GitBranch,
		"started_at":      session.StartedAt,
		"task_id":         session.TaskID,
	}, nil
}

func (r *ToolRegistry) handleEndSession(args map[string]any) (map[string]any, error) {
	err := r.app.Session.EndSession()
	if err != nil {
		return nil, err
	}
	return map[string]any{"ended": true}, nil
}

func (r *ToolRegistry) handleGetSession(args map[string]any) (map[string]any, error) {
	session := r.app.Session.GetActiveSession()
	if session == nil {
		return map[string]any{"session": nil}, nil
	}

	return map[string]any{
		"session": map[string]any{
			"id":              session.ID,
			"project_id":      session.ProjectID,
			"task_id":         session.TaskID,
			"state":           session.State,
			"warm":            session.IsWarm,
			"context_reuse":   session.ContextReuse,
			"snapshot_id":     session.SnapshotID,
			"started_at":      session.StartedAt,
			"last_active_at":  session.LastActiveAt,
			"git_commit":      session.GitCommit,
			"git_branch":      session.GitBranch,
			"metrics":         session.Metrics,
		},
	}, nil
}

func (r *ToolRegistry) handleGetWarmStartContext(args map[string]any) (map[string]any, error) {
	_, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}

	session := r.app.Session.GetActiveSession()
	if session == nil {
		return map[string]any{"warm": false}, nil
	}

	wsc := r.app.Session.GetWarmStartContext()
	if wsc == nil || !wsc.Warm {
		return map[string]any{"warm": false}, nil
	}

	return map[string]any{
		"warm":           wsc.Warm,
		"project_id":     wsc.ProjectID,
		"hot_files":      wsc.HotFiles,
		"hot_functions":  wsc.HotFunctions,
		"active_task":    wsc.ActiveTask,
	}, nil
}

func (r *ToolRegistry) handleGetProjectSnapshot(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	_ = projectID

	snap, err := r.app.BuildProjectSnapshot(projectID)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		"project_id":              snap.ProjectID,
		"name":                   snap.Name,
		"root_path":              snap.RootPath,
		"git_commit":             snap.GitCommit,
		"git_branch":             snap.GitBranch,
		"repository_structure":   snap.RepositoryStructure,
		"modules":               snap.Modules,
		"entry_points":          snap.EntryPoints,
		"dependencies":           snap.Dependencies,
		"important_files":       snap.ImportantFiles,
		"created_at":            snap.CreatedAt,
	}, nil
}

func (r *ToolRegistry) handleResolveContext(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}

	symbol := getString(args, "symbol")
	file := getString(args, "file")
	line := getInt(args, "line", 0)
	taskID := getString(args, "task_id")

	resolver := services.NewHierarchicalResolver(r.app.Graph, r.app)

	var results []*services.ResolvedContext
	if symbol != "" || file != "" {
		results = resolver.ResolveAll(projectID, symbol, file, line, taskID)
	} else if taskID != "" {
		results = append(results, resolver.ResolveRecentTaskContext(projectID, taskID))
	} else {
		results = append(results, resolver.ResolveHotContext(projectID, 20))
	}

	var items []*services.ContextItem
	var methods []string
	for _, r := range results {
		if r != nil {
			items = append(items, r.Items...)
			methods = append(methods, r.Method)
		}
	}

	return map[string]any{
		"items":   items,
		"methods": methods,
		"found":   len(items) > 0,
	}, nil
}

func (r *ToolRegistry) handleCreateSnapshot(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}

	err = r.app.Session.CreateSnapshot(projectID)
	if err != nil {
		return nil, err
	}

	return map[string]any{"created": true}, nil
}
