package services

import (
	"time"

	"codergag/internal/graph"
	"codergag/internal/models"
)

type EvidenceService struct {
	graph graph.GraphRepository
}

func NewEvidenceService(g graph.GraphRepository) *EvidenceService {
	return &EvidenceService{graph: g}
}

func (s *EvidenceService) RecordHypothesis(projectID, subjectID, claim string, confidence float64, opts map[string]any) (map[string]any, error) {
	if confidence < 0 || confidence > 1 {
		return nil, &ServiceError{Message: "confidence must be between 0 and 1"}
	}
	subject, err := s.graph.GetNode(subjectID)
	if err != nil || subject.Properties["project_id"] != projectID {
		return nil, &ServiceError{Message: "subject is absent or belongs to another project"}
	}
	state := "HYPOTHESIS"
	if o, ok := opts["state"]; ok {
		if str, ok := o.(string); ok {
			state = str
		}
	}
	confidenceSource := "analyst assessment"
	if o, ok := opts["confidence_source"]; ok {
		if str, ok := o.(string); ok {
			confidenceSource = str
		}
	}
	analyst := "unknown"
	if o, ok := opts["analyst"]; ok {
		if str, ok := o.(string); ok {
			analyst = str
		}
	}
	agent := "unknown"
	if o, ok := opts["agent"]; ok {
		if str, ok := o.(string); ok {
			agent = str
		}
	}
	method := "manual"
	if o, ok := opts["method"]; ok {
		if str, ok := o.(string); ok {
			method = str
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)

	hypothesis, err := s.graph.UpsertNode("Hypothesis", map[string]any{
		"project_id":  projectID,
		"subject_id":  subjectID,
		"claim":       claim,
	}, map[string]any{
		"state":              state,
		"confidence":         confidence,
		"confidence_source":  confidenceSource,
		"analyst":            analyst,
		"agent":              agent,
		"method":             method,
		"timestamp":          now,
	})
	if err != nil {
		return nil, err
	}
	s.graph.Link("SUSPECTED_AS", subjectID, hypothesis.ID, map[string]any{
		"confidence": confidence,
		"state":      state,
	})
	return Present(hypothesis), nil
}

func (s *EvidenceService) RecordEvidence(projectID, subjectID, description string, opts map[string]any) (map[string]any, error) {
	if confidence, ok := opts["confidence"].(float64); ok {
		if confidence < 0 || confidence > 1 {
			return nil, &ServiceError{Message: "confidence must be between 0 and 1"}
		}
	} else if confidence, ok := opts["confidence"].(int); ok {
		if confidence < 0 || confidence > 1 {
			return nil, &ServiceError{Message: "confidence must be between 0 and 1"}
		}
	}

	subject, err := s.graph.GetNode(subjectID)
	if err != nil || subject.Properties["project_id"] != projectID {
		return nil, &ServiceError{Message: "subject is absent or belongs to another project"}
	}

	kind := "OBSERVATION"
	if o, ok := opts["kind"]; ok {
		if str, ok := o.(string); ok {
			kind = str
		}
	}
	source := "manual"
	if o, ok := opts["source"]; ok {
		if str, ok := o.(string); ok {
			source = str
		}
	}
	confidence := 1.0
	if o, ok := opts["confidence"]; ok {
		confidence = o
	}
	agent := "unknown"
	if o, ok := opts["agent"]; ok {
		if str, ok := o.(string); ok {
			agent = str
		}
	}
	method := "manual"
	if o, ok := opts["method"]; ok {
		if str, ok := o.(string); ok {
			method = str
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)

	evidence, err := s.graph.UpsertNode("Evidence", map[string]any{
		"project_id":  projectID,
		"subject_id":  subjectID,
		"description": description,
		"source":      source,
	}, map[string]any{
		"state":      kind,
		"confidence": confidence,
		"agent":      agent,
		"method":     method,
		"timestamp":  now,
	})
	if err != nil {
		return nil, err
	}
	s.graph.Link("REFERENCES", subjectID, evidence.ID, map[string]any{"role": "evidence"})

	if supportsID, ok := opts["supports_hypothesis_id"].(string); ok && supportsID != "" {
		s.graph.Link("SUPPORTED_BY", supportsID, evidence.ID, map[string]any{"confidence": confidence})
	}
	return Present(evidence), nil
}

func (s *EvidenceService) GetHypotheses(subjectID string) ([]map[string]any, error) {
	neighbors, err := s.graph.Neighbors(subjectID, "SUSPECTED_AS", graph.DirOut)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, en := range neighbors {
		results = append(results, Present(en.Node))
	}
	return results, nil
}

func (s *EvidenceService) GetEvidence(subjectID string) ([]map[string]any, error) {
	neighbors, err := s.graph.Neighbors(subjectID, "REFERENCES", graph.DirOut)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, en := range neighbors {
		if en.Node.Kind == "Evidence" {
			results = append(results, Present(en.Node))
		}
	}
	return results, nil
}
