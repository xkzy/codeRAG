package services

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"codergag/internal/graph"
	"codergag/internal/models"
)

var (
	supportedExts = map[string]bool{
		".py": true, ".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
		".rs": true, ".js": true, ".ts": true, ".tsx": true, ".java": true, ".go": true,
	}
	ignoreDirs = map[string]bool{
		".git": true, ".codegraph": true, "node_modules": true, "build": true,
		"dist": true, "target": true, ".venv": true, "vendor": true,
	}

	genericFuncRe = regexp.MustCompile(`(?:^|\n)\s*(?:pub\s+|static\s+|async\s+|def\s+)?(?:[\w:<>,~*&\[\]\s]+\s+)?([A-Za-z_]\w*)\s*\(([^;{}]*)\)\s*(?:->\s*[\w:<>]+)?\s*(?:\{|:)`)
	pyFuncRe    = regexp.MustCompile(`^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)\s*\(([^)]*)\)`, regexp.Multiline)
	jsFuncRe    = regexp.MustCompile(`^\s*(?:export\s+)?(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s+)?\(([^)]*)\)\s*=>|^\s*(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_$][\w$]*)\s*\(([^)]*)\)`, regexp.Multiline)
	goFuncRe    = regexp.MustCompile(`^\s*func\s+(?:\([^)]*\)\s+)?([A-Za-z_]\w*)\s*\(([^)]*)\)`, regexp.Multiline)
	rustFuncRe  = regexp.MustCompile(`^\s*(?:pub(?:\([^)]*\))?\s+)?(?:async\s+)?fn\s+([A-Za-z_]\w*)\s*(?:<[^>]+>)?\s*\(([^)]*)\)`, regexp.Multiline)
	javaFuncRe  = regexp.MustCompile(`^\s*(?:public|private|protected|static|final|synchronized|abstract|native|\s)+[\w<>\[\], ?]+\s+([A-Za-z_]\w*)\s*\(([^)]*)\)\s*(?:throws[^\{]+)?\{`, regexp.Multiline)
	typeRe      = regexp.MustCompile(`^\s*(?:class|struct|enum|interface)\s+([A-Za-z_]\w*)`, regexp.Multiline)
	importRe    = regexp.MustCompile(`^\s*(?:from\s+([\w.]+)\s+import|import\s+([\w./-]+)|#include\s*[<"]([^>"]+))`, regexp.Multiline)
	callRe      = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*\(`)
)

type IndexedFile struct {
	Path      string
	Changed   bool
	Functions int
}

type CodeIndexService struct {
	graph         graph.GraphRepository
	parserVersion string
}

func NewCodeIndexService(g graph.GraphRepository) *CodeIndexService {
	return &CodeIndexService{graph: g, parserVersion: "regex-v1"}
}

func (s *CodeIndexService) ParserVersion() string { return s.parserVersion }

func (s *CodeIndexService) IndexRepository(projectID, root string, incremental bool, ignore []string) (map[string]any, error) {
	path, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	info, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("not a repository directory: %s", root)
	}
	path = info

	project, err := s.graph.UpsertNode("Project", map[string]any{"id": projectID}, map[string]any{
		"project_id": projectID, "path": path,
	})
	if err != nil {
		return nil, err
	}

	excluded := make(map[string]bool)
	for k, v := range ignoreDirs {
		excluded[k] = v
	}
	for _, ig := range ignore {
		excluded[ig] = true
	}

	var results []*IndexedFile
	discovered := make(map[string]bool)
	walkErr := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != path && excluded[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !supportedExts[ext] {
			return nil
		}
		rel, _ := filepath.Rel(path, p)
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if excluded[part] {
				return nil
			}
		}
		discovered[p] = true
		indexed, err := s.IndexFile(project.ID, projectID, p, incremental)
		if err == nil {
			results = append(results, indexed)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	existingFiles, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID})
	var deletedFiles []*models.Node
	for _, nf := range existingFiles {
		nfPath, _ := nf.Properties["path"].(string)
		if strings.HasPrefix(nfPath, path) && !discovered[nfPath] {
			deletedFiles = append(deletedFiles, nf)
		}
	}
	var deleted []string
	for _, file := range deletedFiles {
		deleted = append(deleted, file.ID)
	}
	for _, file := range deletedFiles {
		neighbors, _ := s.graph.Neighbors(file.ID, "DEFINES", graph.DirOut)
		for _, en := range neighbors {
			deleted = append(deleted, en.Node.ID)
		}
	}
	if len(deleted) > 0 {
		s.graph.RemoveNodes(deleted)
	}

	funcs := 0
	changed := 0
	for _, r := range results {
		funcs += r.Functions
		if r.Changed {
			changed++
		}
	}
	return map[string]any{
		"project_id":     projectID,
		"files_seen":     len(results),
		"files_changed":  changed,
		"files_deleted":  len(deletedFiles),
		"functions":      funcs,
		"parser_version": s.parserVersion,
	}, nil
}

func (s *CodeIndexService) IndexFile(projectNodeID, projectID, filePath string, incremental bool) (*IndexedFile, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	text := string(content)

	existing, _ := s.graph.FindNodes("SourceFile", map[string]any{"project_id": projectID, "path": filePath})
	if incremental && len(existing) > 0 {
		existingHash, _ := existing[0].Properties["hash"].(string)
		existingParser, _ := existing[0].Properties["parser_version"].(string)
		if existingHash == digest && existingParser == s.parserVersion {
			return &IndexedFile{Path: filePath, Changed: false, Functions: 0}, nil
		}
	}

	if len(existing) > 0 {
		neighbors, _ := s.graph.Neighbors(existing[0].ID, "DEFINES", graph.DirOut)
		var ids []string
		for _, en := range neighbors {
			ids = append(ids, en.Node.ID)
		}
		s.graph.RemoveNodes(ids)
	}

	language := strings.TrimPrefix(filepath.Ext(filePath), ".")
	source, err := s.graph.UpsertNode("SourceFile", map[string]any{
		"project_id": projectID, "path": filePath,
	}, map[string]any{
		"hash":             digest,
		"parser_version":   s.parserVersion,
		"language":         language,
		"last_indexed_at":  time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	s.graph.Link("CONTAINS", projectNodeID, source.ID, nil)

	funcMatches := extractFunctions(text, filepath.Ext(filePath))
	symbols := make(map[string]string)
	for _, fm := range funcMatches {
		fn, err := s.graph.UpsertNode("Function", map[string]any{
			"project_id":       projectID,
			"qualified_name":   fmt.Sprintf("%s:%s", filePath, fm.name),
		}, map[string]any{
			"name":             fm.name,
			"path":             filePath,
			"line_start":       fm.start,
			"parameter_count":  countParams(fm.params),
			"language":         language,
			"source_hash":      digest,
		})
		if err == nil {
			s.graph.Link("DEFINES", source.ID, fn.ID, nil)
			symbols[fm.name] = fn.ID
		}
	}

	for _, match := range typeRe.FindAllStringSubmatch(text, -1) {
		_, err := s.graph.UpsertNode("Type", map[string]any{
			"project_id": projectID,
			"qualified_name": fmt.Sprintf("%s:%s", filePath, match[1]),
		}, map[string]any{
			"name":      match[1],
			"path":      filePath,
			"line_start": strings.Count(text[:match[0][0]], "\n") + 1,
		})
		if err == nil {
			// link is handled below
		}
	}

	for _, match := range importRe.FindAllStringSubmatch(text, -1) {
		var target string
		for _, g := range match[1:] {
			if g != "" {
				target = g
				break
			}
		}
		if target == "" {
			continue
		}
		dep, err := s.graph.UpsertNode("Module", map[string]any{
			"project_id": projectID, "name": target,
		}, map[string]any{"name": target})
		if err == nil {
			s.graph.Link("IMPORTS", source.ID, dep.ID, nil)
		}
	}

	allFunctions, _ := s.graph.FindNodes("Function", map[string]any{"project_id": projectID})
	funcMap := make(map[string]string)
	for _, n := range allFunctions {
		if name, ok := n.Properties["name"].(string); ok {
			funcMap[name] = n.Properties["id"].(string)
		}
	}

	lines := strings.Split(text, "\n")
	for _, fm := range funcMatches {
		callerID, ok := symbols[fm.name]
		if !ok {
			continue
		}
		start := fm.start - 1
		end := start + 80
		if end > len(lines) {
			end = len(lines)
		}
		if start < 0 {
			start = 0
		}
		block := strings.Join(lines[start:end], "\n")
		for _, callMatch := range callRe.FindAllStringSubmatch(block, -1) {
			called := callMatch[1]
			if called != fm.name {
				if calleeID, found := funcMap[called]; found {
					s.graph.Link("CALLS", callerID, calleeID, map[string]any{
						"source": "static-parser", "confidence": 0.65,
					})
				}
			}
		}
	}

	return &IndexedFile{Path: filePath, Changed: true, Functions: len(funcMatches)}, nil
}

type functionMatch struct {
	name   string
	params string
	start  int
}

func extractFunctions(content, suffix string) []functionMatch {
	var re *regexp.Regexp
	switch suffix {
	case ".py":
		re = pyFuncRe
	case ".rs":
		re = rustFuncRe
	case ".go":
		re = goFuncRe
	case ".js", ".ts", ".tsx":
		re = jsFuncRe
	case ".java":
		re = javaFuncRe
	default:
		re = genericFuncRe
	}
	var matches []functionMatch
	idxMatches := re.FindAllStringSubmatchIndex(content, -1)
	for _, m := range idxMatches {
		if suffix == ".js" || suffix == ".ts" || suffix == ".tsx" {
			name := ""
			params := ""
			if m[2] >= 0 {
				name = content[m[2]:m[3]]
			}
			if m[4] >= 0 {
				params = content[m[4]:m[5]]
			}
			if name == "" && m[6] >= 0 {
				name = content[m[6]:m[7]]
			}
			if params == "" && m[8] >= 0 {
				params = content[m[8]:m[9]]
			}
			matches = append(matches, functionMatch{
				name:   name,
				params: params,
				start:  strings.Count(content[:m[0]], "\n") + 1,
			})
			continue
		}
		name := content[m[2]:m[3]]
		params := content[m[4]:m[5]]
		matches = append(matches, functionMatch{
			name:   name,
			params: params,
			start:  strings.Count(content[:m[0]], "\n") + 1,
		})
	}
	return matches
}

func countParams(params string) int {
	count := 0
	for _, p := range strings.Split(params, ",") {
		if strings.TrimSpace(p) != "" {
			count++
		}
	}
	return count
}
