package services

import (
	"fmt"
	"time"

	"codergag/internal/graph"
	"codergag/internal/ids"
)

type PortingService struct {
	graph graph.GraphRepository
}

func NewPortingService(g graph.GraphRepository) *PortingService {
	return &PortingService{graph: g}
}

type SemanticMapping struct {
	SourceConstruct   string
	SemanticMeaning   string
	TargetConstruct   string
	TranslationRule   string
	CompatibilityIssue string
	Confidence       float64
	VerificationStatus string
}

type PortingDecision struct {
	BinaryID         string
	FunctionAddr     string
	OriginalConstruct string
	TargetConstruct  string
	Reason           string
	Confidence       float64
	DecisionDate     string
	Alternative      string
}

type KnownDifference struct {
	BinaryID         string
	FunctionAddr     string
	DiffType         string
	Description      string
	Severity         string
	Impact           string
}

type PortingStatus struct {
	BinaryID       string
	FunctionAddr   string
	Language       string
	Status         string
	Progress       float64
	Issues         []KnownDifference
	CompletedAt    string
}

const (
	PortingStatusPending     = "PENDING"
	PortingStatusInProgress = "IN_PROGRESS"
	PortingStatusCompleted  = "COMPLETED"
	PortingStatusBlocked    = "BLOCKED"
)

func (s *PortingService) RecordSemanticMapping(projectID, binaryID, functionAddr string, mapping SemanticMapping) (map[string]any, error) {
	mappingID := ids.BinPortingMapID(binaryID, functionAddr, mapping.TargetConstruct)

	mappingNode, err := s.graph.UpsertNode("SemanticMapping", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"function_addr": functionAddr,
		"target_construct": mapping.TargetConstruct,
	}, map[string]any{
		"source_construct":    mapping.SourceConstruct,
		"semantic_meaning":    mapping.SemanticMeaning,
		"target_construct":    mapping.TargetConstruct,
		"translation_rule":    mapping.TranslationRule,
		"compatibility_issue": mapping.CompatibilityIssue,
		"confidence":          mapping.Confidence,
		"verification_status": mapping.VerificationStatus,
		"created_at":          time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}

	fnNodes, _ := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if len(fnNodes) > 0 {
		s.graph.Link("HAS_MAPPING", fnNodes[0].ID, mappingNode.ID, nil)
	}

	return map[string]any{
		"mapping_id":  mappingID,
		"status":      "recorded",
		"confidence":  mapping.Confidence,
	}, nil
}

func (s *PortingService) RecordPortingDecision(projectID, binaryID, functionAddr, language string, decision PortingDecision) (map[string]any, error) {
	decisionID := fmt.Sprintf("portdecision_%s_%s_%s", binaryID, functionAddr, language)

	decisionNode, err := s.graph.UpsertNode("PortingDecision", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"function_addr": functionAddr,
		"language":   language,
	}, map[string]any{
		"decision_id":         decisionID,
		"original_construct": decision.OriginalConstruct,
		"target_construct":   decision.TargetConstruct,
		"reason":            decision.Reason,
		"confidence":        decision.Confidence,
		"decision_date":     time.Now().UTC().Format(time.RFC3339Nano),
		"alternative":       decision.Alternative,
	})
	if err != nil {
		return nil, err
	}

	fnNodes, _ := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if len(fnNodes) > 0 {
		s.graph.Link("PORTING_DECISION", fnNodes[0].ID, decisionNode.ID, nil)
	}

	return map[string]any{
		"decision_id": decisionID,
		"status":      "recorded",
		"function":    functionAddr,
		"language":   language,
	}, nil
}

func (s *PortingService) RecordKnownDifference(projectID, binaryID, functionAddr string, diff KnownDifference) (map[string]any, error) {
	diffID := fmt.Sprintf("diff_%s_%s_%s", binaryID, functionAddr, diff.DiffType)

	diffNode, err := s.graph.UpsertNode("KnownDifference", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"function_addr": functionAddr,
		"diff_type": diff.DiffType,
	}, map[string]any{
		"description": diff.Description,
		"severity":   diff.Severity,
		"impact":     diff.Impact,
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}

	fnNodes, _ := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if len(fnNodes) > 0 {
		s.graph.Link("HAS_DIFFERENCE", fnNodes[0].ID, diffNode.ID, nil)
	}

	return map[string]any{
		"difference_id": diffID,
		"status":        "recorded",
		"severity":      diff.Severity,
	}, nil
}

func (s *PortingService) GetSemanticMappings(projectID, binaryID, functionAddr string) ([]map[string]any, error) {
	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return nil, err
	}

	mappings, _ := s.graph.Neighbors(fnNodes[0].ID, "HAS_MAPPING", graph.DirOut)
	result := []map[string]any{}
	for _, m := range mappings {
		result = append(result, map[string]any{
			"source_construct":     m.Node.Properties["source_construct"],
			"semantic_meaning":   m.Node.Properties["semantic_meaning"],
			"target_construct":   m.Node.Properties["target_construct"],
			"translation_rule":   m.Node.Properties["translation_rule"],
			"compatibility_issue": m.Node.Properties["compatibility_issue"],
			"confidence":          m.Node.Properties["confidence"],
		})
	}
	return result, nil
}

func (s *PortingService) GetPortingStatus(projectID, binaryID, functionAddr, language string) (*PortingStatus, error) {
	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return nil, err
	}

	status := &PortingStatus{
		BinaryID:     binaryID,
		FunctionAddr: functionAddr,
		Language:    language,
		Status:      PortingStatusPending,
		Progress:    0.0,
		Issues:      []KnownDifference{},
	}

	decisions, _ := s.graph.Neighbors(fnNodes[0].ID, "PORTING_DECISION", graph.DirOut)
	for _, d := range decisions {
		if lang, ok := d.Node.Properties["language"].(string); ok && lang == language {
			status.Status = PortingStatusInProgress
			status.Progress += 0.3
		}
	}

	mappings, _ := s.graph.Neighbors(fnNodes[0].ID, "HAS_MAPPING", graph.DirOut)
	status.Progress += float64(len(mappings)) * 0.1
	if status.Progress >= 1.0 {
		status.Status = PortingStatusCompleted
	}

	diffs, _ := s.graph.Neighbors(fnNodes[0].ID, "HAS_DIFFERENCE", graph.DirOut)
	for _, d := range diffs {
		severity, _ := d.Node.Properties["severity"].(string)
		desc, _ := d.Node.Properties["description"].(string)
		status.Issues = append(status.Issues, KnownDifference{
			DiffType:    d.Node.Properties["diff_type"].(string),
			Description: desc,
			Severity:   severity,
			Impact:     d.Node.Properties["impact"].(string),
		})
		if severity == "critical" || severity == "major" {
			status.Status = PortingStatusBlocked
		}
	}

	return status, nil
}

func (s *PortingService) ResolvePortingIssue(projectID, binaryID, functionAddr, diffType, resolution string) (map[string]any, error) {
	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return nil, err
	}

	diffs, _ := s.graph.Neighbors(fnNodes[0].ID, "HAS_DIFFERENCE", graph.DirOut)
	for _, d := range diffs {
		if dt, ok := d.Node.Properties["diff_type"].(string); ok && dt == diffType {
			s.graph.UpsertNode("KnownDifference", map[string]any{"id": d.Node.ID}, map[string]any{
				"resolution":  resolution,
				"resolved_at": time.Now().UTC().Format(time.RFC3339Nano),
			})
			return map[string]any{
				"status":      "resolved",
				"diff_type":   diffType,
				"resolution":  resolution,
			}, nil
		}
	}

	return nil, fmt.Errorf("difference %s not found", diffType)
}
