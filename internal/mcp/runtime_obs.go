package mcp

import "fmt"

func (r *ToolRegistry) handleGetRuntimeHistory(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	limit := getInt(args, "limit", 50)
	observations, err := r.app.Runtime.GetObservationsByProject(projectID, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"observations": observations,
		"count":       len(observations),
	}, nil
}

func (r *ToolRegistry) handleFindObservation(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	obsID := getString(args, "observation_id")
	if obsID == "" {
		return nil, fmt.Errorf("observation_id is required")
	}
	obs, err := r.app.Runtime.GetObservation(projectID, obsID)
	if err != nil {
		return nil, err
	}
	return obs, nil
}

func (r *ToolRegistry) handleGetRelatedObservations(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	obsID := getString(args, "observation_id")
	if obsID == "" {
		return nil, fmt.Errorf("observation_id is required")
	}
	related, err := r.app.Runtime.GetRelatedObservations(projectID, obsID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"related": related,
		"count":  len(related),
	}, nil
}

func (r *ToolRegistry) handleGetFunctionObservations(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	functionID := getString(args, "function_id")
	if functionID == "" {
		return nil, fmt.Errorf("function_id is required")
	}
	observations, err := r.app.Runtime.GetObservationsByFunction(projectID, functionID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"observations": observations,
		"count":       len(observations),
	}, nil
}

func (r *ToolRegistry) handleGetLogTemplate(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	templateID := getString(args, "template_id")
	if templateID == "" {
		return nil, fmt.Errorf("template_id is required")
	}
	template, err := r.app.Runtime.GetLogTemplate(projectID, templateID)
	if err != nil {
		return nil, err
	}
	if template == nil {
		return map[string]any{"template": nil}, nil
	}
	return map[string]any{"template": template}, nil
}

func (r *ToolRegistry) handleGetLogTemplates(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	templates, err := r.app.Runtime.GetLogTemplatesByProject(projectID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"templates": templates,
		"count":     len(templates),
	}, nil
}

func (r *ToolRegistry) handleExplainObservationRelation(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	obsID := getString(args, "observation_id")
	targetID := getString(args, "target_id")
	if obsID == "" || targetID == "" {
		return nil, fmt.Errorf("observation_id and target_id are required")
	}
	explanation, err := r.app.Runtime.ExplainObservationRelation(projectID, obsID, targetID)
	if err != nil {
		return nil, err
	}
	if explanation == nil {
		return map[string]any{"explanation": nil}, nil
	}
	return map[string]any{"explanation": explanation}, nil
}

func (r *ToolRegistry) handleCaptureOutput(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	sessionID := getString(args, "session_id")
	output := getString(args, "output")
	stream := getString(args, "stream")
	if stream == "" {
		stream = "stdout"
	}
	captured := r.app.Runtime.CaptureOutput(projectID, sessionID, []byte(output), stream)
	return map[string]any{
		"captured": captured,
	}, nil
}

func (r *ToolRegistry) handleRegisterRuntimeSession(args map[string]any) (map[string]any, error) {
	projectID, _, err := r.resolveProjectID(args)
	if err != nil {
		return nil, err
	}
	sessionID := getString(args, "session_id")
	taskID := getString(args, "task_id")
	r.app.Runtime.RegisterSession(sessionID, projectID, taskID)
	return map[string]any{
		"session_id": sessionID,
		"registered": true,
	}, nil
}

func (r *ToolRegistry) handleUnregisterRuntimeSession(args map[string]any) (map[string]any, error) {
	sessionID := getString(args, "session_id")
	r.app.Runtime.UnregisterSession(sessionID)
	return map[string]any{
		"session_id":   sessionID,
		"unregistered": true,
	}, nil
}

func (r *ToolRegistry) handleGetRuntimeStats(args map[string]any) (map[string]any, error) {
	return r.app.Runtime.Stats(), nil
}
