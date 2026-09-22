package services

import (
	"fmt"
	"sort"
)

// ProjectProfile returns a structured overview of a project: record counts
// by node kind, evidence counts, index status. Agentctx ctx_project equivalent.
func (a *Application) ProjectProfile(projectID string) (map[string]any, error) {
	projects, err := a.Graph.FindNodes("Project", map[string]any{"id": projectID})
	if err != nil || len(projects) == 0 {
		return nil, fmt.Errorf("project %q not found", projectID)
	}

	count := func(kind string) int {
		n, _ := a.Graph.FindNodes(kind, map[string]any{"project_id": projectID})
		return len(n)
	}

	// Evidence by state/kind.
	evidences, _ := a.Graph.FindNodes("Evidence", map[string]any{"project_id": projectID})
	evByKind := map[string]int{}
	var evidenceCount int
	for _, e := range evidences {
		evidenceCount++
		if kind := strProp(e, "state"); kind != "" {
			evByKind[kind]++
		}
	}

	// Evidence by confidence.
	evByConf := map[string]int{}
	for _, e := range evidences {
		if conf, ok := e.Properties["confidence"].(float64); ok {
			level := "inferred"
			if conf >= 0.9 {
				level = "explicit"
			}
			evByConf[level]++
		}
	}

	// Hypotheses by state.
	hypotheses, _ := a.Graph.FindNodes("Hypothesis", map[string]any{"project_id": projectID})
	hypByState := map[string]int{}
	for _, h := range hypotheses {
		state := strProp(h, "state")
		if state == "" {
			state = "HYPOTHESIS"
		}
		hypByState[state]++
	}

	// Source files by language.
	files, _ := a.Graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	langs := map[string]int{}
	for _, f := range files {
		if lang := strProp(f, "language"); lang != "" {
			langs[lang]++
		}
	}

	// Last indexed file.
	var newest string
	for _, f := range files {
		if t := strProp(f, "last_indexed_at"); t > newest {
			newest = t
		}
	}

	// Record counts by kind for any node types that store context-like records.
	recordKinds := []string{"Evidence", "Hypothesis", "Memory", "Document"}
	recordCounts := map[string]int{}
	for _, kind := range recordKinds {
		if c := count(kind); c > 0 {
			recordCounts[kind] = c
		}
	}
	totalRecords := 0
	for _, c := range recordCounts {
		totalRecords += c
	}

	// Recent evidence (last 10 by timestamp).
	sort.Slice(evidences, func(i, j int) bool {
		return strProp(evidences[i], "timestamp") > strProp(evidences[j], "timestamp")
	})
	recent := make([]map[string]any, 0)
	for _, e := range capRows(evidences, 5) {
		recent = append(recent, Present(e))
	}

	return map[string]any{
		"project_id":             projectID,
		"root":                   strProp(projects[0], "path"),
		"total_records":          totalRecords,
		"record_counts":          recordCounts,
		"files":                  len(files),
		"languages":              langs,
		"functions":              count("Function"),
		"classes":                count("Class"),
		"structs":                count("Struct"),
		"evidence":               evidenceCount,
		"evidence_by_kind":       evByKind,
		"evidence_by_confidence": evByConf,
		"hypotheses":             len(hypotheses),
		"hypotheses_by_state":    hypByState,
		"last_indexed_at":        newest,
		"recent_evidence":        recent,
	}, nil
}
