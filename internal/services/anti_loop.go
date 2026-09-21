package services

import (
	"strconv"
	"strings"
	"time"

	"codergag/internal/graph"
)

type AntiLoopDetector struct {
	graph graph.GraphRepository
}

func mapStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func NewAntiLoopDetector(g graph.GraphRepository) *AntiLoopDetector {
	return &AntiLoopDetector{graph: g}
}

type InterceptionResult struct {
	LoopDetected      bool     `json:"loop_detected"`
	HallucinationRisk bool     `json:"hallucination_risk"`
	Issues            []string `json:"issues"`
	Recommendations   []string `json:"recommendations"`
}

func (d *AntiLoopDetector) CheckInterception(projectID, subjectID string) (*InterceptionResult, error) {
	subject, err := d.graph.GetNode(subjectID)
	if err != nil || subject.Properties["project_id"] != projectID {
		return nil, &ServiceError{Message: "subject is absent or belongs to another project"}
	}

	result := &InterceptionResult{
		Issues:          []string{},
		Recommendations: []string{},
	}

	hypotheses, err := d.getHypotheses(subjectID)
	if err != nil {
		return nil, err
	}

	evidence, err := d.getEvidence(subjectID)
	if err != nil {
		return nil, err
	}

	d.detectLoopingClaims(hypotheses, result)
	d.detectContradictions(hypotheses, result)
	d.detectHighConfidenceUnverified(evidence, result)
	d.detectMissingEvidence(hypotheses, evidence, result)
	d.detectStaleClaims(hypotheses, result)

	if len(result.Issues) > 0 {
		result.Recommendations = append(result.Recommendations,
			"Pause and verify findings against decompiled output or original binary",
			"Use codergag_get_evidence to review existing evidence before adding new claims",
			"Use codergag_get_related_code to cross-check function relationships",
		)
	}

	return result, nil
}

func (d *AntiLoopDetector) getHypotheses(subjectID string) ([]map[string]any, error) {
	neighbors, err := d.graph.Neighbors(subjectID, "SUSPECTED_AS", graph.DirOut)
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, en := range neighbors {
		if en.Node.Kind == "Hypothesis" {
			results = append(results, Present(en.Node))
		}
	}
	return results, nil
}

func (d *AntiLoopDetector) getEvidence(subjectID string) ([]map[string]any, error) {
	neighbors, err := d.graph.Neighbors(subjectID, "REFERENCES", graph.DirOut)
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

func (d *AntiLoopDetector) detectLoopingClaims(hypotheses []map[string]any, result *InterceptionResult) {
	byClaim := make(map[string][]map[string]any)
	for _, h := range hypotheses {
		claim := strings.ToLower(strings.TrimSpace(mapStr(h, "claim")))
		if claim == "" {
			continue
		}
		byClaim[claim] = append(byClaim[claim], h)
	}

	for claim, group := range byClaim {
		if len(group) > 2 {
			result.LoopDetected = true
			result.Issues = append(result.Issues,
				"Looping detected: same claim '"+claim+"' recorded "+strconv.Itoa(len(group))+" times for the same subject")
		}
	}
}

func (d *AntiLoopDetector) detectContradictions(hypotheses []map[string]any, result *InterceptionResult) {
	for i := 0; i < len(hypotheses); i++ {
		for j := i + 1; j < len(hypotheses); j++ {
			claim1 := strings.ToLower(mapStr(hypotheses[i], "claim"))
			claim2 := strings.ToLower(mapStr(hypotheses[j], "claim"))

			if d.claimsContradict(claim1, claim2) {
				conf1, _ := hypotheses[i]["confidence"].(float64)
				conf2, _ := hypotheses[j]["confidence"].(float64)
				if conf1 > 0.7 || conf2 > 0.7 {
					result.HallucinationRisk = true
					result.Issues = append(result.Issues,
						"Contradictory high-confidence hypotheses: '"+claim1+"' vs '"+claim2+"'")
				}
			}
		}
	}
}

func (d *AntiLoopDetector) claimsContradict(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	contradictionMarkers := []struct {
		pos, neg string
	}{
		{"is a constructor", "is not a constructor"},
		{"validates", "does not validate"},
		{"encrypts", "does not encrypt"},
		{"decrypts", "does not decrypt"},
		{"calls", "does not call"},
		{"returns", "does not return"},
	}
	for _, cm := range contradictionMarkers {
		if strings.Contains(a, cm.pos) && strings.Contains(b, cm.neg) {
			return true
		}
		if strings.Contains(a, cm.neg) && strings.Contains(b, cm.pos) {
			return true
		}
	}
	return false
}

func (d *AntiLoopDetector) detectHighConfidenceUnverified(evidence []map[string]any, result *InterceptionResult) {
	for _, ev := range evidence {
		conf, ok := ev["confidence"].(float64)
		if !ok {
			continue
		}
		if conf > 0.9 {
			method, _ := ev["method"].(string)
			source, _ := ev["source"].(string)
			desc, _ := ev["description"].(string)
			if source == "manual" && method == "manual" {
				result.HallucinationRisk = true
				result.Issues = append(result.Issues,
					"High-confidence evidence without independent verification ("+desc+")")
			}
		}
	}
}

func (d *AntiLoopDetector) detectMissingEvidence(hypotheses []map[string]any, evidence []map[string]any, result *InterceptionResult) {
	if len(hypotheses) > 0 && len(evidence) == 0 {
		result.HallucinationRisk = true
		result.Issues = append(result.Issues,
			"Hypotheses recorded without supporting evidence")
	}
}

func (d *AntiLoopDetector) detectStaleClaims(hypotheses []map[string]any, result *InterceptionResult) {
	now := time.Now().UTC()
	cutoff := now.Add(-30 * time.Second)

	var recent []map[string]any
	for _, h := range hypotheses {
		tsStr, _ := h["timestamp"].(string)
		if tsStr == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339, tsStr); err == nil {
			if ts.After(cutoff) {
				recent = append(recent, h)
			}
		}
	}

	if len(recent) > 5 {
		result.LoopDetected = true
		result.Issues = append(result.Issues,
			"More than 5 hypotheses recorded in the last 30 seconds - possible looping")
	}
}

func (d *AntiLoopDetector) CheckInterceptionByAddress(projectID, binaryID, address string) (*InterceptionResult, error) {
	fns, err := d.graph.FindNodes("BinaryFunction", map[string]any{
		"project_id": projectID,
		"binary_id":  binaryID,
		"address":    address,
	})
	if err != nil || len(fns) == 0 {
		return nil, &ServiceError{Message: "binary function is absent or belongs to another project"}
	}
	return d.CheckInterception(projectID, fns[0].ID)
}

func (d *AntiLoopDetector) ListInterceptedSubjects(projectID string) ([]map[string]any, error) {
	hyps, err := d.graph.FindNodes("Hypothesis", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	var issues []map[string]any
	for _, h := range hyps {
		subjectID, _ := h.Properties["subject_id"].(string)
		if subjectID == "" {
			continue
		}
		res, err := d.CheckInterception(projectID, subjectID)
		if err == nil && len(res.Issues) > 0 {
			issues = append(issues, map[string]any{
				"subject_id":         h.Properties["subject_id"],
				"issues":             res.Issues,
				"loop_detected":      res.LoopDetected,
				"hallucination_risk": res.HallucinationRisk,
			})
		}
	}
	return issues, nil
}
