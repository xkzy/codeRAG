package services

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"codergag/internal/cache"
	"codergag/internal/graph"
	"codergag/internal/security"
)

type SecurityAuditService struct {
	graph         graph.GraphRepository
	code          *CodeGraphService
	cm            *cache.CacheManager
	parserVersion string
}

func NewSecurityAuditService(g graph.GraphRepository) *SecurityAuditService {
	return &SecurityAuditService{
		graph:         g,
		code:          NewCodeGraphService(g),
		parserVersion: "security-scan-v1",
	}
}

func (s *SecurityAuditService) SetCache(cm *cache.CacheManager) {
	s.cm = cm
}

type Finding struct {
	CWE        string  `json:"cwe"`
	Name       string  `json:"name"`
	File       string  `json:"file"`
	Line       int     `json:"line"`
	Snippet    string  `json:"snippet"`
	Confidence float64 `json:"confidence"`
	Language   string  `json:"language"`
}

type AuditReport struct {
	ProjectID     string           `json:"project_id"`
	FilesScanned  int              `json:"files_scanned"`
	Findings      []Finding        `json:"findings"`
	TopCWEs       []map[string]any `json:"top_cwes"`
	ParserVersion string           `json:"parser_version"`
	Sources       []string         `json:"sources"`
}

func (s *SecurityAuditService) AuditProject(projectID string, path string, useCache bool, agent string) (map[string]any, error) {
	if useCache && s.cm != nil {
		if cached, err := s.cm.CheckExactCache(projectID, "", "", "", "audit_security", map[string]any{
			"path": s.normalizePath(path),
		}); err == nil && cached != nil && cached.Freshness == "VALID" {
			s.cm.IncrementStat("analysis_hits")
			return cached.Result, nil
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	var allFindings []Finding
	filesScanned := 0
	languageCount := map[string]int{}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("not a directory: %s", absPath)
	}
	_ = entries

	supportedExts := map[string]string{
		".py":   "python",
		".c":    "c",
		".h":    "c",
		".cc":   "c",
		".cpp":  "c",
		".hpp":  "c",
		".rs":   "rust",
		".go":   "go",
		".js":   "javascript",
		".ts":   "javascript",
		".tsx":  "javascript",
		".java": "java",
	}

	walkErr := filepath.Walk(absPath, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == ".venv" || name == "node_modules" || name == "target" || name == "build" || name == "vendor" || name == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		lang, supported := supportedExts[ext]
		if !supported {
			return nil
		}
		filesScanned++
		languageCount[lang]++
		content, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		text := string(content)
		matched := security.MatchPatterns(text, lang)
		for _, pattern := range matched {
			lines := strings.Split(text, "\n")
			matchIdx := pattern.Pattern.FindStringIndex(text)
			if matchIdx == nil {
				continue
			}
			lineNum := 1 + strings.Count(text[:matchIdx[0]], "\n")
			start := lineNum - 1
			end := start + 3
			if start < 0 {
				start = 0
			}
			if end > len(lines) {
				end = len(lines)
			}
			snippet := strings.Join(lines[start:end], "\n")
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			allFindings = append(allFindings, Finding{
				CWE:        pattern.CWE,
				Name:       pattern.Name,
				File:       p,
				Line:       lineNum,
				Snippet:    snippet,
				Confidence: pattern.Confidence,
				Language:   lang,
			})
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	sort.Slice(allFindings, func(i, j int) bool {
		if allFindings[i].Confidence != allFindings[j].Confidence {
			return allFindings[i].Confidence > allFindings[j].Confidence
		}
		return allFindings[i].File < allFindings[j].File
	})

	topCWEs := computeTopCWEs(allFindings, 10)

	now := time.Now().UTC().Format(time.RFC3339)
	for _, finding := range allFindings {
		_, err := s.graph.UpsertNode("SecurityFinding", map[string]any{
			"project_id": projectID,
			"cwe":        finding.CWE,
			"file":       finding.File,
			"line":       finding.Line,
		}, map[string]any{
			"name":           finding.Name,
			"snippet":        finding.Snippet,
			"confidence":     finding.Confidence,
			"language":       finding.Language,
			"first_seen_at":  now,
			"agent":          agent,
			"parser_version": s.parserVersion,
		})
		if err != nil {
			continue
		}
	}

	sources := make([]string, 0, len(languageCount))
	for k := range languageCount {
		sources = append(sources, k)
	}
	sort.Strings(sources)

	result := map[string]any{
		"project_id":     projectID,
		"files_scanned":  filesScanned,
		"findings":       toFindingMaps(allFindings),
		"top_cwes":       topCWEs,
		"parser_version": s.parserVersion,
		"sources":        sources,
		"created_at":     now,
	}

	if useCache && s.cm != nil {
		s.cm.StoreExactCache(projectID, "", "", "", "audit_security",
			map[string]any{"path": s.normalizePath(path)},
			result, 1.0, agent)
		s.cm.IncrementStat("analysis_hits")
	}

	return result, nil
}

func (s *SecurityAuditService) FindByCWE(projectID, cwe string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 50
	}
	entries, err := s.graph.FindNodes("SecurityFinding", map[string]any{
		"project_id": projectID,
		"cwe":        cwe,
	})
	if err != nil {
		return nil, err
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	var results []map[string]any
	for _, e := range entries {
		results = append(results, graph.Present(e))
	}
	return results, nil
}

func (s *SecurityAuditService) GetFindings(projectID string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 100
	}
	entries, err := s.graph.FindNodes("SecurityFinding", map[string]any{
		"project_id": projectID,
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		ci, _ := entries[i].Properties["confidence"].(float64)
		cj, _ := entries[j].Properties["confidence"].(float64)
		return ci > cj
	})
	if len(entries) > limit {
		entries = entries[:limit]
	}
	var results []map[string]any
	for _, e := range entries {
		results = append(results, graph.Present(e))
	}
	return results, nil
}

func (s *SecurityAuditService) GetCWEDetails(cwe string) (map[string]any, error) {
	entry, ok := security.Lookup(cwe)
	if !ok {
		return nil, fmt.Errorf("unknown CWE: %s", cwe)
	}
	allCWEs := security.AllCWEs()
	related := make([]string, 0)
	for _, other := range allCWEs {
		if other.ID != cwe {
			related = append(related, other.ID)
		}
	}
	return map[string]any{
		"cwe":          entry.ID,
		"name":         entry.Name,
		"description":  entry.Description,
		"severity":     entry.Severity,
		"related_cwes": related,
	}, nil
}

func (s *SecurityAuditService) RecordFinding(projectID, cwe, name, file string, line int, confidence float64, agent string) (map[string]any, error) {
	if confidence < 0 || confidence > 1 {
		return nil, &ServiceError{Message: "confidence must be between 0 and 1"}
	}
	entry, ok := security.Lookup(cwe)
	if !ok {
		return nil, fmt.Errorf("unknown CWE: %s", cwe)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	node, err := s.graph.UpsertNode("SecurityFinding", map[string]any{
		"project_id": projectID,
		"cwe":        cwe,
		"file":       file,
		"line":       line,
	}, map[string]any{
		"name":           entry.Name,
		"pattern_name":   name,
		"confidence":     confidence,
		"language":       detectLanguage(file),
		"snippet":        "",
		"first_seen_at":  now,
		"updated_at":     now,
		"agent":          agent,
		"parser_version": s.parserVersion,
	})
	if err != nil {
		return nil, err
	}
	return graph.Present(node), nil
}

func (s *SecurityAuditService) GetMetrics(projectID string) (map[string]any, error) {
	entries, err := s.graph.FindNodes("SecurityFinding", map[string]any{
		"project_id": projectID,
	})
	if err != nil {
		return nil, err
	}
	byCWE := make(map[string]int)
	bySeverity := map[string]int{"HIGH": 0, "MEDIUM": 0, "LOW": 0}
	total := 0
	for _, e := range entries {
		total++
		cwe, _ := e.Properties["cwe"].(string)
		byCWE[cwe]++
		entry, ok := security.Lookup(cwe)
		if ok {
			bySeverity[entry.Severity]++
		}
	}

	topFindings := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		topFindings = append(topFindings, graph.Present(e))
	}
	sort.Slice(topFindings, func(i, j int) bool {
		ci, _ := topFindings[i]["confidence"].(float64)
		cj, _ := topFindings[j]["confidence"].(float64)
		return ci > cj
	})
	if len(topFindings) > 20 {
		topFindings = topFindings[:20]
	}

	cweBreakdown := make(map[string]map[string]any)
	for cwe, count := range byCWE {
		entry, ok := security.Lookup(cwe)
		cweBreakdown[cwe] = map[string]any{
			"name":  entry.Name,
			"count": count,
		}
		if ok {
			cweBreakdown[cwe]["severity"] = entry.Severity
		}
	}

	return map[string]any{
		"project_id":     projectID,
		"total_findings": total,
		"by_cwe":         cweBreakdown,
		"by_severity":    bySeverity,
		"top_findings":   topFindings,
	}, nil
}

func (s *SecurityAuditService) normalizePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func toFindingMaps(findings []Finding) []map[string]any {
	result := make([]map[string]any, len(findings))
	for i, f := range findings {
		result[i] = map[string]any{
			"cwe":        f.CWE,
			"name":       f.Name,
			"file":       f.File,
			"line":       f.Line,
			"snippet":    f.Snippet,
			"confidence": f.Confidence,
			"language":   f.Language,
		}
	}
	return result
}

func computeTopCWEs(findings []Finding, limit int) []map[string]any {
	counts := make(map[string]int)
	names := make(map[string]string)
	severities := make(map[string]string)
	for _, f := range findings {
		counts[f.CWE]++
		names[f.CWE] = f.Name
		entry, ok := security.Lookup(f.CWE)
		if ok {
			severities[f.CWE] = entry.Severity
		}
	}
	type kv struct {
		cwe   string
		count int
	}
	var sorted []kv
	for cwe, count := range counts {
		sorted = append(sorted, kv{cwe, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	result := make([]map[string]any, len(sorted))
	for i, kv := range sorted {
		result[i] = map[string]any{
			"cwe":      kv.cwe,
			"name":     names[kv.cwe],
			"count":    kv.count,
			"severity": severities[kv.cwe],
		}
	}
	return result
}

func detectLanguage(file string) string {
	switch ext := strings.ToLower(filepath.Ext(file)); ext {
	case ".py":
		return "python"
	case ".c", ".h", ".cc", ".cpp", ".hpp":
		return "c"
	case ".rs":
		return "rust"
	case ".go":
		return "go"
	case ".js", ".ts", ".tsx":
		return "javascript"
	case ".java":
		return "java"
	default:
		return "unknown"
	}
}

func (s *SecurityAuditService) ParserVersion() string { return s.parserVersion }

func ensureStr(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func ensureInt(m map[string]any, key string, defaultVal int) int {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case float64:
			return int(val)
		case string:
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return defaultVal
}
