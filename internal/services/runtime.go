package services

import (
	"context"
	"time"

	"codergag/internal/graph"
	"codergag/internal/models"
	"codergag/internal/runtime"
)

type RuntimeService struct {
	graph     graph.GraphRepository
	observer  *runtime.RuntimeObserver
	index     interface {
		ResolveSymbol(projectID, name string) string
		ResolveFileLine(projectID, file string, line int) string
		ResolveAddress(projectID, addr string) string
		ResolveBinaryFunction(projectID, binaryID, addr string) string
	}
}

func NewRuntimeService(g graph.GraphRepository) *RuntimeService {
	cfg := runtime.ObserverConfig{
		QueueSize:   10000,
		DropPolicy:  runtime.DropPolicyDrop,
		NumWorkers:  4,
		ProcessInterval: 100 * time.Millisecond,
	}
	svc := &RuntimeService{
		graph:    g,
		observer: runtime.NewRuntimeObserver(g, cfg),
	}
	svc.observer.Start()
	return svc
}

func (s *RuntimeService) SetCodeIndex(idx interface {
	ResolveSymbol(projectID, name string) string
	ResolveFileLine(projectID, file string, line int) string
	ResolveAddress(projectID, addr string) string
	ResolveBinaryFunction(projectID, binaryID, addr string) string
}) {
	s.index = idx
	s.observer.SetCodeIndex(idx)
}

func (s *RuntimeService) CaptureOutput(projectID, sessionID string, output []byte, stream string) bool {
	return s.observer.CaptureOutput(projectID, sessionID, output, stream)
}

func (s *RuntimeService) CaptureAgentOutput(projectID, sessionID string, text string) bool {
	return s.observer.CaptureOutput(projectID, sessionID, []byte(text), "agent")
}

func (s *RuntimeService) RegisterSession(sessionID, projectID, taskID string) {
	s.observer.RegisterSession(sessionID, projectID, taskID)
}

func (s *RuntimeService) UnregisterSession(sessionID string) {
	s.observer.UnregisterSession(sessionID)
}

func (s *RuntimeService) GetObservation(projectID, obsID string) (map[string]any, error) {
	node, err := s.observer.GetObservation(projectID, obsID)
	if err != nil {
		return nil, err
	}
	return graph.Present(node), nil
}

func (s *RuntimeService) GetObservationsByFunction(projectID, functionID string) ([]map[string]any, error) {
	nodes, err := s.observer.GetObservationsByFunction(projectID, functionID)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, len(nodes))
	for i, n := range nodes {
		result[i] = graph.Present(n)
	}
	return result, nil
}

func (s *RuntimeService) GetObservationsByProject(projectID string, limit int) ([]map[string]any, error) {
	nodes, err := s.observer.GetObservationsByProject(projectID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, len(nodes))
	for i, n := range nodes {
		result[i] = graph.Present(n)
	}
	return result, nil
}

func (s *RuntimeService) GetLogTemplate(projectID, templateID string) (map[string]any, error) {
	tpl := s.observer.GetLogTemplate(projectID, templateID)
	if tpl == nil {
		return nil, nil
	}
	return map[string]any{
		"id":           tpl.ID,
		"project_id":   tpl.ProjectID,
		"template":     tpl.Template,
		"source_ids":   tpl.SourceIDs,
		"count":        tpl.Count,
		"first_seen":   tpl.FirstSeen,
		"last_seen":    tpl.LastSeen,
		"sample_vals":  tpl.SampleVals,
	}, nil
}

func (s *RuntimeService) GetLogTemplatesByProject(projectID string) ([]map[string]any, error) {
	templates := s.observer.GetLogTemplatesByProject(projectID)
	result := make([]map[string]any, len(templates))
	for i, t := range templates {
		result[i] = map[string]any{
			"id":          t.ID,
			"project_id":  t.ProjectID,
			"template":    t.Template,
			"source_ids":  t.SourceIDs,
			"count":       t.Count,
			"first_seen":  t.FirstSeen,
			"last_seen":   t.LastSeen,
			"sample_vals": t.SampleVals,
		}
	}
	return result, nil
}

func (s *RuntimeService) GetRelatedObservations(projectID, obsID string) ([]map[string]any, error) {
	edges, err := s.graph.Neighbors(obsID, "", graph.DirBoth)
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	for _, e := range edges {
		result = append(result, map[string]any{
			"edge": edgeToMap(e.Edge),
			"node": graph.Present(e.Node),
		})
	}
	return result, nil
}

func (s *RuntimeService) ExplainObservationRelation(projectID, obsID, targetID string) (map[string]any, error) {
	edges, err := s.graph.Neighbors(obsID, "", graph.DirOut)
	if err != nil {
		return nil, err
	}
	for _, e := range edges {
		if e.Node.ID == targetID {
			return map[string]any{
				"observation_id": obsID,
				"target_id":      targetID,
				"relation_kind":  e.Edge.Kind,
				"evidence":       e.Edge.Properties["evidence"],
				"confidence":     e.Edge.Properties["confidence"],
				"method":         e.Edge.Properties["method"],
				"reason":         e.Edge.Properties["reason"],
			}, nil
		}
	}
	return nil, nil
}

func (s *RuntimeService) Stats() map[string]any {
	stats := s.observer.Stats()
	return map[string]any{
		"queue_count":   stats.QueueStats.Count,
		"queue_capacity": stats.QueueStats.Capacity,
		"dropped":       stats.QueueStats.Dropped,
		"num_sessions":  stats.NumSessions,
	}
}

func (s *RuntimeService) Close() {
	s.observer.Stop()
}

type AgentEvent struct {
	ProjectID  string
	SessionID  string
	EventKind  string
	Content    string
	Entity     string
	Confidence float64
}

func (s *RuntimeService) RecordAgentEvent(ctx context.Context, event AgentEvent) error {
	text := event.Content
	var eventType string
	var severity string

	switch event.EventKind {
	case "DECISION":
		eventType = "AGENT_EVENT"
		severity = "P3"
	case "HYPOTHESIS":
		eventType = "AGENT_EVENT"
		severity = "P2"
	case "FAILURE":
		eventType = "AGENT_EVENT"
		severity = "P1"
	case "FIX":
		eventType = "AGENT_EVENT"
		severity = "P2"
	case "VALIDATION":
		eventType = "AGENT_EVENT"
		severity = "P3"
	default:
		eventType = "AGENT_EVENT"
		severity = "P3"
	}

	obs := &runtime.ObservationEvent{
		ID:        "agent_" + event.ProjectID + "_" + time.Now().Format("nanoid"),
		ProjectID: event.ProjectID,
		SessionID: event.SessionID,
		Timestamp: time.Now(),
		RawText:   []byte(text),
		Stream:    "agent",
		Source:    "AGENT",
		Metadata: map[string]string{
			"event_type": eventType,
			"severity":   severity,
		},
	}

	node := s.observer.CaptureOutput(event.ProjectID, event.SessionID, []byte(text), "agent")
	if !node {
		return nil
	}

	if event.Entity != "" && s.index != nil {
		if targetID := s.index.ResolveSymbol(event.ProjectID, event.Entity); targetID != "" {
			s.graph.Link("REFERENCES", targetID, obs.ID, map[string]any{
				"agent_event": event.EventKind,
				"content":     text,
				"confidence":  event.Confidence,
			})
		}
	}

	return nil
}

func edgeToMap(edge *models.Edge) map[string]any {
	if edge == nil {
		return nil
	}
	result := map[string]any{"id": edge.ID, "kind": edge.Kind, "from_id": edge.FromID, "to_id": edge.ToID}
	for k, v := range edge.Properties {
		result[k] = v
	}
	return result
}
