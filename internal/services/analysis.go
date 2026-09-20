package services

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"codergag/internal/graph"
	"codergag/internal/models"
)

type AnalysisService struct {
	graph graph.GraphRepository
}

func NewAnalysisService(g graph.GraphRepository) *AnalysisService {
	return &AnalysisService{graph: g}
}

func (s *AnalysisService) Complexity(projectID, functionID string) (map[string]any, error) {
	fn, err := s.graph.GetNode(functionID)
	if err != nil || fn.Properties["project_id"] != projectID {
		return nil, errOrVal(err, "function is absent or belongs to another project", "function is absent or belongs to another project")
	}
	score := 1
	details := map[string]int{"if": 0, "for": 0, "while": 0, "case": 0, "catch": 0, "boolean": 0}
	path := strProp(fn, "path")
	if path != "" {
		text, err := os.ReadFile(path)
		if err == nil {
			t := string(text)
			patterns := map[string]*regexp.Regexp{
				"if":      regexp.MustCompile(`\bif\b`),
				"for":     regexp.MustCompile(`\bfor\b`),
				"while":   regexp.MustCompile(`\bwhile\b`),
				"case":    regexp.MustCompile(`\bcase\b`),
				"catch":   regexp.MustCompile(`\bcatch\b`),
				"boolean": regexp.MustCompile(`&&|\|\|`),
			}
			for key, re := range patterns {
				details[key] = len(re.FindAllString(t, -1))
			}
			for _, v := range details {
				score += v
			}
		}
	}
	return map[string]any{
		"function":              map[string]any{"id": fn.ID, "name": fn.Properties["name"]},
		"cyclomatic_complexity": score,
		"breakdown":             details,
		"method":                "bounded lexical analysis",
	}, nil
}

func errOrVal(err error, msg, defaultMsg string) error {
	if err != nil {
		return err
	}
	return &ServiceError{Message: defaultMsg}
}

type ServiceError struct {
	Message string
}

func (e *ServiceError) Error() string { return e.Message }

func (s *AnalysisService) CircularDependencies(projectID string, limit int) ([][]string, error) {
	nodes, err := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*models.Node)
	adj := make(map[string][]string)
	for _, n := range nodes {
		byID[n.ID] = n
		adj[n.ID] = []string{}
	}
	for _, n := range nodes {
		neighbors, err := s.graph.Neighbors(n.ID, "IMPORTS", graph.DirOut)
		if err != nil {
			continue
		}
		for _, en := range neighbors {
			if _, ok := byID[en.Node.ID]; ok {
				adj[n.ID] = append(adj[n.ID], en.Node.ID)
			}
		}
	}
	var cycles [][]string
	visited := make(map[string]bool)
	stack := []string{}
	onStack := make(map[string]bool)

	var visit func(node string)
	visit = func(node string) {
		if len(cycles) >= limit {
			return
		}
		if onStack[node] {
			idx := indexOf(stack, node)
			if idx >= 0 {
				cycle := append([]string{}, stack[idx:]...)
				names := make([]string, len(cycle))
				for i, x := range cycle {
					p, _ := byID[x].Properties["path"].(string)
					if p != "" {
						names[i] = p
					} else {
						names[i] = x
					}
				}
				if !containsCycle(cycles, names) {
					cycles = append(cycles, names)
				}
			}
			return
		}
		if visited[node] {
			return
		}
		visited[node] = true
		onStack[node] = true
		stack = append(stack, node)
		for _, child := range adj[node] {
			visit(child)
		}
		stack = stack[:len(stack)-1]
		onStack[node] = false
	}

	for node := range adj {
		visit(node)
	}
	return cycles, nil
}

func indexOf(slice []string, val string) int {
	for i, v := range slice {
		if v == val {
			return i
		}
	}
	return -1
}

func containsCycle(cycles [][]string, candidate []string) bool {
	for _, c := range cycles {
		if sliceEqual(c, candidate) {
			return true
		}
	}
	return false
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *AnalysisService) HotPaths(projectID string, limit int) ([]map[string]any, error) {
	fns, err := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int)
	for _, fn := range fns {
		visited := map[string]bool{fn.ID: true}
		queue := []string{fn.ID}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			inNeighbors, _ := s.graph.Neighbors(current, "CALLS", graph.DirIn)
			for _, en := range inNeighbors {
				if !visited[en.Node.ID] {
					visited[en.Node.ID] = true
					counts[fn.ID]++
					queue = append(queue, en.Node.ID)
				}
			}
		}
	}
	type kv struct {
		id    string
		count int
	}
	var sorted []kv
	for k, v := range counts {
		sorted = append(sorted, kv{k, v})
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].count > sorted[i].count {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	var results []map[string]any
	for i := 0; i < limit && i < len(sorted); i++ {
		node, err := s.graph.GetNode(sorted[i].id)
		if err != nil {
			continue
		}
		results = append(results, map[string]any{
			"function":           Present(node),
			"transitive_callers": sorted[i].count,
		})
	}
	return results, nil
}

func (s *AnalysisService) DeadImports(projectID string, limit int) ([]map[string]any, error) {
	files, err := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, source := range files {
		path := strProp(source, "path")
		if path == "" {
			continue
		}
		text, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(text)
		modules, _ := s.graph.Neighbors(source.ID, "IMPORTS", graph.DirOut)
		for _, en := range modules {
			name, _ := en.Node.Properties["name"].(string)
			parts := strings.Split(name, ".")
			short := parts[len(parts)-1]
			slashParts := strings.Split(short, "/")
			short = slashParts[len(slashParts)-1]
			if short != "" {
				count := len(regexp.MustCompile(`\b`+regexp.QuoteMeta(short)+`\b`).FindAllString(content, -1))
				if count <= 1 {
					results = append(results, map[string]any{
						"source": path,
						"module": en.Node.Properties["name"],
					})
				}
			}
		}
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *AnalysisService) ModuleSummary(projectID, query string, limit int) (map[string]any, error) {
	files, err := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var filtered []*models.Node
	for _, n := range files {
		path := strProp(n, "path")
		if query == "" || strings.Contains(strings.ToLower(path), q) {
			filtered = append(filtered, n)
		}
		if len(filtered) >= limit {
			break
		}
	}
	type fileSummary struct {
		Path      string `json:"path"`
		Language  string `json:"language"`
		Functions int    `json:"functions"`
	}
	var summaries []map[string]any
	for _, n := range filtered {
		neighbors, _ := s.graph.Neighbors(n.ID, "DEFINES", graph.DirOut)
		fnCount := 0
		for _, en := range neighbors {
			if en.Node.Kind == "Function" {
				fnCount++
			}
		}
		summaries = append(summaries, map[string]any{
			"path":      n.Properties["path"],
			"language":  n.Properties["language"],
			"functions": fnCount,
		})
	}
	return map[string]any{
		"project_id": projectID,
		"files":      summaries,
		"file_count": len(filtered),
	}, nil
}

func (s *AnalysisService) Signature(projectID string, parameterCount *int, language string, limit int) ([]map[string]any, error) {
	fns, err := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	var results []map[string]any
	for _, n := range fns {
		if parameterCount != nil {
			pc, _ := n.Properties["parameter_count"].(int)
			if pc != *parameterCount {
				continue
			}
		}
		if language != "" {
			lang := strProp(n, "language")
			if lang != language {
				continue
			}
		}
		results = append(results, Present(n))
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

func (s *AnalysisService) EntryPoints(projectID string, limit int) ([]map[string]any, error) {
	fns, err := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	entrySet := map[string]bool{
		"main": true, "init": true, "handler": true, "handle": true, "run": true,
	}
	var results []map[string]any
	for _, n := range fns {
		name := strProp(n, "name")
		if entrySet[name] || strings.HasPrefix(name, "main") || strings.HasPrefix(name, "on_") || strings.HasPrefix(name, "handle_") {
			results = append(results, Present(n))
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

// isTestPath recognises test files by convention, judged on the path relative
// to the project root so a parent directory named "tests" does not count.
func isTestPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	base := strings.ToLower(filepath.Base(rel))
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_"),
		strings.Contains(base, ".test."), strings.Contains(base, ".spec."),
		strings.HasSuffix(base, "test.java"), strings.HasSuffix(base, "tests.java"), strings.HasPrefix(base, "test") && strings.HasSuffix(base, ".java"),
		strings.HasSuffix(base, "_test.rs"), strings.HasSuffix(base, "_test.cc"), strings.HasSuffix(base, "_test.cpp"), strings.HasSuffix(base, "_test.c"):
		return true
	}
	for _, part := range strings.Split(strings.ToLower(filepath.Dir(rel)), "/") {
		if part == "test" || part == "tests" || part == "__tests__" || part == "spec" {
			return true
		}
	}
	return false
}

func (s *AnalysisService) RelatedTests(projectID, functionName string, limit int) ([]map[string]any, error) {
	fns, err := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	if err != nil {
		return nil, err
	}
	root := ""
	if ps, _ := s.graph.FindNodes("Project", map[string]any{"id": projectID}); len(ps) > 0 {
		root, _ = ps[0].Properties["path"].(string)
	}
	fn := strings.ToLower(functionName)
	var results []map[string]any
	for _, n := range fns {
		path := strProp(n, "path")
		name := strProp(n, "name")
		rel := path
		if r, err := filepath.Rel(root, path); err == nil && root != "" {
			rel = r
		}
		if (isTestPath(rel) || strings.HasPrefix(name, "test_")) && strings.Contains(strings.ToLower(name), fn) {
			results = append(results, Present(n))
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}
