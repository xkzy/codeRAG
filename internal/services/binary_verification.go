package services

import (
	"fmt"
	"time"

	"codergag/internal/graph"
	"codergag/internal/ids"
)

type BinaryVerificationService struct {
	graph graph.GraphRepository
}

func NewBinaryVerificationService(g graph.GraphRepository) *BinaryVerificationService {
	return &BinaryVerificationService{graph: g}
}

type VerificationTestCase struct {
	Name         string
	Input        map[string]any
	BinaryOutput map[string]any
	PortedOutput map[string]any
	Match        bool
	Differences  []OutputDifference
}

type OutputDifference struct {
	Field          string
	BinaryValue    any
	PortedValue   any
	DiffType      string
	LikelyCause   string
	Severity      string
	Confidence     float64
}

type BinaryVerificationResult struct {
	TestID            string
	BinaryID         string
	FunctionAddr     string
	TestName         string
	Status           string
	Match            bool
	Differences      []OutputDifference
	ExecutionTime    float64
	Confidence       float64
	VerifiedAt       string
	LikelyCauses    []string
}

const (
	VerificationStatusPassed    = "PASSED"
	VerificationStatusFailed    = "FAILED"
	VerificationStatusBlocked   = "BLOCKED"
	VerificationStatusSkipped   = "SKIPPED"
)

func (s *BinaryVerificationService) RecordVerificationTest(projectID, binaryID, functionAddr, testName string, testCase VerificationTestCase) (map[string]any, error) {
	testID := ids.BinVerificationID(binaryID, functionAddr, testName)

	testNode, err := s.graph.UpsertNode("VerificationTest", map[string]any{
		"project_id":    projectID,
		"binary_id":     binaryID,
		"function_addr": functionAddr,
		"name":         testName,
	}, map[string]any{
		"test_id":       testID,
		"input":         testCase.Input,
		"binary_output": testCase.BinaryOutput,
		"ported_output": testCase.PortedOutput,
		"match":         testCase.Match,
		"created_at":    time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}

	for _, diff := range testCase.Differences {
		diffNode, err := s.graph.UpsertNode("VerificationDifference", map[string]any{
			"project_id": projectID,
			"test_id":   testID,
		}, map[string]any{
			"field":         diff.Field,
			"binary_value":  diff.BinaryValue,
			"ported_value":  diff.PortedValue,
			"diff_type":    diff.DiffType,
			"likely_cause":  diff.LikelyCause,
			"severity":      diff.Severity,
			"confidence":    diff.Confidence,
		})
		if err == nil {
			s.graph.Link("CONTAINS", testNode.ID, diffNode.ID, nil)
		}
	}

	fnNodes, _ := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if len(fnNodes) > 0 {
		s.graph.Link("VERIFIED_BY", fnNodes[0].ID, testNode.ID, nil)
	}

	status := VerificationStatusPassed
	if !testCase.Match {
		status = VerificationStatusFailed
	}

	return map[string]any{
		"test_id":    testID,
		"status":    status,
		"match":     testCase.Match,
		"differences": len(testCase.Differences),
	}, nil
}

func (s *BinaryVerificationService) GetVerificationResults(projectID, binaryID, functionAddr string) ([]BinaryVerificationResult, error) {
	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return nil, err
	}

	tests, _ := s.graph.Neighbors(fnNodes[0].ID, "VERIFIED_BY", graph.DirOut)
	results := []BinaryVerificationResult{}

	for _, t := range tests {
		testName, _ := t.Node.Properties["name"].(string)
		match, _ := t.Node.Properties["match"].(bool)

		result := BinaryVerificationResult{
			TestID:        t.Node.Properties["test_id"].(string),
			BinaryID:     binaryID,
			FunctionAddr: functionAddr,
			TestName:     testName,
			Match:        match,
			Status:       VerificationStatusPassed,
		}
		if !match {
			result.Status = VerificationStatusFailed
		}

		diffs, _ := s.graph.Neighbors(t.Node.ID, "CONTAINS", graph.DirOut)
		for _, d := range diffs {
			if d.Node.Kind == "VerificationDifference" {
				result.Differences = append(result.Differences, OutputDifference{
					Field:        d.Node.Properties["field"].(string),
					BinaryValue:  d.Node.Properties["binary_value"],
					PortedValue:  d.Node.Properties["ported_value"],
					DiffType:     d.Node.Properties["diff_type"].(string),
					LikelyCause:  d.Node.Properties["likely_cause"].(string),
					Severity:     d.Node.Properties["severity"].(string),
					Confidence:   d.Node.Properties["confidence"].(float64),
				})
			}
		}

		results = append(results, result)
	}

	return results, nil
}

func (s *BinaryVerificationService) AnalyzeMismatch(projectID, binaryID, functionAddr, testID string) (map[string]any, error) {
	testNodes, err := s.graph.FindNodes("VerificationTest", map[string]any{
		"project_id": projectID,
		"test_id":   testID,
	})
	if err != nil || len(testNodes) == 0 {
		return nil, fmt.Errorf("test %s not found", testID)
	}

	test := testNodes[0]
	diffs, _ := s.graph.Neighbors(test.ID, "CONTAINS", graph.DirOut)

	analysis := map[string]any{
		"test_id":       testID,
		"likely_causes": []string{},
		"severity":      "minor",
		"recommendation": "",
	}

	if len(diffs) == 0 {
		return analysis, nil
	}

	var totalConfidence float64
	causes := []string{}

	for _, d := range diffs {
		if d.Node.Kind == "VerificationDifference" {
			if cause, ok := d.Node.Properties["likely_cause"].(string); ok && cause != "" {
				causes = append(causes, cause)
			}
			if conf, ok := d.Node.Properties["confidence"].(float64); ok {
				totalConfidence += conf
			}
			if sev, ok := d.Node.Properties["severity"].(string); ok {
				if sev == "critical" {
					analysis["severity"] = "critical"
				}
			}
		}
	}

	analysis["likely_causes"] = causes
	analysis["avg_confidence"] = totalConfidence / float64(len(diffs))

	if len(causes) > 0 {
		analysis["recommendation"] = s.generateRecommendation(causes)
	}

	s.recordMismatchInvestigation(projectID, binaryID, functionAddr, testID, analysis)

	return analysis, nil
}

func (s *BinaryVerificationService) generateRecommendation(causes []string) string {
	seenSigned := false
	seenType := false
	seenOverflow := false

	for _, c := range causes {
		switch c {
		case "signed_conversion", "sign_extension", "signed_division":
			seenSigned = true
		case "type_mismatch", "type_conversion":
			seenType = true
		case "overflow", "wraparound", "integer_overflow":
			seenOverflow = true
		}
	}

	if seenSigned && seenOverflow {
		return "Consider using explicit signed integer types with overflow checking disabled"
	}
	if seenType {
		return "Verify type definitions match between binary and ported implementation"
	}
	if seenOverflow {
		return "Enable overflow checking or use wrapping arithmetic"
	}
	return "Review the affected code region for subtle semantic differences"
}

func (s *BinaryVerificationService) recordMismatchInvestigation(projectID, binaryID, functionAddr, testID string, analysis map[string]any) {
	investigationID := fmt.Sprintf("investigation_%s_%s", testID, time.Now().UTC().Format("20060102150405"))

	investigationNode, err := s.graph.UpsertNode("MismatchInvestigation", map[string]any{
		"project_id": projectID,
		"test_id":   testID,
	}, map[string]any{
		"investigation_id": investigationID,
		"binary_id":       binaryID,
		"function_addr":   functionAddr,
		"likely_causes":   analysis["likely_causes"],
		"severity":        analysis["severity"],
		"recommendation":  analysis["recommendation"],
		"created_at":       time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return
	}

	fnNodes, _ := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if len(fnNodes) > 0 {
		s.graph.Link("HAS_INVESTIGATION", fnNodes[0].ID, investigationNode.ID, nil)
	}
}

func (s *BinaryVerificationService) GetMismatchSummary(projectID, binaryID, functionAddr string) (map[string]any, error) {
	fnNodes, err := s.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    functionAddr,
	})
	if err != nil || len(fnNodes) == 0 {
		return nil, err
	}

	summary := map[string]any{
		"total_tests":     0,
		"passed":          0,
		"failed":          0,
		"critical_issues": 0,
		"major_issues":    0,
		"minor_issues":    0,
	}

	tests, _ := s.graph.Neighbors(fnNodes[0].ID, "VERIFIED_BY", graph.DirOut)
	summary["total_tests"] = len(tests)

	for _, t := range tests {
		match, _ := t.Node.Properties["match"].(bool)
		if match {
			summary["passed"] = summary["passed"].(int) + 1
		} else {
			summary["failed"] = summary["failed"].(int) + 1
		}

		diffs, _ := s.graph.Neighbors(t.Node.ID, "CONTAINS", graph.DirOut)
		for _, d := range diffs {
			if d.Node.Kind == "VerificationDifference" {
				severity, _ := d.Node.Properties["severity"].(string)
				switch severity {
				case "critical":
					summary["critical_issues"] = summary["critical_issues"].(int) + 1
				case "major":
					summary["major_issues"] = summary["major_issues"].(int) + 1
				case "minor":
					summary["minor_issues"] = summary["minor_issues"].(int) + 1
				}
			}
		}
	}

	return summary, nil
}

func (s *BinaryVerificationService) CreateInvestigationTask(projectID, binaryID, functionAddr, testID string) (map[string]any, error) {
	mismatchSummary, err := s.AnalyzeMismatch(projectID, binaryID, functionAddr, testID)
	if err != nil {
		return nil, err
	}

	investigationID := fmt.Sprintf("investigation_%s_%s", testID, time.Now().UTC().Format("20060102150405"))

 taskID := ids.BinArtifactID(binaryID, "investigation", investigationID)
	_ = taskID

	return map[string]any{
		"investigation_id": investigationID,
		"test_id":         testID,
		"function_addr":   functionAddr,
		"severity":        mismatchSummary["severity"],
		"causes":          mismatchSummary["likely_causes"],
		"recommendation":  mismatchSummary["recommendation"],
		"status":          "created",
	}, nil
}
